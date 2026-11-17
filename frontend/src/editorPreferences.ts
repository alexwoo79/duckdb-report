import { readonly, ref, watch, type Ref } from "vue";

const editorFontSizeStorageKey = "duckdb-report-editor-font-size";
const defaultEditorFontSize = 11;
const minEditorFontSize = 9;
const maxEditorFontSize = 24;

function normalizeEditorFontSize(value: number): number {
  if (!Number.isFinite(value)) return defaultEditorFontSize;
  return Math.min(maxEditorFontSize, Math.max(minEditorFontSize, Math.round(value)));
}

function loadEditorFontSize(): number {
  try {
    const saved = Number(localStorage.getItem(editorFontSizeStorageKey));
    return saved > 0 ? normalizeEditorFontSize(saved) : defaultEditorFontSize;
  } catch {
    return defaultEditorFontSize;
  }
}

const editorFontSize = ref(loadEditorFontSize());

export function useEditorFontSize(host: Ref<HTMLElement | null>) {
  function apply(size = editorFontSize.value) {
    host.value?.style.setProperty("--editor-font-size", `${size}px`);
  }

  function set(size: number) {
    editorFontSize.value = normalizeEditorFontSize(size);
    try {
      localStorage.setItem(editorFontSizeStorageKey, String(editorFontSize.value));
    } catch {
      // The current session keeps the size even when storage is unavailable.
    }
    apply();
  }

  watch(host, () => apply(), { immediate: true });
  watch(editorFontSize, () => apply());

  return {
    editorFontSize: readonly(editorFontSize),
    increaseEditorFontSize: () => {
      set(editorFontSize.value + 1);
      return true;
    },
    decreaseEditorFontSize: () => {
      set(editorFontSize.value - 1);
      return true;
    },
    resetEditorFontSize: () => {
      set(defaultEditorFontSize);
      return true;
    },
  };
}
