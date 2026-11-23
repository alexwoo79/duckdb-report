<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from "vue";
import {
  acceptCompletion,
  autocompletion,
  closeBrackets,
  closeBracketsKeymap,
  completeFromList,
  completionKeymap,
  snippet,
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
} from "@codemirror/commands";
import {
  bracketMatching,
  HighlightStyle,
  indentOnInput,
  syntaxHighlighting,
} from "@codemirror/language";
import { markdown, markdownLanguage } from "@codemirror/lang-markdown";
import { highlightSelectionMatches, search, searchKeymap } from "@codemirror/search";
import { EditorState } from "@codemirror/state";
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

const props = defineProps<{
  modelValue: string;
  busy?: boolean;
}>();

const emit = defineEmits<{
  "update:modelValue": [value: string];
  render: [format: "html" | "pdf", value: string];
  save: [value: string];
  "save-as": [value: string];
  new: [];
}>();

const host = ref<HTMLDivElement | null>(null);
const line = ref(1);
const column = ref(1);
const selectedCharacters = ref(0);
const isMac = /Mac|iPhone|iPad/.test(navigator.platform);
const modifierLabel = isMac ? "⌘" : "Ctrl";
const {
  editorFontSize,
  increaseEditorFontSize,
  decreaseEditorFontSize,
  resetEditorFontSize,
} = useEditorFontSize(host);

let editor: EditorView | null = null;
let syncingFromProps = false;
let modelSyncTimer: number | undefined;

const MODEL_SYNC_DELAY_MS = 160;

const snippetSection = { name: "Quarto 片段", rank: 1 };
const yamlSection = { name: "YAML 选项", rank: 2 };
const chunkSection = { name: "代码块选项", rank: 3 };
const referenceSection = { name: "交叉引用与变量", rank: 4 };

const pythonChunkTemplate =
  "```{python}\n#| label: ${1:analysis}\n#| echo: false\n#| warning: false\n#| fig-cap: \"${2:图表标题}\"\n\n${3:# Python code}\n```";
const rChunkTemplate =
  "```{r}\n#| label: ${1:analysis}\n#| echo: false\n#| warning: false\n#| fig-cap: \"${2:图表标题}\"\n\n${3:# R code}\n```";
const ojsChunkTemplate =
  "```{ojs}\n//| label: ${1:analysis}\n${2:// Observable JavaScript}\n```";

const snippets: Completion[] = [
  snippetCompletion(pythonChunkTemplate, {
    label: "python",
    detail: "Python 代码块",
    type: "function",
    section: snippetSection,
    boost: 30,
  }),
  snippetCompletion(rChunkTemplate, {
    label: "r",
    detail: "R 代码块",
    type: "function",
    section: snippetSection,
    boost: 28,
  }),
  snippetCompletion(ojsChunkTemplate, {
    label: "ojs",
    detail: "Observable JS 代码块",
    type: "function",
    section: snippetSection,
    boost: 24,
  }),
  snippetCompletion("::: {.callout-note}\n## ${1:提示}\n\n${2:正文}\n:::", {
    label: "callout-note",
    detail: "提示框",
    type: "namespace",
    section: snippetSection,
  }),
  snippetCompletion("::: {.callout-warning}\n## ${1:注意}\n\n${2:正文}\n:::", {
    label: "callout-warning",
    detail: "警告框",
    type: "namespace",
    section: snippetSection,
  }),
  snippetCompletion("::: {.callout-tip}\n## ${1:建议}\n\n${2:正文}\n:::", {
    label: "callout-tip",
    detail: "建议框",
    type: "namespace",
    section: snippetSection,
  }),
  snippetCompletion(
    "::: {.panel-tabset}\n## ${1:标签 1}\n\n${2:内容}\n\n## ${3:标签 2}\n\n${4:内容}\n:::",
    {
      label: "panel-tabset",
      detail: "标签页面板",
      type: "namespace",
      section: snippetSection,
    },
  ),
  snippetCompletion(
    ":::: {.columns}\n::: {.column width=\"50%\"}\n${1:左栏}\n:::\n::: {.column width=\"50%\"}\n${2:右栏}\n:::\n::::",
    {
      label: "columns",
      detail: "双栏布局",
      type: "namespace",
      section: snippetSection,
    },
  ),
  snippetCompletion(
    "| ${1:列 1} | ${2:列 2} |\n| --- | --- |\n| ${3:值} | ${4:值} |\n\n: ${5:表标题} {#tbl-${6:name}}",
    {
      label: "table",
      detail: "Markdown 表格",
      type: "namespace",
      section: snippetSection,
    },
  ),
  snippetCompletion(
    "![${1:图标题}](${2:assets/image.png}){#fig-${3:name}}",
    {
      label: "figure",
      detail: "图片与图注",
      type: "namespace",
      section: snippetSection,
    },
  ),
  snippetCompletion("$$ ${1:E = mc^2} $$ {#eq-${2:name}}", {
    label: "equation",
    detail: "带编号公式",
    type: "namespace",
    section: snippetSection,
  }),
];

const yamlCompletions: Completion[] = [
  ["title", "报告标题"],
  ["subtitle", "副标题"],
  ["author", "作者"],
  ["date", "日期"],
  ["lang", "语言"],
  ["format", "输出格式"],
  ["execute", "执行选项"],
  ["params", "运行参数"],
  ["toc", "目录"],
  ["toc-depth", "目录深度"],
  ["embed-resources", "自包含资源"],
  ["code-fold", "折叠代码"],
  ["fig-width", "图宽"],
  ["fig-height", "图高"],
  ["fig-cap-location", "图注位置"],
  ["tbl-cap-location", "表注位置"],
  ["bibliography", "参考文献"],
  ["csl", "引用样式"],
  ["crossref", "交叉引用"],
].map(([key, detail], index) =>
  snippetCompletion(`${key}: `, {
    label: key,
    detail,
    type: "property",
    section: yamlSection,
    boost: 10 - index / 20,
  }),
);

const chunkCompletions: Completion[] = [
  ["label", "块标识"],
  ["echo", "显示代码"],
  ["eval", "执行代码"],
  ["include", "包含输出"],
  ["warning", "显示警告"],
  ["message", "显示消息"],
  ["error", "允许错误"],
  ["results", "结果模式"],
  ["cache", "缓存结果"],
  ["output", "输出模式"],
  ["fig-cap", "图标题"],
  ["fig-width", "图宽"],
  ["fig-height", "图高"],
  ["tbl-cap", "表标题"],
  ["layout-ncol", "布局列数"],
  ["layout-nrow", "布局行数"],
].map(([key, detail], index) =>
  snippetCompletion(`${key}: `, {
    label: key,
    detail,
    type: "property",
    section: chunkSection,
    boost: 8 - index / 20,
  }),
);

const shortcodeCompletions: Completion[] = [
  snippetCompletion("{{< var ${1:name} >}}", {
    label: "var",
    detail: "Quarto 变量",
    type: "variable",
    section: referenceSection,
  }),
  snippetCompletion("{{< include ${1:_chapter.qmd} >}}", {
    label: "include",
    detail: "包含文件",
    type: "variable",
    section: referenceSection,
  }),
  snippetCompletion("{{< pagebreak >}}", {
    label: "pagebreak",
    detail: "分页",
    type: "keyword",
    section: referenceSection,
  }),
  snippetCompletion("{{< video ${1:https://example.com/video.mp4} >}}", {
    label: "video",
    detail: "插入视频",
    type: "variable",
    section: referenceSection,
  }),
  snippetCompletion("{{< embed ${1:notebook.ipynb} >}}", {
    label: "embed",
    detail: "嵌入内容",
    type: "variable",
    section: referenceSection,
  }),
];

const referenceCompletions: Completion[] = [
  snippetCompletion("@fig-${1:name}", {
    label: "@fig-",
    detail: "引用图片",
    type: "variable",
    section: referenceSection,
  }),
  snippetCompletion("@tbl-${1:name}", {
    label: "@tbl-",
    detail: "引用表格",
    type: "variable",
    section: referenceSection,
  }),
  snippetCompletion("@sec-${1:name}", {
    label: "@sec-",
    detail: "引用章节",
    type: "variable",
    section: referenceSection,
  }),
  snippetCompletion("@eq-${1:name}", {
    label: "@eq-",
    detail: "引用公式",
    type: "variable",
    section: referenceSection,
  }),
];

const reportVariableCompletions: Completion[] = [
  ["table", "注入报告表格"],
  ["title", "报告标题"],
  ["database", "数据库路径"],
  ["start_date", "开始日期"],
  ["end_date", "结束日期"],
  ["report_font", "报告中文字体"],
].map(([name, detail]) =>
  snippetCompletion(`{{${name}}}`, {
    label: name,
    detail,
    type: "variable",
    section: referenceSection,
  }),
);

const allCompletions = [
  ...snippets,
  ...yamlCompletions,
  ...chunkCompletions,
  ...shortcodeCompletions,
  ...referenceCompletions,
  ...reportVariableCompletions,
];

function insideYAMLFrontmatter(state: EditorState, pos: number): boolean {
  if (state.doc.line(1).text.trim() !== "---") return false;
  const currentLine = state.doc.lineAt(pos).number;
  for (let number = 2; number <= currentLine; number += 1) {
    if (state.doc.line(number).text.trim() === "---") return false;
  }
  return true;
}

const quartoCompletionSource: CompletionSource = (context) => {
  const state = context.state;
  const currentLine = state.doc.lineAt(context.pos);
  const beforeCursor = state.sliceDoc(currentLine.from, context.pos);

  const chunkMatch = beforeCursor.match(/(?:^|\s)#\|\s*([\w-]*)$/);
  if (chunkMatch) {
    return {
      from: context.pos - chunkMatch[1].length,
      options: chunkCompletions,
      validFor: /^[\w-]*$/,
    };
  }

  if (insideYAMLFrontmatter(state, context.pos)) {
    const yamlMatch = beforeCursor.match(/^\s*([\w-]*)$/);
    if (yamlMatch) {
      return {
        from: context.pos - yamlMatch[1].length,
        options: yamlCompletions,
        validFor: /^[\w-]*$/,
      };
    }
  }

  const shortcodeMatch = beforeCursor.match(/\{\{<\s*([\w-]*)$/);
  if (shortcodeMatch) {
    return {
      from: context.pos - shortcodeMatch[1].length,
      options: shortcodeCompletions,
      validFor: /^[\w-]*$/,
    };
  }

  const variableMatch = beforeCursor.match(/\{\{\s*([\w-]*)$/);
  if (variableMatch) {
    return {
      from: context.pos - variableMatch[1].length,
      options: reportVariableCompletions,
      validFor: /^[\w-]*$/,
    };
  }

  const calloutMatch = beforeCursor.match(/:::\s*\{?\.?([\w-]*)$/);
  if (calloutMatch) {
    return {
      from: context.pos - calloutMatch[1].length,
      options: snippets.filter((option) => option.section === snippetSection),
      validFor: /^[\w-]*$/,
    };
  }

  const word = context.matchBefore(/@?[\w-]*/);
  if (!word || (word.from === word.to && !context.explicit)) return null;
  return {
    from: word.from,
    options: allCompletions,
    validFor: /@?[\w-]*$/,
  };
};

const qmdHighlightStyle = HighlightStyle.define([
  { tag: tags.heading, color: "var(--orange)", fontWeight: "700" },
  { tag: [tags.heading1, tags.heading2], color: "var(--orange)" },
  { tag: [tags.strong, tags.emphasis], color: "var(--text)" },
  { tag: tags.link, color: "var(--blue)", textDecoration: "underline" },
  { tag: tags.url, color: "var(--teal)" },
  { tag: [tags.monospace, tags.string], color: "var(--teal)" },
  {
    tag: [tags.processingInstruction, tags.meta],
    color: "var(--blue)",
  },
  { tag: [tags.comment, tags.lineComment], color: "var(--subtle)" },
  { tag: tags.invalid, color: "var(--red)" },
]);

function replaceSelectionWithSnippet(view: EditorView, template: string) {
  const range = view.state.selection.main;
  snippet(template)(view, null, range.from, range.to);
  view.focus();
}

function toggleHTMLComment(view: EditorView): boolean {
  const range = view.state.selection.main;
  const from = range.empty ? view.state.doc.lineAt(range.from).from : range.from;
  const to = range.empty ? view.state.doc.lineAt(range.to).to : range.to;
  const selected = view.state.sliceDoc(from, to);
  const wrapped = /^<!--\s?([\s\S]*?)\s?-->$/.exec(selected);
  const replacement = wrapped ? wrapped[1] : `<!-- ${selected} -->`;
  view.dispatch({
    changes: { from, to, insert: replacement },
    selection: { anchor: from, head: from + replacement.length },
  });
  return true;
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

function renderFromSelection(view: EditorView, format: "html" | "pdf"): boolean {
  flushModelSync(view);
  emit("render", format, view.state.doc.toString());
  view.focus();
  return true;
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
    run: (view: EditorView) => renderFromSelection(view, "html"),
  },
  {
    key: "Mod-Shift-Enter",
    preventDefault: true,
    run: (view: EditorView) => renderFromSelection(view, "pdf"),
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
    key: "Mod-Shift-s",
    preventDefault: true,
    run: (view: EditorView) => {
      flushModelSync(view);
      emit("save-as", view.state.doc.toString());
      return true;
    },
  },
  {
    key: "Mod-Shift-n",
    preventDefault: true,
    run: () => {
      emit("new");
      return true;
    },
  },
  {
    key: "Mod-Alt-p",
    preventDefault: true,
    run: (view: EditorView) => {
      replaceSelectionWithSnippet(view, pythonChunkTemplate);
      return true;
    },
  },
  {
    key: "Mod-Alt-r",
    preventDefault: true,
    run: (view: EditorView) => {
      replaceSelectionWithSnippet(view, rChunkTemplate);
      return true;
    },
  },
  {
    key: "Mod-Alt-n",
    preventDefault: true,
    run: (view: EditorView) => {
      replaceSelectionWithSnippet(
        view,
        "::: {.callout-note}\n## ${1:提示}\n\n${2:正文}\n:::",
      );
      return true;
    },
  },
  {
    key: "Mod-/",
    preventDefault: true,
    run: toggleHTMLComment,
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
      markdown({
        base: markdownLanguage,
        addKeymap: true,
        completeHTMLTags: false,
        pasteURLAsLink: true,
      }),
      syntaxHighlighting(qmdHighlightStyle),
      autocompletion({
        override: [quartoCompletionSource],
        activateOnTyping: true,
        activateOnTypingDelay: 80,
        closeOnBlur: true,
        defaultKeymap: false,
        icons: true,
        maxRenderedOptions: 28,
      }),
      EditorView.contentAttributes.of({
        "aria-label": "QMD 编辑器",
        spellcheck: "false",
      }),
      EditorView.domEventHandlers({
        blur: () => {
          flushModelSync();
          return false;
        },
      }),
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

defineExpose({
  focus: () => editor?.focus(),
  getValue: () => editor?.state.doc.toString() ?? props.modelValue,
});
</script>

<template>
  <div class="sql-editor-shell qmd-editor-shell">
    <div ref="host" class="sql-code-mirror qmd-code-mirror"></div>
    <div class="sql-editor-status qmd-editor-status" aria-live="polite">
      <span>Ln {{ line }}, Col {{ column }}</span>
      <span v-if="selectedCharacters > 0">已选 {{ selectedCharacters }} 字符</span>
      <span>{{ busy ? "正在渲染报告…" : "Quarto 补全可用" }}</span>
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
        {{ modifierLabel }}+Enter HTML · ⇧{{ modifierLabel }}+Enter PDF ·
        {{ modifierLabel }}+S 保存 · Tab 接受补全 · {{ modifierLabel }}-/+ 字号
      </span>
    </div>
  </div>
</template>
