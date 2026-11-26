<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from "vue";
import {
  acceptCompletion,
  autocompletion,
  closeBrackets,
  closeBracketsKeymap,
  completeFromList,
  completionKeymap,
  snippetCompletion,
  startCompletion,
  type Completion,
  type CompletionSource,
} from "@codemirror/autocomplete";
import {
  defaultKeymap,
  history,
  historyKeymap,
  indentWithTab,
  toggleLineComment,
} from "@codemirror/commands";
import {
  bracketMatching,
  indentOnInput,
  syntaxHighlighting,
} from "@codemirror/language";
import {
  keywordCompletionSource,
  schemaCompletionSource,
  sql,
  type SQLNamespace,
} from "@codemirror/lang-sql";
import { highlightSelectionMatches, search, searchKeymap } from "@codemirror/search";
import { Compartment, EditorState } from "@codemirror/state";
import {
  crosshairCursor,
  drawSelection,
  dropCursor,
  EditorView,
  highlightActiveLine,
  highlightActiveLineGutter,
  highlightSpecialChars,
  keymap,
  lineNumbers,
  rectangularSelection,
} from "@codemirror/view";
import { useEditorFontSize } from "../editorPreferences";
import { formatDuckDBSQL } from "../sqlFormatter";
import { duckDBDialect, sqlHighlightStyle } from "../sqlLanguage";
import type { SQLCompletionTable } from "../types";
import AppIcon from "./AppIcon.vue";

const props = defineProps<{
  modelValue: string;
  tables: SQLCompletionTable[];
  busy?: boolean;
}>();

const emit = defineEmits<{
  "update:modelValue": [value: string];
  run: [query: string];
  save: [value: string];
  "save-as": [value: string];
}>();

type SQLNamespaceMap = Record<string, SQLNamespace>;

const host = ref<HTMLDivElement | null>(null);
const line = ref(1);
const column = ref(1);
const selectedCharacters = ref(0);
const formatFeedback = ref<{
  kind: "success" | "error";
  message: string;
} | null>(null);
const isMac = /Mac|iPhone|iPad/.test(navigator.platform);
const modifierLabel = isMac ? "⌘" : "Ctrl";
const formatShortcutLabel = isMac
  ? `${modifierLabel}+Shift+F / ${modifierLabel}+Alt+L`
  : `${modifierLabel}+Shift+F`;
const {
  editorFontSize,
  increaseEditorFontSize,
  decreaseEditorFontSize,
  resetEditorFontSize,
} = useEditorFontSize(host);

const completionCompartment = new Compartment();
let editor: EditorView | null = null;
let syncingFromProps = false;
let modelSyncTimer: number | undefined;
let formatFeedbackTimer: number | undefined;

const MODEL_SYNC_DELAY_MS = 160;

const tableSection = { name: "表与视图", rank: 1 };
const columnSection = { name: "字段", rank: 2 };

const snippets: Completion[] = [
  snippetCompletion("SELECT ${1:*}\nFROM ${2:table}\nLIMIT ${3:100};", {
    label: "SELECT",
    detail: "查询模板",
    type: "keyword",
    section: "常用片段",
    boost: 20,
  }),
  snippetCompletion(
    "WITH ${1:summary} AS (\n  SELECT ${2:*}\n  FROM ${3:table}\n)\nSELECT *\nFROM ${1:summary};",
    {
      label: "WITH",
      detail: "CTE 模板",
      type: "keyword",
      section: "常用片段",
      boost: 18,
    },
  ),
  snippetCompletion(
    "SELECT ${1:a.*}\nFROM ${2:left_table} AS a\nJOIN ${3:right_table} AS b\n  ON ${4:a.id = b.id};",
    {
      label: "JOIN",
      detail: "连接模板",
      type: "keyword",
      section: "常用片段",
      boost: 16,
    },
  ),
  snippetCompletion(
    "CASE\n  WHEN ${1:condition} THEN ${2:value}\n  ELSE ${3:value}\nEND",
    {
      label: "CASE",
      detail: "条件表达式",
      type: "keyword",
      section: "常用片段",
      boost: 14,
    },
  ),
];

function namespaceMap(value: SQLNamespace | undefined): SQLNamespaceMap | null {
  if (!value || Array.isArray(value)) return null;
  if ("self" in value && "children" in value) return null;
  return value as SQLNamespaceMap;
}

function childNamespace(parent: SQLNamespaceMap, key: string): SQLNamespaceMap {
  const existing = namespaceMap(parent[key]);
  if (existing) return existing;
  const child: SQLNamespaceMap = {};
  parent[key] = child;
  return child;
}

function addTableNamespace(
  root: SQLNamespaceMap,
  path: string[],
  tableNamespace: SQLNamespace,
) {
  let current = root;
  for (const part of path.slice(0, -1)) {
    current = childNamespace(current, part);
  }
  current[path[path.length - 1]] = tableNamespace;
}

function buildSQLNamespace(tables: SQLCompletionTable[]): SQLNamespace {
  const root: SQLNamespaceMap = {};
  for (const table of tables) {
    const columns: Completion[] = table.columns.map((column) => ({
      label: column.name,
      type: "property",
      detail: column.type,
      info: `${table.catalog}.${table.schema}.${table.name}.${column.name} · ${column.type}`,
      section: columnSection,
    }));
    const tableNamespace: SQLNamespace = {
      self: {
        label: table.name,
        type: "class",
        detail: table.type,
        info: `${table.catalog}.${table.schema}.${table.name}`,
        section: tableSection,
      },
      children: columns,
    };

    addTableNamespace(root, [table.name], tableNamespace);
    addTableNamespace(root, [table.schema, table.name], tableNamespace);
    addTableNamespace(root, [table.catalog, table.schema, table.name], tableNamespace);
  }
  return root;
}

function completionSources(tables: SQLCompletionTable[]): CompletionSource[] {
  return [
    schemaCompletionSource({
      dialect: duckDBDialect,
      schema: buildSQLNamespace(tables),
      upperCaseKeywords: true,
    }),
    completeFromList(snippets),
    keywordCompletionSource(duckDBDialect, true, (label, type) => ({
      label,
      type,
      section: "SQL 关键字",
    })),
  ];
}

function completionExtension(tables: SQLCompletionTable[]) {
  return autocompletion({
    override: completionSources(tables),
    activateOnTyping: true,
    activateOnTypingDelay: 80,
    closeOnBlur: true,
    defaultKeymap: false,
    icons: true,
    maxRenderedOptions: 24,
    selectOnOpen: true,
  });
}

function selectedSQL(view: EditorView): string {
  const document = view.state.doc.toString();
  return view.state.selection.ranges
    .filter((range) => !range.empty)
    .map((range) => view.state.sliceDoc(range.from, range.to))
    .join("\n")
    .trim() || document;
}

function flushModelSync(view: EditorView | null = editor) {
  if (modelSyncTimer !== undefined) {
    window.clearTimeout(modelSyncTimer);
    modelSyncTimer = undefined;
  }
  if (view) emit("update:modelValue", view.state.doc.toString());
}

function showFormatFeedback(kind: "success" | "error", message: string) {
  if (formatFeedbackTimer !== undefined) {
    window.clearTimeout(formatFeedbackTimer);
  }
  formatFeedback.value = { kind, message };
  formatFeedbackTimer = window.setTimeout(() => {
    formatFeedback.value = null;
    formatFeedbackTimer = undefined;
  }, kind === "error" ? 5000 : 1800);
}

function formatSQLInEditor(view: EditorView): boolean {
  const state = view.state;
  const ranges = state.selection.ranges.filter((range) => !range.empty);
  const hasSingleSelection =
    ranges.length === 1 && state.selection.ranges.length === 1;
  const from = hasSingleSelection ? ranges[0].from : 0;
  const to = hasSingleSelection ? ranges[0].to : state.doc.length;
  const source = state.sliceDoc(from, to);
  if (!source.trim()) return false;

  try {
    const formatted = formatDuckDBSQL(source);
    if (formatted === source) {
      showFormatFeedback("success", hasSingleSelection ? "所选 SQL 已格式化" : "SQL 已格式化");
      return true;
    }
    const preserveSelection = hasSingleSelection;
    view.dispatch({
      changes: { from, to, insert: formatted },
      selection: preserveSelection
        ? { anchor: from, head: from + formatted.length }
        : { anchor: Math.min(state.selection.main.head, formatted.length) },
      scrollIntoView: true,
    });
    showFormatFeedback(
      "success",
      preserveSelection ? "已格式化所选 SQL" : "已格式化全部 SQL",
    );
    view.focus();
    return true;
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    showFormatFeedback("error", `格式化失败：${message}`);
    return false;
  }
}

function formatFromToolbar() {
  if (editor) formatSQLInEditor(editor);
}

function scheduleModelSync(view: EditorView) {
  if (modelSyncTimer !== undefined) window.clearTimeout(modelSyncTimer);
  modelSyncTimer = window.setTimeout(() => {
    modelSyncTimer = undefined;
    emit("update:modelValue", view.state.doc.toString());
  }, MODEL_SYNC_DELAY_MS);
}

const customKeymap = [
  {
    key: "Mod-=",
    preventDefault: true,
    run: increaseEditorFontSize,
  },
  {
    key: "Mod-Shift-=",
    preventDefault: true,
    run: increaseEditorFontSize,
  },
  {
    key: "Mod-+",
    preventDefault: true,
    run: increaseEditorFontSize,
  },
  {
    key: "Mod--",
    preventDefault: true,
    run: decreaseEditorFontSize,
  },
  {
    key: "Mod-0",
    preventDefault: true,
    run: resetEditorFontSize,
  },
  {
    key: "Tab",
    run: acceptCompletion,
  },
  {
    key: "Mod-Enter",
    preventDefault: true,
    run: (view: EditorView) => {
      flushModelSync(view);
      const query = selectedSQL(view);
      if (query.trim()) emit("run", query);
      return true;
    },
  },
  {
    key: "F5",
    preventDefault: true,
    run: (view: EditorView) => {
      flushModelSync(view);
      const query = selectedSQL(view);
      if (query.trim()) emit("run", query);
      return true;
    },
  },
  {
    key: "Mod-Shift-s",
    preventDefault: true,
    run: (view: EditorView) => {
      flushModelSync(view);
      emit("save-as", view.state.doc.toString());
      return true;
    },
  },
  {
    key: "Mod-s",
    preventDefault: true,
    run: (view: EditorView) => {
      flushModelSync(view);
      emit("save", view.state.doc.toString());
      return true;
    },
  },
  {
    key: "Mod-/",
    preventDefault: true,
    run: toggleLineComment,
  },
  {
    key: "Mod-Space",
    preventDefault: true,
    run: startCompletion,
  },
  {
    key: "Mod-Shift-f",
    preventDefault: true,
    run: formatSQLInEditor,
  },
  ...(isMac
    ? [
        {
          key: "Mod-Alt-l",
          preventDefault: true,
          run: formatSQLInEditor,
        },
      ]
    : []),
];

function updateCursorState(state: EditorState) {
  const selection = state.selection.main;
  const currentLine = state.doc.lineAt(selection.head);
  line.value = currentLine.number;
  column.value = selection.head - currentLine.from + 1;
  selectedCharacters.value = state.selection.ranges.reduce(
    (total, range) => total + Math.abs(range.to - range.from),
    0,
  );
}

function replaceDocument(value: string) {
  if (!editor || editor.state.doc.toString() === value) return;
  if (modelSyncTimer !== undefined) {
    window.clearTimeout(modelSyncTimer);
    modelSyncTimer = undefined;
  }
  syncingFromProps = true;
  editor.dispatch({
    changes: { from: 0, to: editor.state.doc.length, insert: value },
    selection: { anchor: 0 },
  });
  syncingFromProps = false;
}

onMounted(() => {
  if (!host.value) return;
  const state = EditorState.create({
    doc: props.modelValue,
    extensions: [
      lineNumbers(),
      highlightActiveLineGutter(),
      highlightSpecialChars(),
      history(),
      drawSelection(),
      dropCursor(),
      EditorState.allowMultipleSelections.of(true),
      EditorState.tabSize.of(2),
      indentOnInput(),
      bracketMatching(),
      closeBrackets(),
      rectangularSelection(),
      crosshairCursor(),
      highlightActiveLine(),
      highlightSelectionMatches(),
      search({ top: true }),
      sql({ dialect: duckDBDialect }),
      syntaxHighlighting(sqlHighlightStyle),
      EditorView.contentAttributes.of({
        "aria-label": "SQL 编辑器",
        spellcheck: "false",
      }),
      EditorView.domEventHandlers({
        blur: () => {
          flushModelSync();
          return false;
        },
      }),
      completionCompartment.of(completionExtension(props.tables)),
      keymap.of([
        ...customKeymap,
        ...completionKeymap,
        ...closeBracketsKeymap,
        ...defaultKeymap,
        ...historyKeymap,
        ...searchKeymap,
        indentWithTab,
      ]),
      EditorView.updateListener.of((update) => {
        if (update.docChanged && !syncingFromProps) {
          scheduleModelSync(update.view);
        }
        if (update.docChanged || update.selectionSet) {
          updateCursorState(update.state);
        }
      }),
    ],
  });
  editor = new EditorView({ state, parent: host.value });
  updateCursorState(state);
});

onBeforeUnmount(() => {
  flushModelSync();
  if (formatFeedbackTimer !== undefined) {
    window.clearTimeout(formatFeedbackTimer);
  }
  editor?.destroy();
  editor = null;
});

watch(
  () => props.modelValue,
  (value) => replaceDocument(value),
);

watch(
  () => props.tables,
  (tables) => {
    editor?.dispatch({
      effects: completionCompartment.reconfigure(completionExtension(tables)),
    });
  },
);

defineExpose({
  focus: () => editor?.focus(),
  getValue: () => editor?.state.doc.toString() ?? props.modelValue,
});
</script>

<template>
  <div class="sql-editor-shell">
    <div ref="host" class="sql-code-mirror"></div>
    <div class="sql-editor-status" aria-live="polite">
      <span>Ln {{ line }}, Col {{ column }}</span>
      <span v-if="selectedCharacters > 0">已选 {{ selectedCharacters }} 字符</span>
      <span>{{ busy ? "查询执行中…" : "Enter 换行" }}</span>
      <span
        v-if="formatFeedback"
        class="sql-format-feedback"
        :class="formatFeedback.kind"
      >
        {{ formatFeedback.message }}
      </span>
      <button
        type="button"
        class="sql-format-button"
        :title="`格式化 SQL (${formatShortcutLabel})`"
        @click="formatFromToolbar"
      >
        <AppIcon name="format" :size="12" />
        <span>格式化</span>
        <kbd>{{ modifierLabel }}+Shift+F</kbd>
      </button>
      <span class="editor-font-size-control">
        <button type="button" title="减小编辑器字号" @click="decreaseEditorFontSize">
          −
        </button>
        <span>{{ editorFontSize }}px</span>
        <button type="button" title="增大编辑器字号" @click="increaseEditorFontSize">
          +
        </button>
      </span>
      <span class="sql-editor-shortcuts">
        {{ modifierLabel }}+Enter 运行 · {{ modifierLabel }}+S 保存 ·
        {{ modifierLabel }}+Shift+F 格式化 · Tab 接受补全
      </span>
    </div>
  </div>
</template>
