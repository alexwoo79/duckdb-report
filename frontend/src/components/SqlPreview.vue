<script setup lang="ts">
import {
  computed,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";
import { sql } from "@codemirror/lang-sql";
import { syntaxHighlighting } from "@codemirror/language";
import { EditorState } from "@codemirror/state";
import {
  EditorView,
  lineNumbers,
} from "@codemirror/view";
import { formatDuckDBSQL } from "../sqlFormatter";
import { duckDBDialect, sqlHighlightStyle } from "../sqlLanguage";

const props = withDefaults(defineProps<{
  sql: string;
  emptyText?: string;
  ariaLabel?: string;
  maxHeight?: number;
}>(), {
  emptyText: "没有可显示的 SQL。",
  ariaLabel: "只读 SQL 预览",
  maxHeight: 220,
});

const host = ref<HTMLDivElement | null>(null);
let editor: EditorView | null = null;

const formattedSQL = computed(() => {
  const source = props.sql.trim();
  if (!source) return "";
  try {
    return formatDuckDBSQL(source);
  } catch {
    return source;
  }
});

const content = computed(() => formattedSQL.value || props.emptyText);
const hasSQL = computed(() => formattedSQL.value.length > 0);

function updateDocument(value: string) {
  if (!editor) return;
  editor.dispatch({
    changes: {
      from: 0,
      to: editor.state.doc.length,
      insert: value,
    },
  });
}

onMounted(() => {
  if (!host.value) return;
  const state = EditorState.create({
    doc: content.value,
    extensions: [
      lineNumbers(),
      EditorState.readOnly.of(true),
      EditorState.tabSize.of(2),
      EditorView.editable.of(false),
      EditorView.lineWrapping,
      sql({ dialect: duckDBDialect }),
      syntaxHighlighting(sqlHighlightStyle),
      EditorView.contentAttributes.of({
        "aria-label": props.ariaLabel,
        "aria-readonly": "true",
        spellcheck: "false",
      }),
    ],
  });
  editor = new EditorView({
    state,
    parent: host.value,
  });
});

watch(content, updateDocument, { flush: "post" });

onBeforeUnmount(() => {
  editor?.destroy();
  editor = null;
});
</script>

<template>
  <div
    class="sql-preview"
    :class="{ 'is-empty': !hasSQL }"
    :style="{ '--sql-preview-max-height': `${maxHeight}px` }"
  >
    <div ref="host" class="sql-preview-editor"></div>
  </div>
</template>

<style scoped>
.sql-preview {
  min-height: 58px;
  max-height: var(--sql-preview-max-height);
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: color-mix(in srgb, var(--bg-deep) 78%, var(--panel));
}

.sql-preview :deep(.cm-editor) {
  color: var(--text);
  background: transparent;
  font: calc(10px * var(--ui-font-scale)) / 1.65 var(--mono);
}

.sql-preview :deep(.cm-editor.cm-focused) {
  outline: none;
}

.sql-preview :deep(.cm-scroller) {
  max-height: var(--sql-preview-max-height);
  overflow: auto;
  font-family: var(--mono);
  line-height: 1.65;
}

.sql-preview :deep(.cm-content) {
  padding: 10px 12px;
}

.sql-preview :deep(.cm-line) {
  padding: 0;
}

.sql-preview :deep(.cm-gutters) {
  color: color-mix(in srgb, var(--subtle) 66%, transparent);
  background: color-mix(in srgb, var(--panel) 84%, transparent);
  border-right: 1px solid var(--border);
}

.sql-preview :deep(.cm-lineNumbers .cm-gutterElement) {
  min-width: 34px;
  padding: 0 8px 0 4px;
}

.sql-preview :deep(.cm-selectionBackground),
.sql-preview :deep(.cm-editor.cm-focused .cm-selectionBackground) {
  background: color-mix(in srgb, var(--blue) 26%, transparent);
}

.sql-preview.is-empty :deep(.cm-gutters) {
  display: none;
}

.sql-preview.is-empty :deep(.cm-content) {
  color: var(--muted);
}
</style>
