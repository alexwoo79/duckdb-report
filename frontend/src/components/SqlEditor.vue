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
  HighlightStyle,
  indentOnInput,
  syntaxHighlighting,
} from "@codemirror/language";
import {
  PostgreSQL,
  SQLDialect,
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
import { tags } from "@lezer/highlight";
import { useEditorFontSize } from "../editorPreferences";
import type { SQLCompletionTable } from "../types";

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
const modifierLabel = /Mac|iPhone|iPad/.test(navigator.platform) ? "⌘" : "Ctrl";
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

const MODEL_SYNC_DELAY_MS = 160;

const duckDBDialect = SQLDialect.define({
  ...PostgreSQL.spec,
  keywords: `${PostgreSQL.spec.keywords ?? ""}
    ATTACH DETACH INSTALL LOAD PIVOT UNPIVOT QUALIFY SAMPLE SUMMARIZE
    DESCRIBE EXPORT COPY MACRO COLUMNS EXCLUDE REPLACE RENAME STAR`,
  builtin: `${PostgreSQL.spec.builtin ?? ""}
    list_filter list_transform list_aggregate list_value list_sort unnest
    generate_series range read_csv_auto read_parquet read_json_auto glob
    regexp_matches string_agg arg_max arg_min quantile_cont median
    approx_count_distinct struct_pack map_from_entries json_extract json_value
    epoch_ms strftime date_trunc`,
  types: `${PostgreSQL.spec.types ?? ""}
    HUGEINT UHUGEINT UBIGINT UTINYINT USMALLINT UINTEGER BIGNUM VARINT
    MAP STRUCT UNION LIST ARRAY ENUM UUID JSON`,
  caseInsensitiveIdentifiers: true,
});

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

const sqlHighlightStyle = HighlightStyle.define([
  { tag: tags.keyword, color: "var(--orange)", fontWeight: "600" },
  {
    tag: [tags.string, tags.special(tags.string), tags.regexp],
    color: "var(--teal)",
  },
  {
    tag: [tags.number, tags.bool, tags.null, tags.atom],
    color: "var(--blue)",
  },
  {
    tag: [tags.function(tags.variableName), tags.function(tags.propertyName)],
    color: "var(--blue)",
  },
  { tag: [tags.typeName, tags.className], color: "var(--teal)" },
  { tag: tags.operator, color: "var(--muted)" },
  {
    tag: [tags.comment, tags.lineComment, tags.blockComment],
    color: "var(--subtle)",
    fontStyle: "italic",
  },
  { tag: [tags.variableName, tags.propertyName], color: "var(--text)" },
  { tag: [tags.bracket, tags.punctuation], color: "var(--muted)" },
  { tag: tags.invalid, color: "var(--red)" },
]);

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
        Tab 接受补全 · {{ modifierLabel }}-/+ 字号
      </span>
    </div>
  </div>
</template>
