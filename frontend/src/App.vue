<script setup lang="ts">
import {
  computed,
  defineAsyncComponent,
  onBeforeUnmount,
  onMounted,
  reactive,
  ref,
  watch,
} from "vue";
import AppIcon from "./components/AppIcon.vue";
import ResultGrid from "./components/ResultGrid.vue";
import {
  backend,
  baseName,
  errorMessage,
  fileKind,
  fileStem,
  formatTime,
} from "./api";
import type {
  AppSettings,
  Bootstrap,
  DataCatalog,
  DataSource,
  DatabaseRef,
  Preset,
  PresetType,
  QueryResult,
  RenderResult,
  SQLCompletionTable,
  StepEvent,
  TableRef,
  TableStructureResult,
  TableWriteResult,
  UpdateDownload,
  UpdateInfo,
  ViewName,
  Workflow,
  WorkflowStep,
} from "./types";
import appIcon from "./assets/images/app-icon.svg";

const SqlEditor = defineAsyncComponent(() => import("./components/SqlEditor.vue"));
const QmdEditor = defineAsyncComponent(() => import("./components/QmdEditor.vue"));
const TableEditorPage = defineAsyncComponent(
  () => import("./components/TableEditorPage.vue"),
);

interface LogEntry {
  time: string;
  level: "info" | "success" | "error";
  text: string;
}

interface StepState {
  status: "idle" | "running" | "success" | "error";
  message: string;
  duration: number;
}

const loading = ref(true);
const fatalError = ref("");
const state = ref<Bootstrap | null>(null);
const workflow = ref<Workflow | null>(null);
const activeView = ref<ViewName>("sql");
const activeTable = ref<TableRef | null>(null);
const inspectorOpen = ref(true);
const logsOpen = ref(true);
const settingsBusy = ref(false);
const exportWorkspaceBusy = ref(false);
const updateBusy = ref(false);
const updateDownloadBusy = ref(false);
const updateInfo = ref<UpdateInfo | null>(null);
const updateError = ref("");
const updateDownloaded = ref<UpdateDownload | null>(null);
const settingsDraft = ref<AppSettings>({
  displayName: "",
  workspace: "",
  uiFontSize: 13,
});
const logs = ref<LogEntry[]>([]);
const workflowBusy = ref(false);
const workflowTransferBusy = ref(false);
const sqlBusy = ref(false);
const reportBusy = ref(false);
const theme = ref<"dark" | "light">("dark");
const toast = ref<{ text: string; kind: "info" | "error" | "success" } | null>(null);
let toastTimer: number | undefined;
let updatePollTimer: number | undefined;

const sqlFiles = computed(() => state.value?.sqlFiles ?? []);
const templates = computed(() => state.value?.templates ?? []);
const tables = computed(() => state.value?.tables ?? []);
const sources = computed(() => state.value?.sources ?? []);
const runs = computed(() => state.value?.runs ?? []);
const presets = computed(() => workflow.value?.presets ?? []);
const runtimeParams = computed(() => workflow.value?.runtime ?? {});

const selectedSQL = ref("");
const sqlContent = ref("");
const sqlResult = ref<QueryResult | null>(null);
const sqlFilter = ref("");
const selectedTemplate = ref("");
const reportContent = ref("");
const reportFormat = ref<"html" | "pdf">("html");
const reportBusyLabel = ref("正在渲染报告…");
const previewUrl = ref("");
const previewError = ref("");
const activeRunID = ref("");
const renderResult = ref<RenderResult | null>(null);
const sqlEditorRatio = ref(52);
const reportEditorRatio = ref(46);
const sqlEditorCollapsed = ref(false);
const sqlResultCollapsed = ref(false);
const reportEditorCollapsed = ref(false);
const reportPreviewCollapsed = ref(false);
const sidebarWidthStorageKey = "duckdb-report-sidebar-width";
const sidebarWidth = ref(loadSidebarWidth());
const sqlSplitRef = ref<HTMLElement | null>(null);
const reportSplitRef = ref<HTMLElement | null>(null);
const sqlEditorRef = ref<{ getValue(): string } | null>(null);
const qmdEditorRef = ref<{ getValue(): string } | null>(null);
const sqlCompletionTables = ref<SQLCompletionTable[]>([]);
let stopSplitResize: (() => void) | undefined;
let stopSidebarResize: (() => void) | undefined;
const sqlDisplayName = computed(
  () => selectedSQL.value || "临时查询（未保存）",
);
const newQMDContent = `---
title: "{{title}}"
subtitle: "Quarto 分析报告"
date: today
lang: zh-CN
format:
  html:
    toc: true
    embed-resources: true
execute:
  echo: false
  warning: false
  message: false
---

## 分析说明

在新的 QMD 文件中编写报告内容。
`;

const importOpen = ref(false);
const importPath = ref("");
const importKind = ref("csv");
const importTable = ref("");
const importSheet = ref("");
const importSheets = ref<string[]>([]);
const importBusy = ref(false);
const sourceBusy = ref(false);
const clearDataOpen = ref(false);
const stepStates = reactive<Record<string, StepState>>({});
const presetEditorOpen = ref(false);
const presetDraft = ref<Preset | null>(null);
const presetOriginalID = ref("");
const presetParamRows = ref<Array<{ key: string; value: string }>>([]);
const presetIsNew = ref(false);
const presetTypeOptions: Array<{ value: PresetType; label: string }> = [
  { value: "datasource", label: "数据源" },
  { value: "sqlset", label: "SQL 参数集" },
  { value: "report", label: "报告模板" },
  { value: "output", label: "输出配置" },
];
const uiFontSizeOptions = [
  { value: 11, label: "小" },
  { value: 13, label: "标准" },
  { value: 15, label: "大" },
  { value: 17, label: "特大" },
];

interface AssetSchemaNode {
  name: string;
  tables: TableRef[];
}

interface AssetDatabaseNode {
  database: DatabaseRef;
  schemas: AssetSchemaNode[];
  tableCount: number;
}

const treeExpansionStorageKey = "duckdb-report-database-tree-expansion";

function loadSidebarWidth(): number {
  const value = Number(localStorage.getItem(sidebarWidthStorageKey));
  return Number.isFinite(value) && value >= 240 ? value : 264;
}

function clampSidebarWidth(width: number): number {
  const inspectorWidth = inspectorOpen.value ? 294 : 0;
  const available = window.innerWidth - 72 - inspectorWidth - 420;
  return Math.round(Math.min(Math.max(240, available), 640, Math.max(240, width)));
}

function loadTreeExpansion(): Record<string, boolean> {
  try {
    const value = JSON.parse(localStorage.getItem(treeExpansionStorageKey) ?? "{}");
    if (!value || typeof value !== "object" || Array.isArray(value)) return {};
    return value as Record<string, boolean>;
  } catch {
    return {};
  }
}

const expandedTreeNodes = ref<Record<string, boolean>>(loadTreeExpansion());

const databaseTree = computed<AssetDatabaseNode[]>(() => {
  const query = sqlFilter.value.trim().toLocaleLowerCase();
  const databases = new Map<string, AssetDatabaseNode>();

  for (const database of state.value?.databases ?? []) {
    databases.set(database.name, {
      database,
      schemas: [],
      tableCount: 0,
    });
  }

  const schemaMaps = new Map<string, Map<string, AssetSchemaNode>>();
  for (const table of tables.value) {
    let databaseNode = databases.get(table.catalog);
    if (!databaseNode) {
      databaseNode = {
        database: {
          name: table.catalog,
          path: "",
          type: "catalog",
          readOnly: false,
          isCurrent: table.catalog === "workspace",
        },
        schemas: [],
        tableCount: 0,
      };
      databases.set(table.catalog, databaseNode);
    }
    let schemaMap = schemaMaps.get(table.catalog);
    if (!schemaMap) {
      schemaMap = new Map();
      schemaMaps.set(table.catalog, schemaMap);
    }
    let schemaNode = schemaMap.get(table.schema);
    if (!schemaNode) {
      schemaNode = { name: table.schema, tables: [] };
      schemaMap.set(table.schema, schemaNode);
      databaseNode.schemas.push(schemaNode);
    }
    schemaNode.tables.push(table);
    databaseNode.tableCount += 1;
  }

  const result = [...databases.values()]
    .map((databaseNode) => {
      databaseNode.schemas.sort((left, right) => {
        if ((left.name === "main") !== (right.name === "main")) {
          return left.name === "main" ? -1 : 1;
        }
        return left.name.localeCompare(right.name, "zh-CN");
      });
      for (const schema of databaseNode.schemas) {
        schema.tables.sort((left, right) =>
          left.name.localeCompare(right.name, "zh-CN", {
            numeric: true,
            sensitivity: "base",
          }),
        );
      }
      if (!query) return databaseNode;

      const databaseText = [
        databaseNode.database.name,
        databaseNode.database.path,
        databaseNode.database.type,
      ]
        .join(" ")
        .toLocaleLowerCase();
      if (databaseText.includes(query)) return databaseNode;

      const matchingSchemas = databaseNode.schemas
        .map((schema) => {
          if (schema.name.toLocaleLowerCase().includes(query)) return schema;
          const matchingTables = schema.tables.filter((table) =>
            `${schema.name}.${table.name}`.toLocaleLowerCase().includes(query),
          );
          return matchingTables.length > 0
            ? { ...schema, tables: matchingTables }
            : null;
        })
        .filter((schema): schema is AssetSchemaNode => schema !== null);
      if (matchingSchemas.length === 0) return null;
      return {
        ...databaseNode,
        schemas: matchingSchemas,
        tableCount: matchingSchemas.reduce(
          (total, schema) => total + schema.tables.length,
          0,
        ),
      };
    })
    .filter((databaseNode): databaseNode is AssetDatabaseNode => databaseNode !== null)
    .sort((left, right) => {
      if (left.database.isCurrent !== right.database.isCurrent) {
        return left.database.isCurrent ? -1 : 1;
      }
      return left.database.name.localeCompare(right.database.name, "zh-CN");
    });

  return result;
});

function databaseNodeKey(database: DatabaseRef): string {
  return `database:${database.name}`;
}

function schemaNodeKey(database: DatabaseRef, schema: string): string {
  return `schema:${database.name}.${schema}`;
}

function isTreeNodeExpanded(key: string, fallback: boolean): boolean {
  if (sqlFilter.value.trim()) return true;
  return expandedTreeNodes.value[key] ?? fallback;
}

function isDatabaseExpanded(node: AssetDatabaseNode): boolean {
  return isTreeNodeExpanded(
    databaseNodeKey(node.database),
    node.database.isCurrent || databaseTree.value.length === 1,
  );
}

function isSchemaExpanded(database: DatabaseRef, schema: AssetSchemaNode): boolean {
  return isTreeNodeExpanded(schemaNodeKey(database, schema.name), schema.name === "main");
}

function toggleTreeNode(key: string, fallback: boolean) {
  expandedTreeNodes.value = {
    ...expandedTreeNodes.value,
    [key]: !isTreeNodeExpanded(key, fallback),
  };
  try {
    localStorage.setItem(
      treeExpansionStorageKey,
      JSON.stringify(expandedTreeNodes.value),
    );
  } catch {
    // The tree remains interactive when local storage is unavailable.
  }
}

function tableKindLabel(table: TableRef): string {
  return table.type.toUpperCase().includes("VIEW") ? "VIEW" : "TABLE";
}

function databaseSource(database: DatabaseRef): DataSource | undefined {
  return sources.value.find(
    (source) => source.kind === "duckdb" && source.table === database.name,
  );
}

function detachDatabase(database: DatabaseRef) {
  const source = databaseSource(database);
  if (!source) return;
  void removeSource(source);
}

function toggleSQLPane(pane: "editor" | "result") {
  if (pane === "editor") {
    sqlEditorCollapsed.value = !sqlEditorCollapsed.value;
    if (sqlEditorCollapsed.value && sqlResultCollapsed.value) {
      sqlResultCollapsed.value = false;
    }
    if (!sqlEditorCollapsed.value) sqlEditorRatio.value = 52;
    return;
  }
  sqlResultCollapsed.value = !sqlResultCollapsed.value;
  if (sqlResultCollapsed.value && sqlEditorCollapsed.value) {
    sqlEditorCollapsed.value = false;
  }
  if (!sqlResultCollapsed.value) sqlEditorRatio.value = 52;
}

function toggleReportPane(pane: "editor" | "preview") {
  if (pane === "editor") {
    reportEditorCollapsed.value = !reportEditorCollapsed.value;
    if (reportEditorCollapsed.value && reportPreviewCollapsed.value) {
      reportPreviewCollapsed.value = false;
    }
    if (!reportEditorCollapsed.value) reportEditorRatio.value = 46;
    return;
  }
  reportPreviewCollapsed.value = !reportPreviewCollapsed.value;
  if (reportPreviewCollapsed.value && reportEditorCollapsed.value) {
    reportEditorCollapsed.value = false;
  }
  if (!reportPreviewCollapsed.value) reportEditorRatio.value = 46;
}

function beginSplitResize(kind: "sql" | "report", event: PointerEvent) {
  if (event.button !== 0) return;
  const container = kind === "sql" ? sqlSplitRef.value : reportSplitRef.value;
  if (!container) return;
  if (
    (kind === "sql" && (sqlEditorCollapsed.value || sqlResultCollapsed.value)) ||
    (kind === "report" && (reportEditorCollapsed.value || reportPreviewCollapsed.value))
  ) {
    return;
  }
  event.preventDefault();
  stopSplitResize?.();
  const rect = container.getBoundingClientRect();
  const total = kind === "sql" ? rect.height : rect.width;
  const minFirst = kind === "sql" ? 130 : 280;
  const minSecond = kind === "sql" ? 170 : 300;
  const dividerSize = 10;

  const onMove = (moveEvent: PointerEvent) => {
    const position = kind === "sql" ? moveEvent.clientY - rect.top : moveEvent.clientX - rect.left;
    const available = Math.max(1, total - dividerSize);
    const first = Math.min(
      available - minSecond,
      Math.max(minFirst, position),
    );
    const ratio = Math.round((first / available) * 1000) / 10;
    if (kind === "sql") {
      sqlEditorRatio.value = Math.min(78, Math.max(22, ratio));
    } else {
      reportEditorRatio.value = Math.min(72, Math.max(28, ratio));
    }
  };
  const onUp = () => {
    window.removeEventListener("pointermove", onMove);
    window.removeEventListener("pointerup", onUp);
    window.removeEventListener("pointercancel", onUp);
    document.body.classList.remove("is-resizing-sql", "is-resizing-report");
    stopSplitResize = undefined;
  };
  document.body.classList.add(kind === "sql" ? "is-resizing-sql" : "is-resizing-report");
  window.addEventListener("pointermove", onMove);
  window.addEventListener("pointerup", onUp);
  window.addEventListener("pointercancel", onUp);
  stopSplitResize = onUp;
}

function nudgeSplit(kind: "sql" | "report", delta: number) {
  if (kind === "sql") {
    if (sqlEditorCollapsed.value || sqlResultCollapsed.value) return;
    sqlEditorRatio.value = Math.min(78, Math.max(22, sqlEditorRatio.value + delta));
    return;
  }
  if (reportEditorCollapsed.value || reportPreviewCollapsed.value) return;
  reportEditorRatio.value = Math.min(72, Math.max(28, reportEditorRatio.value + delta));
}

function beginSidebarResize(event: PointerEvent) {
  stopSidebarResize?.();
  const startX = event.clientX;
  const startWidth = sidebarWidth.value;
  const onMove = (moveEvent: PointerEvent) => {
    sidebarWidth.value = clampSidebarWidth(startWidth + moveEvent.clientX - startX);
  };
  const onUp = () => {
    window.removeEventListener("pointermove", onMove);
    window.removeEventListener("pointerup", onUp);
    window.removeEventListener("pointercancel", onUp);
    document.body.classList.remove("is-resizing-sidebar");
    localStorage.setItem(sidebarWidthStorageKey, String(sidebarWidth.value));
    stopSidebarResize = undefined;
  };
  document.body.classList.add("is-resizing-sidebar");
  window.addEventListener("pointermove", onMove);
  window.addEventListener("pointerup", onUp);
  window.addEventListener("pointercancel", onUp);
  stopSidebarResize = onUp;
}

function nudgeSidebar(delta: number) {
  sidebarWidth.value = clampSidebarWidth(sidebarWidth.value + delta);
  localStorage.setItem(sidebarWidthStorageKey, String(sidebarWidth.value));
}

function resetSidebarWidth() {
  sidebarWidth.value = clampSidebarWidth(264);
  localStorage.setItem(sidebarWidthStorageKey, String(sidebarWidth.value));
}

function handleWindowResize() {
  sidebarWidth.value = clampSidebarWidth(sidebarWidth.value);
}

const workflowCompleted = computed(() => {
  if (!workflow.value) return 0;
  return workflow.value.steps.filter((step) => stepStates[step.id]?.status === "success").length;
});

const latestRun = computed(() => runs.value[0] ?? null);
const brandPrimary = computed(
  () => state.value?.settings.displayName || "DuckDB Report Studio",
);
const activeDocumentLabel = computed(() => {
  if (activeView.value === "report") return selectedTemplate.value || "临时报告";
  if (activeView.value === "sql") return selectedSQL.value || "临时 SQL";
  if (activeView.value === "data" && activeTable.value) {
    return `${activeTable.value.catalog}.${activeTable.value.schema}.${activeTable.value.name}`;
  }
  return "工作区";
});
const qmdUsesR = computed(() => /```\s*\{\s*r\b/i.test(reportContent.value));
const qmdUsesPython = computed(() => /```\s*\{\s*python\b/i.test(reportContent.value));
const qmdNeedsJupyter = computed(() => qmdUsesPython.value);
const qmdDependencyError = computed(() => {
  if (qmdUsesR.value && !state.value?.environment.rPath) {
    return "当前 QMD 含 R 代码块，但未检测到 Rscript。请安装 R 运行时。";
  }
  if (qmdNeedsJupyter.value && !state.value?.environment.jupyterAvailable) {
    return "当前 QMD 含 Python 代码块，但未检测到 Jupyter。请完成 Quarto/Jupyter 配置。";
  }
  return "";
});

function clone<T>(value: T): T {
  // Workflow models are plain JSON data; JSON cloning also unwraps Vue proxies.
  return JSON.parse(JSON.stringify(value)) as T;
}

function currentStepState(step: WorkflowStep): StepState {
  return stepStates[step.id] ?? { status: "idle", message: "", duration: 0 };
}

function presetFor(step: WorkflowStep): Preset | undefined {
  return presets.value.find((preset) => preset.id === step.presetId);
}

function presetIcon(type: PresetType): string {
  if (type === "datasource") return "database";
  if (type === "sqlset") return "terminal";
  if (type === "report") return "report";
  return "file";
}

function presetTypeLabel(type: PresetType): string {
  return {
    datasource: "数据源",
    sqlset: "SQL",
    report: "报告",
    output: "输出",
  }[type];
}

function stepKindForPreset(preset: Preset): WorkflowStep["kind"] {
  if (preset.type === "datasource") return "import";
  if (preset.type === "report") return "render";
  if (preset.type === "output") return "export";
  return "sql";
}

function defaultPreset(type: PresetType): Preset {
  const params: Record<PresetType, Record<string, string>> = {
    datasource: { kind: "csv", path: "", table: "" },
    sqlset: { mode: "file", file: "", inline_sql: "", query_file: "", limit: "500" },
    report: { template: "", format: "html", query: "" },
    output: {
      filename: "report-package",
      directory: "exports",
      include_report: "true",
      include_database: "true",
      include_source: "true",
    },
  };
  return {
    id: `${type}-${Date.now().toString(36)}`,
    type,
    name: "新 Preset",
    description: "",
    params: clone(params[type]),
  };
}

function syncPresetParamRows() {
  const hiddenKeys =
    presetDraft.value?.type === "sqlset"
      ? new Set(["mode", "inline_sql"])
      : presetDraft.value?.type === "output"
        ? new Set([
            "filename",
            "directory",
            "include_report",
            "include_database",
            "include_source",
          ])
        : new Set<string>();
  presetParamRows.value = Object.entries(presetDraft.value?.params ?? {})
    .filter(([key]) => !hiddenKeys.has(key))
    .map(
    ([key, value]) => ({ key, value }),
    );
}

function openNewPreset(type: PresetType = "datasource") {
  presetIsNew.value = true;
  presetOriginalID.value = "";
  presetDraft.value = defaultPreset(type);
  syncPresetParamRows();
  presetEditorOpen.value = true;
}

function openPreset(preset?: Preset) {
  if (!preset) {
    notify("该步骤引用的 Preset 不存在", "error");
    return;
  }
  presetIsNew.value = false;
  presetOriginalID.value = preset.id;
  presetDraft.value = clone(preset);
  if (presetDraft.value.type === "sqlset") {
    presetDraft.value.params.mode ||= "file";
    presetDraft.value.params.inline_sql ||= "";
  }
  if (presetDraft.value.type === "output") {
    presetDraft.value.params.filename ||= "report-package";
    presetDraft.value.params.directory ||= "exports";
    presetDraft.value.params.include_report ||= "true";
    presetDraft.value.params.include_database ||= "true";
    presetDraft.value.params.include_source ||= "true";
  }
  syncPresetParamRows();
  presetEditorOpen.value = true;
}

function changePresetType() {
  if (!presetDraft.value) return;
  if (presetIsNew.value || presetParamRows.value.length === 0) {
    const defaults = defaultPreset(presetDraft.value.type);
    presetDraft.value.params = defaults.params;
    syncPresetParamRows();
  }
}

function addPresetParam() {
  presetParamRows.value.push({ key: "", value: "" });
}

function removePresetParam(index: number) {
  presetParamRows.value.splice(index, 1);
}

function setPresetParamValue(key: string, value: string) {
  if (!presetDraft.value) return;
  presetDraft.value.params[key] = value;
  const row = presetParamRows.value.find((item) => item.key === key);
  if (row) row.value = value;
}

function setPresetSQLMode(mode: "file" | "inline") {
  if (!presetDraft.value) return;
  presetDraft.value.params.mode = mode;
}

async function pickPresetSQLFile() {
  try {
    const path = await backend().PickSQLFile();
    if (path) setPresetParamValue("file", path);
  } catch (error) {
    notify(errorMessage(error), "error");
  }
}

async function savePreset(addAsStep = false) {
  if (!workflow.value || !presetDraft.value) return;
  const name = presetDraft.value.name.trim();
  if (!name) {
    notify("Preset 名称不能为空", "error");
    return;
  }
  const params: Record<string, string> = {};
  if (presetDraft.value.type === "sqlset") {
    params.mode = presetDraft.value.params.mode === "inline" ? "inline" : "file";
    params.inline_sql = presetDraft.value.params.inline_sql ?? "";
  }
  if (presetDraft.value.type === "output") {
    params.filename = presetDraft.value.params.filename?.trim() || "report-package";
    params.directory = presetDraft.value.params.directory?.trim() || "exports";
    params.include_report =
      presetDraft.value.params.include_report === "false" ? "false" : "true";
    params.include_database =
      presetDraft.value.params.include_database === "false" ? "false" : "true";
    params.include_source =
      presetDraft.value.params.include_source === "false" ? "false" : "true";
  }
  for (const row of presetParamRows.value) {
    const key = row.key.trim();
    if (key) params[key] = row.value;
  }
  const next: Preset = {
    ...clone(presetDraft.value),
    id: presetDraft.value.id.trim() || `${presetDraft.value.type}-${Date.now().toString(36)}`,
    name,
    params,
  };
  const duplicate = workflow.value.presets.find(
    (preset) => preset.id === next.id && preset.id !== presetOriginalID.value,
  );
  if (duplicate) {
    notify(`Preset ID「${next.id}」已存在`, "error");
    return;
  }
  const existingIndex = workflow.value.presets.findIndex(
    (preset) => preset.id === presetOriginalID.value,
  );
  if (existingIndex >= 0) {
    if (presetOriginalID.value !== next.id) {
      for (const step of workflow.value.steps) {
        if (step.presetId === presetOriginalID.value) step.presetId = next.id;
      }
    }
    workflow.value.presets.splice(existingIndex, 1, next);
  } else {
    workflow.value.presets.push(next);
  }
  await saveWorkflow(true);
  presetEditorOpen.value = false;
  notify(presetIsNew.value ? "Preset 已创建" : "Preset 已更新", "success");
  if (addAsStep) addPresetStep(next);
}

async function deletePreset() {
  if (!workflow.value) return;
  const inUse = workflow.value.steps.some(
    (step) => step.presetId === presetOriginalID.value,
  );
  if (inUse) {
    notify("该 Preset 仍被工作流步骤引用，请先移除或改用其他 Preset", "error");
    return;
  }
  workflow.value.presets = workflow.value.presets.filter(
    (preset) => preset.id !== presetOriginalID.value,
  );
  await saveWorkflow(true);
  presetEditorOpen.value = false;
  notify("Preset 已删除", "success");
}

function notify(text: string, kind: "info" | "error" | "success" = "info") {
  toast.value = { text, kind };
  if (toastTimer) window.clearTimeout(toastTimer);
  toastTimer = window.setTimeout(() => {
    toast.value = null;
  }, 4200);
}

function pushLog(level: LogEntry["level"], text: string) {
  logs.value.push({
    level,
    text,
    time: new Intl.DateTimeFormat("zh-CN", {
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    }).format(new Date()),
  });
  if (logs.value.length > 400) logs.value.splice(0, logs.value.length - 400);
}

function setTheme(next: "dark" | "light") {
  theme.value = next;
  localStorage.setItem("duckdb-report-theme", next);
  document.documentElement.dataset.theme = next;
}

function toggleTheme() {
  setTheme(theme.value === "dark" ? "light" : "dark");
}

function loadStepStates() {
  for (const step of workflow.value?.steps ?? []) {
    stepStates[step.id] = { status: "idle", message: "", duration: 0 };
  }
}

function ensureWorkflowArchitecture(): boolean {
  if (!workflow.value) return false;
  let changed = false;
  let source = workflow.value.presets.find((preset) => preset.type === "datasource");
  if (!source) {
    const connected = state.value?.sources[0] ?? {
      kind: "csv",
      path: "data/sales.csv",
      table: "sales",
    };
    source = {
      id: "source-restored",
      type: "datasource",
      name: connected.table,
      description: `恢复的基础数据连接 · ${connected.kind.toUpperCase()}`,
      params: {
        kind: connected.kind,
        path: connected.path,
        table: connected.table,
        sheet: connected.sheet ?? "",
      },
    };
    workflow.value.presets.push(source);
    changed = true;
  }
  if (!workflow.value.steps.some((step) => step.kind === "import")) {
    workflow.value.steps.unshift({
      id: "import-restored",
      name: "导入基础数据",
      kind: "import",
      presetId: source.id,
      enabled: true,
    });
    changed = true;
  }

  let sql = workflow.value.presets.find((preset) => preset.type === "sqlset");
  if (!sql) {
    sql = {
      id: "sql-restored",
      type: "sqlset",
      name: "基础 SQL 分析",
      description: "恢复的 SQL 参数集",
      params: {
        file: "sql/monthly_analysis.sql",
        query_file: "sql/monthly_result.sql",
        limit: "500",
      },
    };
    workflow.value.presets.push(sql);
    changed = true;
  }
  if (!workflow.value.steps.some((step) => step.kind === "sql")) {
    workflow.value.steps.push({
      id: "sql-restored",
      name: "执行基础分析",
      kind: "sql",
      presetId: sql.id,
      enabled: true,
    });
    changed = true;
  }

  let report = workflow.value.presets.find((preset) => preset.type === "report");
  if (!report) {
    report = {
      id: "report-restored",
      type: "report",
      name: "基础报告模板",
      description: "恢复的 Quarto 报告模板",
      params: {
        template: "examples/monthly_sales/reports/monthly_sales_python.qmd",
        format: "html",
        query: "SELECT strftime(month, '%Y-%m') AS month, region, product, orders, revenue, cost, gross_profit, gross_margin_pct FROM monthly_summary ORDER BY month, region, product",
      },
    };
    workflow.value.presets.push(report);
    changed = true;
  }
  if (!workflow.value.steps.some((step) => step.kind === "render")) {
    workflow.value.steps.push({
      id: "render-restored",
      name: "渲染基础报告",
      kind: "render",
      presetId: report.id,
      enabled: true,
    });
    changed = true;
  }

  let output = workflow.value.presets.find((preset) => preset.type === "output");
  if (!output) {
    output = {
      id: "output-restored",
      type: "output",
      name: "报告与数据发布包",
      description: "将报告、数据库快照和 QMD 源文件打包为 ZIP。",
      params: {
        filename: "report-package",
        directory: "exports",
        include_report: "true",
        include_database: "true",
        include_source: "true",
      },
    };
    workflow.value.presets.push(output);
    changed = true;
  } else {
    const migrateLegacyDefault =
      output.id === "output-default" &&
      output.params.directory === "runs" &&
      output.params.filename === undefined;
    const outputDefaults: Record<string, string> = {
      filename: "report-package",
      directory: "exports",
      include_report: "true",
      include_database: "true",
      include_source: "true",
    };
    for (const [key, value] of Object.entries(outputDefaults)) {
      if (!output.params[key]) {
        output.params[key] = value;
        changed = true;
      }
    }
    if (migrateLegacyDefault) {
      output.name = "报告与数据发布包";
      output.description = "将报告、数据库快照和 QMD 源文件打包为 ZIP。";
      output.params.directory = "exports";
      changed = true;
    }
  }
  if (!workflow.value.steps.some((step) => step.kind === "export")) {
    workflow.value.steps.push({
      id: "export-restored",
      name: "打包报告与数据",
      kind: "export",
      presetId: output.id,
      enabled: true,
    });
    changed = true;
  }
  return changed;
}

async function loadInitial(): Promise<boolean> {
  loading.value = true;
  fatalError.value = "";
  try {
    const result = await backend().Bootstrap();
    state.value = result;
    settingsDraft.value = {
      displayName: result.settings.displayName,
      workspace: result.settings.workspace,
      uiFontSize: result.settings.uiFontSize || 13,
    };
    sqlResult.value = null;
    renderResult.value = null;
    activeRunID.value = "";
    previewUrl.value = "";
    previewError.value = "";
    for (const key of Object.keys(stepStates)) delete stepStates[key];
    const availableSQLFiles = result.sqlFiles ?? [];
    const firstSQL =
      availableSQLFiles.find((file) => file.path === "examples/monthly_sales/sql/03_pivot.sql") ??
      availableSQLFiles[0];
    if (firstSQL) {
      selectedSQL.value = firstSQL.path;
      await loadSQL(firstSQL.path);
    } else {
      selectedSQL.value = "";
      sqlContent.value = "";
    }
    await loadSQLCompletions();
    const availableTemplates = result.templates ?? [];
    const firstTemplate =
      availableTemplates.find((file) => file.path === "examples/monthly_sales/reports/monthly_sales_python.qmd") ??
      availableTemplates[0];
    if (firstTemplate) {
      selectedTemplate.value = firstTemplate.path;
      await loadTemplate(firstTemplate.path);
    } else {
      selectedTemplate.value = "";
      reportContent.value = "";
    }
    if (result.runs[0]?.htmlPath) {
      previewUrl.value = `/preview/${result.runs[0].id}/report.html`;
      previewError.value = "";
      activeRunID.value = result.runs[0].id;
    }
    pushLog("success", "工作区已连接，DuckDB 会话就绪。");
    return true;
  } catch (error) {
    fatalError.value = errorMessage(error);
    pushLog("error", fatalError.value);
    return false;
  } finally {
    loading.value = false;
  }
}

async function refreshState(preserveWorkflow = true) {
  try {
    const current = preserveWorkflow ? clone(workflow.value) : undefined;
    const result = await backend().Bootstrap();
    state.value = result;
    if (!settingsDraft.value.displayName) {
      settingsDraft.value = {
        displayName: result.settings.displayName,
        workspace: result.settings.workspace,
        uiFontSize: result.settings.uiFontSize || 13,
      };
    }
    if (current) workflow.value = current;
    await loadSQLCompletions();
  } catch (error) {
    notify(errorMessage(error), "error");
  }
}

async function loadSQLCompletions() {
  try {
    const catalog = await backend().LoadSQLCompletions();
    sqlCompletionTables.value = catalog.tables ?? [];
  } catch (error) {
    pushLog("error", `SQL 补全目录加载失败：${errorMessage(error)}`);
  }
}

function currentSQLContent() {
  return sqlEditorRef.value?.getValue() ?? sqlContent.value;
}

function currentReportContent() {
  return qmdEditorRef.value?.getValue() ?? reportContent.value;
}

function tableCatalogSignature(values: TableRef[]) {
  return values
    .map((table) => `${table.catalog}.${table.schema}.${table.name}:${table.type}`)
    .join("|");
}

async function refreshDataCatalog() {
  const catalog = await backend().LoadDataCatalog();
  const previousSignature = tableCatalogSignature(tables.value);
  const changed =
    tableCatalogSignature(catalog.tables) !== previousSignature;
  if (state.value) {
    state.value.databases = catalog.databases;
    state.value.tables = catalog.tables;
    state.value.sources = catalog.sources;
  }
  if (changed) {
    await loadSQLCompletions();
  }
  return changed;
}

function scheduleSQLCompletionRefresh() {
  window.setTimeout(() => {
    void loadSQLCompletions();
  }, 200);
}

function onWorkflowEvent(event: StepEvent) {
  stepStates[event.stepId] = {
    status: event.status,
    message: event.message,
    duration: event.durationMs,
  };
  if (event.status === "running") {
    pushLog("info", `▶ ${event.stepName}`);
  } else if (event.status === "success") {
    pushLog("success", `✓ ${event.stepName} · ${event.durationMs} ms`);
  } else {
    pushLog("error", `✗ ${event.stepName} · ${event.message}`);
  }
}

async function saveWorkflow(silent = false) {
  if (!workflow.value) return;
  try {
    await backend().SaveWorkflow(workflow.value);
    if (!silent) notify("工作流已保存", "success");
  } catch (error) {
    notify(errorMessage(error), "error");
    throw error;
  }
}

async function runWorkflow() {
  if (!workflow.value || workflowBusy.value) return;
  loadStepStates();
  workflowBusy.value = true;
  logsOpen.value = true;
  pushLog("info", `开始运行「${workflow.value.name}」`);
  try {
    const result = await backend().RunWorkflow({
      workflow: workflow.value,
      params: workflow.value.runtime,
    });
    if (result.query) sqlResult.value = result.query;
    if (result.previewUrl) {
      previewUrl.value = `${result.previewUrl}?v=${Date.now()}`;
      activeRunID.value = result.runId;
      activeView.value = "report";
    }
    if (result.packagePath) {
      pushLog("success", `导出包：${result.packagePath}`);
    }
    await refreshState();
    notify(
      result.packagePath
        ? `工作流运行完成，导出包：${result.packagePath}`
        : "工作流运行完成",
      "success",
    );
  } catch (error) {
    const message = errorMessage(error);
    pushLog("error", message);
    notify(message, "error");
  } finally {
    workflowBusy.value = false;
  }
}

function addPresetStep(preset: Preset) {
  if (!workflow.value) return;
  const id = `${stepKindForPreset(preset)}-${Date.now().toString(36)}`;
  workflow.value.steps.push({
    id,
    name: preset.name,
    kind: stepKindForPreset(preset),
    presetId: preset.id,
    enabled: true,
  });
  stepStates[id] = { status: "idle", message: "", duration: 0 };
  notify(`已添加步骤：${preset.name}`);
}

function moveStep(index: number, direction: -1 | 1) {
  if (!workflow.value) return;
  const target = index + direction;
  if (target < 0 || target >= workflow.value.steps.length) return;
  const [step] = workflow.value.steps.splice(index, 1);
  workflow.value.steps.splice(target, 0, step);
}

function removeStep(index: number) {
  if (!workflow.value) return;
  const [removed] = workflow.value.steps.splice(index, 1);
  delete stepStates[removed.id];
}

async function duplicateWorkflow() {
  if (!workflow.value) return;
  workflow.value.id = `${workflow.value.id}-copy-${Date.now().toString(36)}`;
  workflow.value.name = `${workflow.value.name}（副本）`;
  await saveWorkflow(true);
  notify("已创建工作流副本", "success");
}

async function exportWorkflowPackage() {
  if (!workflow.value || workflowTransferBusy.value) return;
  workflowTransferBusy.value = true;
  try {
    await saveWorkflow(true);
    const path = await backend().ExportWorkflowPackage();
    if (path) notify(`工作流包已导出：${path}`, "success");
  } catch (error) {
    notify(errorMessage(error), "error");
  } finally {
    workflowTransferBusy.value = false;
  }
}

async function importWorkflowPackage() {
  if (workflowTransferBusy.value) return;
  workflowTransferBusy.value = true;
  try {
    const imported = await backend().ImportWorkflowPackage();
    if (!imported?.id) return;
    workflow.value = imported;
    loadStepStates();
    await refreshState();
    activeView.value = "workflow";
    notify(`已导入工作流：${imported.name}`, "success");
  } catch (error) {
    notify(errorMessage(error), "error");
  } finally {
    workflowTransferBusy.value = false;
  }
}

async function chooseDataFile() {
  try {
    const path = await backend().PickDataFile();
    if (!path) return;
    importPath.value = path;
    importKind.value = fileKind(path);
    importTable.value = fileStem(path).replace(/[^a-zA-Z0-9_\u4e00-\u9fa5]+/g, "_");
    importSheet.value = "";
    importSheets.value = [];
    if (importKind.value === "xlsx" || importKind.value === "xlsm") {
      importSheets.value = await backend().ListSheets(path);
      importSheet.value = importSheets.value[0] ?? "";
    }
    importOpen.value = true;
  } catch (error) {
    notify(errorMessage(error), "error");
  }
}

async function confirmImport() {
  if (!importTable.value.trim() || importBusy.value) return;
  importBusy.value = true;
  try {
    const imported = await backend().ImportData({
      path: importPath.value,
      kind: importKind.value,
      table: importTable.value.trim(),
      sheet: importSheet.value,
    });
    importOpen.value = false;
    await refreshState();
    notify(`已导入 ${imported.source.table}`, "success");
    for (const line of imported.log) pushLog("success", line);
  } catch (error) {
    notify(errorMessage(error), "error");
  } finally {
    importBusy.value = false;
  }
}

function applyDataCatalog(catalog: DataCatalog) {
  if (!state.value) return;
  const previousSignature = tableCatalogSignature(tables.value);
  state.value.databases = catalog.databases;
  state.value.tables = catalog.tables;
  state.value.sources = catalog.sources;
  if (tableCatalogSignature(catalog.tables) !== previousSignature) {
    void loadSQLCompletions();
  }
}

async function removeSource(source: DataSource) {
  if (sourceBusy.value) return;
  sourceBusy.value = true;
  try {
    const catalog = await backend().DeleteSource(source);
    applyDataCatalog(catalog);
    notify(`已移除数据源：${source.table}`, "success");
  } catch (error) {
    notify(errorMessage(error), "error");
  } finally {
    sourceBusy.value = false;
  }
}

async function clearWorkspaceData() {
  if (sourceBusy.value) return;
  sourceBusy.value = true;
  try {
    const catalog = await backend().ResetUserData();
    applyDataCatalog(catalog);
    sqlResult.value = null;
    clearDataOpen.value = false;
    notify("工作库中的数据已清空", "success");
  } catch (error) {
    notify(errorMessage(error), "error");
  } finally {
    sourceBusy.value = false;
  }
}

async function previewTable(table: TableRef) {
  activeView.value = "sql";
  selectedSQL.value = "";
  sqlResult.value = null;
  sqlContent.value = `SELECT *\nFROM "${table.catalog}"."${table.schema}"."${table.name}"\nLIMIT 200;`;
  await runSQL(false, sqlContent.value);
}

function openTableEditor(table: TableRef) {
  activeTable.value = table;
  activeView.value = "data";
}

function handleTableEditorSaved(result: TableWriteResult) {
  const action = result.operation === "insert" ? "新增" : "更新";
  pushLog(
    "success",
    `${action}表数据完成 · ${result.table.name} · ${result.rowsAffected} 行`,
  );
  notify(`${action}成功，受影响 ${result.rowsAffected} 行`, "success");
}

async function handleTableEditorSchemaChanged(result: TableStructureResult) {
  activeTable.value = result.table;
  notify("表结构已更新", "success");
  pushLog("success", `表结构已更新 · ${result.table.name} · ${result.operation}`);
  try {
    await refreshDataCatalog();
    await loadSQLCompletions();
  } catch (error) {
    notify(errorMessage(error), "error");
  }
}

async function loadSQL(path: string) {
  if (!path) {
    startNewSQL();
    return;
  }
  try {
    const file = await backend().ReadText(path);
    selectedSQL.value = file.path;
    sqlContent.value = file.content;
  } catch (error) {
    notify(errorMessage(error), "error");
  }
}

async function saveSQL(contentOverride?: string) {
  const content = contentOverride ?? currentSQLContent();
  if (!selectedSQL.value) {
    await saveSQLAs(content);
    return;
  }
  try {
    const saved = await backend().SaveText(selectedSQL.value, content);
    selectedSQL.value = saved.path;
    sqlContent.value = saved.content;
    notify("SQL 已保存", "success");
  } catch (error) {
    notify(errorMessage(error), "error");
  }
}

function startNewSQL() {
  selectedSQL.value = "";
  sqlResult.value = null;
  sqlContent.value = "-- 新建 SQL\nSELECT *\nFROM sales\nLIMIT 200;\n";
}

async function newSQLFile() {
  startNewSQL();
  await saveSQLAs(sqlContent.value);
}

async function saveSQLAs(contentOverride?: string) {
  const content = contentOverride ?? currentSQLContent();
  try {
    const saved = await backend().SaveSQLAs(content);
    if (!saved.path) return;
    selectedSQL.value = saved.path;
    sqlContent.value = saved.content;
    if (
      state.value &&
      !state.value.sqlFiles.some((file) => file.path === saved.path)
    ) {
      state.value.sqlFiles.push({
        name: baseName(saved.path),
        path: saved.path,
      });
    }
    notify(`SQL 已另存为：${saved.path}`, "success");
  } catch (error) {
    notify(errorMessage(error), "error");
  }
}

async function runSQL(showNotification = true, queryOverride?: string) {
  const content = queryOverride ?? currentSQLContent();
  if (sqlBusy.value || !content.trim()) return;
  sqlContent.value = content;
  sqlBusy.value = true;
  const started = performance.now();
  try {
    sqlResult.value = await backend().RunSQL(content, 500, selectedSQL.value);
    pushLog(
      "success",
      `SQL 完成 · ${sqlResult.value.rowCount} 行 · ${Math.round(performance.now() - started)} ms`,
    );
    if (showNotification) notify("查询执行完成", "success");
  } catch (error) {
    const message = errorMessage(error);
    pushLog("error", message);
    notify(message, "error");
  } finally {
    sqlBusy.value = false;
    try {
      // DDL and scripts can change the catalog even when a later statement fails.
      const catalogChanged = await refreshDataCatalog();
      if (!catalogChanged) scheduleSQLCompletionRefresh();
    } catch (error) {
      pushLog("error", `数据目录刷新失败：${errorMessage(error)}`);
    }
  }
}

function runSQLFromEditor(query: string) {
  void runSQL(true, query);
}

function explainSelectedSQL() {
  if (!selectedSQL.value) {
    return "临时查询尚未绑定文件；保存时会打开“另存为”窗口。";
  }
  const file = selectedSQL.value.toLowerCase();
  if (file.includes("quality")) return "数据质量检查：逐行标记空值、类型与口径异常。";
  if (file.includes("validation")) return "验收 SQL：对关键行数和金额平衡执行断言。";
  if (file.includes("publish")) return "发布层：将分析视图物化为稳定结果表。";
  if (file.includes("pivot")) return "透视层：使用 GROUPING SETS 生成小计与总计。";
  return "SQL 中的模板变量会先按当前工作区参数展开，再交给 DuckDB 执行。";
}

async function loadTemplate(path: string) {
  if (!path) {
    startNewQMD();
    return;
  }
  try {
    const file = await backend().ReadText(path);
    selectedTemplate.value = file.path;
    reportContent.value = file.content;
    previewUrl.value = "";
    previewError.value = "";
    renderResult.value = null;
    activeRunID.value = "";
  } catch (error) {
    const message = errorMessage(error);
    previewError.value = message;
    notify(message, "error");
  }
}

function startNewQMD() {
  selectedTemplate.value = "";
  reportContent.value = newQMDContent;
  previewUrl.value = "";
  previewError.value = "";
  renderResult.value = null;
  activeRunID.value = "";
}

async function newQMDFile() {
  startNewQMD();
  await saveQMDAs(reportContent.value);
}

async function renderReport(
  format: "html" | "pdf",
  contentOverride?: string,
) {
  const content = contentOverride ?? currentReportContent();
  if (!content.trim() || reportBusy.value) return;
  reportContent.value = content;
  if (qmdDependencyError.value) {
    notify(qmdDependencyError.value, "error");
    return;
  }
  reportBusy.value = true;
  reportFormat.value = format;
  reportBusyLabel.value = format === "pdf" ? "正在生成 PDF…" : "正在生成 HTML…";
  previewUrl.value = "";
  previewError.value = "";
  renderResult.value = null;
  activeRunID.value = "";
  logsOpen.value = true;
  try {
    const result = await backend().RenderReport({
      content,
      format,
      query: "",
      params: {},
      saveAs: selectedTemplate.value ? fileStem(selectedTemplate.value) : "analysis-report",
      templatePath: selectedTemplate.value,
    });
    renderResult.value = result;
    previewUrl.value = `${result.previewUrl}?v=${Date.now()}`;
    activeRunID.value = result.runId;
    previewError.value = "";
    for (const line of result.log) pushLog("info", line);
    await refreshState();
    notify(format === "pdf" ? "PDF 已生成" : "HTML 报告已生成", "success");
  } catch (error) {
    const message = errorMessage(error);
    previewUrl.value = "";
    previewError.value = message;
    pushLog("error", message);
    notify(message, "error");
  } finally {
    reportBusy.value = false;
  }
}

function renderReportFromEditor(format: "html" | "pdf", content: string) {
  void renderReport(format, content);
}

async function saveTemplate(contentOverride?: string) {
  const content = contentOverride ?? currentReportContent();
  if (!selectedTemplate.value) {
    await saveQMDAs(content);
    return;
  }
  try {
    const saved = await backend().SaveText(selectedTemplate.value, content);
    selectedTemplate.value = saved.path;
    reportContent.value = saved.content;
    notify("QMD 已保存", "success");
  } catch (error) {
    notify(errorMessage(error), "error");
  }
}

async function saveQMDAs(contentOverride?: string) {
  const content = contentOverride ?? currentReportContent();
  try {
    const saved = await backend().SaveQMDAs(content);
    if (!saved.path) return;
    selectedTemplate.value = saved.path;
    reportContent.value = saved.content;
    if (
      state.value &&
      !state.value.templates.some((file) => file.path === saved.path)
    ) {
      state.value.templates.push({
        name: baseName(saved.path),
        path: saved.path,
      });
    }
    notify(`QMD 已另存为：${saved.path}`, "success");
  } catch (error) {
    notify(errorMessage(error), "error");
  }
}

async function chooseWorkspaceDirectory() {
  if (settingsBusy.value) return;
  if (state.value?.settings.workspaceOverride) {
    notify("当前工作区由 DUCKDB_REPORT_HOME 环境变量控制", "error");
    return;
  }
  try {
    const path = await backend().PickWorkspaceDirectory();
    if (path) {
      settingsDraft.value.workspace = path;
      await saveSettings();
    }
  } catch (error) {
    notify(errorMessage(error), "error");
  }
}

async function saveSettings() {
  if (settingsBusy.value) return;
  settingsBusy.value = true;
  try {
    const previousWorkspace = state.value?.settings.currentWorkspace ?? "";
    const saved = await backend().SaveSettings(settingsDraft.value);
    if (state.value) state.value.settings = saved;
    settingsDraft.value = {
      displayName: saved.displayName,
      workspace: saved.workspace,
      uiFontSize: saved.uiFontSize || 13,
    };
    if (previousWorkspace && saved.currentWorkspace !== previousWorkspace) {
      const reloaded = await loadInitial();
      notify(
        reloaded
          ? "工作区已切换，文件和数据库均已重新加载"
          : "工作区已切换，但界面刷新失败",
        reloaded ? "success" : "error",
      );
    } else {
      notify("设置已保存", "success");
    }
  } catch (error) {
    notify(errorMessage(error), "error");
  } finally {
    settingsBusy.value = false;
  }
}

function releaseVersion(value: string | undefined): string {
  if (!value) return "未知";
  return value.startsWith("v") ? value : `v${value}`;
}

function formatBytes(value: number): string {
  if (!value) return "大小未知";
  if (value >= 1024 * 1024) return `${(value / 1024 / 1024).toFixed(1)} MB`;
  if (value >= 1024) return `${Math.round(value / 1024)} KB`;
  return `${value} B`;
}

function formatReleaseDate(value: string): string {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(date);
}

async function checkForUpdates(showFeedback = true) {
  if (updateBusy.value) return;
  updateBusy.value = true;
  updateError.value = "";
  try {
    const previousLatest = updateInfo.value?.latestVersion;
    const info = await backend().CheckForUpdates();
    updateInfo.value = info;
    if (previousLatest !== info.latestVersion) {
      updateDownloaded.value = null;
    }
    if (info.available) {
      pushLog("info", `发现新版本 ${releaseVersion(info.latestVersion)}。`);
      if (showFeedback) {
        notify(`发现新版本 ${releaseVersion(info.latestVersion)}`, "success");
      }
    } else if (showFeedback) {
      notify("当前已是最新正式版本", "success");
    }
  } catch (error) {
    updateError.value = errorMessage(error);
    if (showFeedback) {
      notify(`检查更新失败：${updateError.value}`, "error");
    }
  } finally {
    updateBusy.value = false;
  }
}

async function downloadUpdate() {
  if (updateDownloadBusy.value || !updateInfo.value?.available) return;
  updateDownloadBusy.value = true;
  updateError.value = "";
  try {
    const download = await backend().DownloadUpdate();
    updateDownloaded.value = download;
    notify("更新包已下载并通过 SHA-256 校验", "success");
  } catch (error) {
    updateError.value = errorMessage(error);
    notify(`下载更新失败：${updateError.value}`, "error");
  } finally {
    updateDownloadBusy.value = false;
  }
}

async function openUpdateRelease() {
  try {
    await backend().OpenUpdateRelease();
  } catch (error) {
    notify(errorMessage(error), "error");
  }
}

async function openDownloadedUpdate() {
  try {
    await backend().OpenDownloadedUpdate();
  } catch (error) {
    notify(errorMessage(error), "error");
  }
}

async function openWorkspaceFolder() {
  try {
    await backend().OpenWorkspace();
  } catch (error) {
    notify(errorMessage(error), "error");
  }
}

async function exportWorkspaceDuckDB() {
  if (exportWorkspaceBusy.value) return;
  exportWorkspaceBusy.value = true;
  try {
    const path = await backend().ExportWorkspaceDuckDB();
    if (path) notify(`工作库已导出：${path}`, "success");
  } catch (error) {
    notify(errorMessage(error), "error");
  } finally {
    exportWorkspaceBusy.value = false;
  }
}

async function openLatestOutput() {
  const runID = activeRunID.value || renderResult.value?.runId || latestRun.value?.id;
  if (!runID) {
    notify("当前没有可打开的产物", "error");
    return;
  }
  try {
    await backend().OpenOutput(runID, reportFormat.value);
  } catch (error) {
    notify(errorMessage(error), "error");
  }
}

async function downloadOutput() {
  const runID = activeRunID.value || renderResult.value?.runId || latestRun.value?.id;
  if (!runID) {
    notify("当前没有可下载的产物", "error");
    return;
  }
  try {
    const path = await backend().SaveOutput(runID, reportFormat.value);
    if (path) notify(`文件已保存：${path}`, "success");
  } catch (error) {
    notify(errorMessage(error), "error");
  }
}

function openRun(run: NonNullable<Bootstrap["runs"]>[number]) {
  if (!run.htmlPath && !run.pdfPath) return;
  reportFormat.value = run.htmlPath ? "html" : "pdf";
  activeRunID.value = run.id;
  previewError.value = "";
  previewUrl.value = `/preview/${run.id}/report.${reportFormat.value}?v=${Date.now()}`;
  activeView.value = "report";
}

watch(
  () => settingsDraft.value.uiFontSize,
  (size) => {
    const normalized = Math.min(17, Math.max(11, size || 13));
    document.documentElement.style.setProperty(
      "--ui-font-scale",
      String(normalized / 13),
    );
  },
  { immediate: true },
);

onMounted(async () => {
  const savedTheme = localStorage.getItem("duckdb-report-theme");
  const preferredDark = window.matchMedia("(prefers-color-scheme: dark)").matches;
  setTheme(savedTheme === "light" || savedTheme === "dark" ? savedTheme : preferredDark ? "dark" : "light");
  window.addEventListener("resize", handleWindowResize);
  const loaded = await loadInitial();
  if (loaded) {
    void checkForUpdates(false);
    updatePollTimer = window.setInterval(
      () => void checkForUpdates(false),
      6 * 60 * 60 * 1000,
    );
  }
});

onBeforeUnmount(() => {
  stopSplitResize?.();
  stopSidebarResize?.();
  if (updatePollTimer) window.clearInterval(updatePollTimer);
  if (toastTimer) window.clearTimeout(toastTimer);
  window.removeEventListener("resize", handleWindowResize);
});
</script>

<template>
  <div class="app-shell" :data-theme="theme">
    <header class="titlebar">
      <div class="brand">
        <img class="brand-icon" :src="appIcon" alt="" aria-hidden="true" />
        <div class="brand-copy">
          <strong>{{ brandPrimary }}</strong>
        </div>
      </div>
      <div class="titlebar-breadcrumb">
        <span>{{ activeDocumentLabel }}</span>
        <AppIcon name="chevron" :size="13" />
        <strong>{{
          activeView === "data"
            ? "表编辑"
            : activeView === "sql"
              ? "SQL 分析"
              : activeView === "settings"
                ? "设置"
                : "报告生成"
        }}</strong>
      </div>
      <div class="titlebar-actions">
        <div class="runtime-pill" :class="{ ready: state?.environment.quartoPath }">
          <span class="pulse"></span>
          DuckDB {{ state?.environment.duckdbVersion || "…" }}
        </div>
        <button class="icon-button" title="切换主题" @click="toggleTheme">
          <AppIcon :name="theme === 'dark' ? 'sun' : 'moon'" />
        </button>
        <button
          class="icon-button"
          :class="{ active: inspectorOpen }"
          title="显示检查器"
          @click="inspectorOpen = !inspectorOpen"
        >
          <AppIcon name="settings" />
        </button>
      </div>
    </header>

    <div v-if="loading" class="loading-screen">
      <div class="duck-loader">
        <span></span><span></span><span></span>
      </div>
      <p>正在连接本地分析工作区…</p>
    </div>

    <div v-else-if="fatalError" class="fatal-screen">
      <div class="fatal-card">
        <span class="eyebrow">STARTUP ERROR</span>
        <h1>工作区未能启动</h1>
        <pre>{{ fatalError }}</pre>
        <button class="primary-button" @click="loadInitial">重新连接</button>
      </div>
    </div>

    <div
      v-else
      class="app-body"
      :style="{ '--sidebar-width': `${sidebarWidth}px` }"
      :class="{
        'logs-visible': logsOpen,
        'settings-mode': activeView === 'settings',
        'inspector-hidden': !inspectorOpen || activeView === 'settings',
      }"
    >
      <nav class="activity-rail">
        <button
          v-for="item in [
            { id: 'data', icon: 'table', label: '数据' },
            { id: 'sql', icon: 'terminal', label: 'SQL' },
            { id: 'report', icon: 'report', label: '报告' },
          ]"
          :key="item.id"
          class="rail-button"
          :class="{ active: activeView === item.id }"
          :title="item.label"
          @click="activeView = item.id as ViewName"
        >
          <AppIcon :name="item.icon" :size="20" />
          <span>{{ item.label }}</span>
        </button>
        <div class="rail-spacer"></div>
        <button class="rail-button" title="运行日志" @click="logsOpen = !logsOpen">
          <span class="terminal-glyph">&gt;_</span>
          <span>日志</span>
        </button>
        <button
          class="rail-button"
          :class="{
            active: activeView === 'settings',
            'update-available': updateInfo?.available,
          }"
          :title="
            updateInfo?.available
              ? `设置 · 新版本 ${releaseVersion(updateInfo.latestVersion)} 可用`
              : '设置'
          "
          @click="activeView = 'settings'"
        >
          <AppIcon name="settings" :size="19" />
          <span>设置</span>
          <span
            v-if="updateInfo?.available"
            class="rail-update-badge"
            :aria-label="`新版本 ${releaseVersion(updateInfo.latestVersion)} 可用`"
          >
            1
          </span>
        </button>
      </nav>

      <aside class="sidebar">
        <div class="sidebar-heading">
          <div>
            <span class="eyebrow">EXPLORER</span>
            <h2>数据与分析资产</h2>
          </div>
          <button class="icon-button compact" title="刷新" @click="refreshState()">
            <AppIcon name="refresh" :size="15" />
          </button>
        </div>

        <section class="workspace-nav" aria-label="工作区操作">
          <div class="workspace-nav-head">
            <div>
              <span class="eyebrow">WORKSPACE</span>
              <strong>当前工作区</strong>
            </div>
            <button
              type="button"
              class="icon-button compact"
              title="打开当前工作区目录"
              aria-label="打开当前工作区目录"
              @click="openWorkspaceFolder"
            >
              <AppIcon name="external" :size="14" />
            </button>
          </div>
          <code
            class="workspace-nav-path"
            :title="state?.settings.currentWorkspace"
          >
            {{ state?.settings.currentWorkspace }}
          </code>
          <div class="workspace-nav-actions">
            <button
              class="ghost-button small"
              :disabled="state?.settings.workspaceOverride || settingsBusy"
              @click="chooseWorkspaceDirectory"
            >
              <AppIcon name="file" :size="14" />
              切换工作区
            </button>
            <button
              class="ghost-button small"
              :disabled="exportWorkspaceBusy"
              @click="exportWorkspaceDuckDB"
            >
              <AppIcon name="download" :size="14" />
              {{ exportWorkspaceBusy ? "导出中…" : "导出数据库" }}
            </button>
          </div>
          <p v-if="state?.settings.workspaceOverride" class="workspace-nav-note">
            当前目录由 <code>DUCKDB_REPORT_HOME</code> 控制
          </p>
        </section>

        <section class="side-section">
          <div class="section-title">
            <span>数据文件与数据库</span>
            <div class="section-actions">
              <button
                v-if="sources.length"
                class="text-button danger-text"
                :disabled="sourceBusy"
                @click="clearDataOpen = true"
              >
                清空工作库
              </button>
              <button class="text-button" @click="chooseDataFile">
                <AppIcon name="plus" :size="14" /> 导入 / 连接
              </button>
            </div>
          </div>
          <p class="side-help">按 database → schema → table/view 浏览；单击表进入表编辑器。</p>
          <label class="search-field">
            <AppIcon name="search" :size="14" />
            <input v-model="sqlFilter" type="search" placeholder="筛选数据库、schema 或表" />
          </label>
          <div v-if="sources.length" class="source-list">
            <div v-for="source in sources.slice(0, 4)" :key="`${source.kind}-${source.table}`" class="source-chip">
              <span>{{ source.kind.toUpperCase() }}</span>
              <strong>{{ source.table }}</strong>
              <button
                type="button"
                class="source-remove"
                :disabled="sourceBusy"
                :title="`移除 ${source.table}`"
                @click="removeSource(source)"
              >
                ×
              </button>
            </div>
          </div>
          <div class="database-tree">
            <div
              v-for="databaseNode in databaseTree"
              :key="databaseNode.database.name"
              class="database-node"
            >
              <div class="database-row">
                <button
                  type="button"
                  class="tree-row database-tree-row"
                  :title="databaseNode.database.path || databaseNode.database.name"
                  @click="toggleTreeNode(
                    databaseNodeKey(databaseNode.database),
                    databaseNode.database.isCurrent || databaseTree.length === 1,
                  )"
                >
                  <span
                    class="tree-chevron"
                    :class="{ expanded: isDatabaseExpanded(databaseNode) }"
                  >
                    <AppIcon name="chevron" :size="11" />
                  </span>
                  <AppIcon name="database" :size="15" />
                  <span class="tree-node-name">{{ databaseNode.database.name }}</span>
                  <span
                    v-if="databaseNode.database.isCurrent"
                    class="tree-badge local"
                  >LOCAL</span>
                  <span v-else class="tree-badge attached">ATTACHED</span>
                  <span
                    v-if="databaseNode.database.readOnly"
                    class="tree-badge readonly"
                  >READ ONLY</span>
                  <small>{{ databaseNode.tableCount }}</small>
                </button>
                <button
                  v-if="databaseSource(databaseNode.database)"
                  type="button"
                  class="tree-disconnect"
                  :disabled="sourceBusy"
                  :title="`断开 ${databaseNode.database.name}`"
                  @click="detachDatabase(databaseNode.database)"
                >
                  <AppIcon name="trash" :size="12" />
                </button>
              </div>

              <div v-if="isDatabaseExpanded(databaseNode)" class="tree-children database-children">
                <div
                  v-for="schema in databaseNode.schemas"
                  :key="`${databaseNode.database.name}.${schema.name}`"
                  class="schema-node"
                >
                  <button
                    type="button"
                    class="tree-row schema-tree-row"
                    @click="toggleTreeNode(
                      schemaNodeKey(databaseNode.database, schema.name),
                      schema.name === 'main',
                    )"
                  >
                    <span
                      class="tree-chevron"
                      :class="{
                        expanded: isSchemaExpanded(databaseNode.database, schema),
                      }"
                    >
                      <AppIcon name="chevron" :size="10" />
                    </span>
                    <span class="schema-branch-dot"></span>
                    <span class="tree-node-name">{{ schema.name }}</span>
                    <small>{{ schema.tables.length }}</small>
                  </button>

                  <div
                    v-if="isSchemaExpanded(databaseNode.database, schema)"
                    class="tree-children schema-children"
                  >
                    <button
                      v-for="table in schema.tables"
                      :key="`${table.catalog}.${table.schema}.${table.name}`"
                      type="button"
                      class="tree-row table-row"
                      :title="`${table.catalog}.${table.schema}.${table.name}`"
                      @click="openTableEditor(table)"
                    >
                      <AppIcon name="table" :size="14" />
                      <span class="tree-node-name">{{ table.name }}</span>
                      <small>{{ tableKindLabel(table) }}</small>
                    </button>
                  </div>
                </div>
                <div
                  v-if="databaseNode.schemas.length === 0"
                  class="sidebar-empty compact"
                >
                  此数据库暂无可用表
                </div>
              </div>
            </div>
            <div v-if="databaseTree.length === 0" class="sidebar-empty">
              {{ sqlFilter.trim() ? "没有匹配的数据库或表" : "尚未接入数据表" }}
            </div>
          </div>
        </section>

        <section class="side-section analysis-flow-section">
          <div class="section-title">
            <span>分析路径</span>
          </div>
          <ol class="analysis-flow">
            <li>
              <span>01</span>
              <div><strong>打开 SQL</strong><small>选择或新建源文件，确认输入表与结果表。</small></div>
            </li>
            <li>
              <span>02</span>
              <div><strong>手工运行</strong><small>逐段检查数据、质量问题和物化结果表。</small></div>
            </li>
            <li>
              <span>03</span>
              <div><strong>生成报告</strong><small>在 QMD 中编写分析代码，渲染 HTML 或 PDF。</small></div>
            </li>
          </ol>
        </section>

        <div class="sidebar-footer">
          <span>WORKSPACE</span>
          <code>{{ state?.database }}</code>
        </div>
      </aside>

      <div
        class="sidebar-resizer"
        role="separator"
        aria-label="调整数据与分析资产栏宽度"
        aria-orientation="vertical"
        :aria-valuenow="sidebarWidth"
        aria-valuemin="240"
        aria-valuemax="640"
        tabindex="0"
        title="拖动调整宽度，双击恢复默认"
        @pointerdown.prevent="beginSidebarResize"
        @keydown.left.prevent="nudgeSidebar(-20)"
        @keydown.right.prevent="nudgeSidebar(20)"
        @dblclick="resetSidebarWidth"
      ></div>

      <main class="main-workspace">
        <TableEditorPage
          v-if="activeView === 'data'"
          :tables="sqlCompletionTables"
          :databases="state?.databases ?? []"
          :active-table="activeTable"
          @saved="handleTableEditorSaved"
          @schema-changed="handleTableEditorSchemaChanged"
          @select-table="activeTable = $event"
        />

        <section v-else-if="activeView === 'workflow'" class="view-panel">
          <div class="page-head">
            <div>
              <span class="eyebrow">COMPOSABLE PIPELINE</span>
              <div class="workflow-title-row">
                <input
                  v-model="workflow!.name"
                  class="workflow-title-input"
                  aria-label="工作流标题"
                  placeholder="工作流标题"
                />
                <span class="editable-hint">可直接编辑</span>
              </div>
              <textarea
                v-model="workflow!.description"
                class="workflow-description-input"
                aria-label="工作流说明"
                rows="2"
                placeholder="说明这条工作流的目标和口径"
              ></textarea>
            </div>
            <div class="page-actions">
              <button class="ghost-button" :disabled="workflowTransferBusy" @click="importWorkflowPackage">
                <AppIcon name="download" :size="15" /> 导入工作流
              </button>
              <button class="ghost-button" :disabled="workflowTransferBusy" @click="exportWorkflowPackage">
                <AppIcon name="file" :size="15" /> 导出工作流包
              </button>
              <button class="ghost-button" @click="duplicateWorkflow">
                <AppIcon name="file" :size="15" /> 创建副本
              </button>
              <button class="ghost-button" @click="saveWorkflow()">
                <AppIcon name="save" :size="15" /> 保存
              </button>
              <button class="primary-button" :disabled="workflowBusy" @click="runWorkflow">
                <AppIcon name="play" :size="15" />
                {{ workflowBusy ? "正在运行…" : "运行工作流" }}
              </button>
            </div>
          </div>

          <div class="workflow-summary">
            <div class="summary-main">
              <span class="status-orb" :class="{ running: workflowBusy }"></span>
              <div>
                <small>{{ workflowBusy ? "RUNNING" : "READY" }}</small>
                <strong>{{ workflowBusy ? "数据管线正在执行" : "工作流已就绪" }}</strong>
              </div>
            </div>
            <div class="summary-stat">
              <strong>{{ workflowCompleted }}/{{ workflow?.steps.length }}</strong>
              <span>步骤完成</span>
            </div>
            <div class="summary-stat">
              <strong>{{ workflow?.presets.length }}</strong>
              <span>可复用参数</span>
            </div>
            <div class="summary-stat">
              <strong>{{ sources.length }}</strong>
              <span>数据连接</span>
            </div>
          </div>

          <div class="workflow-layout">
            <section class="panel workflow-canvas">
              <div class="panel-head">
                <div>
                  <span class="eyebrow">STEPS</span>
                  <h3>执行画布</h3>
                </div>
                <span class="panel-hint">参数优先级：运行参数 &gt; 步骤覆盖 &gt; Preset</span>
              </div>

              <div class="step-list">
                <article
                  v-for="(step, index) in workflow?.steps"
                  :key="step.id"
                  class="step-card"
                  :class="[
                    `is-${currentStepState(step).status}`,
                    { disabled: !step.enabled },
                  ]"
                >
                  <div class="step-index">{{ String(index + 1).padStart(2, "0") }}</div>
                  <div class="step-icon">
                    <AppIcon
                      :name="step.kind === 'import' ? 'database' : step.kind === 'sql' ? 'terminal' : step.kind === 'render' ? 'report' : 'file'"
                      :size="19"
                    />
                  </div>
                  <div class="step-content">
                    <div class="step-title-row">
                      <input v-model="step.name" class="inline-title" aria-label="步骤名称" />
                      <span class="kind-chip">{{ step.kind }}</span>
                    </div>
                    <p>{{ presetFor(step)?.description }}</p>
                    <div class="step-meta">
                      <span>{{ presetFor(step)?.name }}</span>
                      <button class="step-edit-link" @click="openPreset(presetFor(step))">
                        {{ Object.keys(presetFor(step)?.params ?? {}).length }} 个参数 · 编辑
                      </button>
                      <span
                        v-if="currentStepState(step).status !== 'idle'"
                        class="step-status"
                        :class="currentStepState(step).status"
                      >
                        {{ currentStepState(step).status === "running" ? "运行中" : currentStepState(step).status === "success" ? "成功" : "失败" }}
                        <template v-if="currentStepState(step).duration">
                          · {{ currentStepState(step).duration }} ms
                        </template>
                      </span>
                    </div>
                  </div>
                  <div class="step-controls">
                    <button class="mini-icon" title="编辑 Preset" @click="openPreset(presetFor(step))">
                      <AppIcon name="settings" :size="14" />
                    </button>
                    <label class="switch" title="启用步骤">
                      <input v-model="step.enabled" type="checkbox" />
                      <span></span>
                    </label>
                    <button class="mini-icon" :disabled="index === 0" @click="moveStep(index, -1)">
                      <AppIcon name="up" :size="14" />
                    </button>
                    <button class="mini-icon" :disabled="index === workflow!.steps.length - 1" @click="moveStep(index, 1)">
                      <AppIcon name="down" :size="14" />
                    </button>
                    <button class="mini-icon danger" @click="removeStep(index)">
                      <AppIcon name="trash" :size="14" />
                    </button>
                  </div>
                </article>
              </div>

              <button class="add-step-button" @click="notify('从左侧 Preset 库选择一个组件即可添加步骤')">
                <AppIcon name="plus" :size="16" />
                从左侧 Preset 库组合新步骤
              </button>
            </section>

            <section class="panel runtime-panel">
              <div class="panel-head">
                <div>
                  <span class="eyebrow">RUN PARAMETERS</span>
                  <h3>本次运行参数</h3>
                </div>
              </div>
              <div class="runtime-form">
                <label v-for="(value, key) in workflow?.runtime" :key="key">
                  <span>{{ key }}</span>
                  <input v-model="workflow!.runtime[key]" :type="String(value).match(/^\d{4}-\d{2}-\d{2}$/) ? 'date' : 'text'" />
                </label>
              </div>
              <div class="parameter-note">
                <strong>一个 Workflow = 一套分析与报告</strong>
                <p>Preset 固化口径，步骤覆盖本次差异，运行参数可继续覆盖。保存后可由 Agent 读取或生成新的工作流文件。</p>
              </div>
            </section>
          </div>
        </section>

        <section v-else-if="activeView === 'sql'" class="view-panel sql-view">
          <div class="page-head compact-head">
            <div>
              <span class="eyebrow">SQL WORKBENCH</span>
              <h1>SQL 分析</h1>
            </div>
            <div class="page-actions">
              <select v-model="selectedSQL" class="select-control" @change="loadSQL(selectedSQL)">
                <option value="">临时查询（未保存）</option>
                <option v-for="file in sqlFiles" :key="file.path" :value="file.path">{{ file.name }}</option>
              </select>
              <button class="ghost-button" @click="newSQLFile">
                <AppIcon name="plus" :size="15" /> 新建 SQL
              </button>
              <button class="ghost-button" @click="saveSQL()">
                <AppIcon name="save" :size="15" /> 保存
              </button>
              <button class="ghost-button" @click="saveSQLAs()">
                <AppIcon name="file" :size="15" /> 另存为
              </button>
              <button class="primary-button" :disabled="sqlBusy" @click="runSQL()">
                <AppIcon name="play" :size="15" />
                {{ sqlBusy ? "执行中…" : "运行" }}
              </button>
            </div>
          </div>

          <div
            ref="sqlSplitRef"
            class="sql-split-layout"
            :class="{ 'editor-only': sqlResultCollapsed }"
          >
            <section
              v-if="!sqlEditorCollapsed"
              class="editor-card sql-editor-card"
              :style="{ flexBasis: `${sqlEditorRatio}%` }"
            >
              <div class="editor-topline">
                <span :class="{ 'scratch-document': !selectedSQL }">{{ sqlDisplayName }}</span>
                <div class="pane-head-actions">
                  <span>{{ explainSelectedSQL() }}</span>
                  <button type="button" class="text-button" @click="toggleSQLPane('editor')">
                    收起
                  </button>
                </div>
              </div>
              <SqlEditor
                ref="sqlEditorRef"
                v-model="sqlContent"
                :tables="sqlCompletionTables"
                :busy="sqlBusy"
                @run="runSQLFromEditor"
                @save="saveSQL"
                @save-as="saveSQLAs"
              />
            </section>
            <button
              v-else
              type="button"
              class="pane-collapsed-strip horizontal"
              @click="toggleSQLPane('editor')"
            >
              <AppIcon name="terminal" :size="14" />
              展开 SQL 编辑器
            </button>

            <div
              v-if="!sqlEditorCollapsed && !sqlResultCollapsed"
              class="split-divider horizontal"
              role="separator"
              aria-label="调整 SQL 编辑器与查询结果高度"
              aria-orientation="horizontal"
              tabindex="0"
              @pointerdown="beginSplitResize('sql', $event)"
              @keydown.up.prevent="nudgeSplit('sql', -4)"
              @keydown.down.prevent="nudgeSplit('sql', 4)"
            >
              <span></span>
            </div>

            <section v-if="!sqlResultCollapsed" class="result-card sql-result-card">
              <div class="result-header">
                <div>
                  <span class="eyebrow">RESULT SET</span>
                  <h3>查询结果</h3>
                </div>
                <div class="pane-head-actions">
                  <button v-if="sqlResult" class="text-button" @click="sqlResult = null">清空</button>
                  <button type="button" class="text-button" @click="toggleSQLPane('result')">
                    收起
                  </button>
                </div>
              </div>
              <ResultGrid :result="sqlResult" />
            </section>
            <button
              v-else
              type="button"
              class="pane-collapsed-strip horizontal"
              @click="toggleSQLPane('result')"
            >
              <AppIcon name="table" :size="14" />
              展开查询结果
            </button>
          </div>
        </section>

        <section v-else-if="activeView === 'report'" class="view-panel report-view">
          <div class="page-head compact-head">
            <div>
              <span class="eyebrow">QUARTO COMPOSER</span>
              <h1>报告生成</h1>
            </div>
            <div class="page-actions">
              <select v-model="selectedTemplate" class="select-control wide" @change="loadTemplate(selectedTemplate)">
                <option value="">临时报告（未保存）</option>
                <option v-for="file in templates" :key="file.path" :value="file.path">{{ file.path }}</option>
              </select>
              <button class="ghost-button" @click="newQMDFile">
                <AppIcon name="plus" :size="15" /> 新建 QMD
              </button>
              <button class="ghost-button" @click="saveTemplate()">
                <AppIcon name="save" :size="15" /> 保存
              </button>
              <button class="ghost-button" @click="saveQMDAs()">
                <AppIcon name="file" :size="15" /> 另存为
              </button>
              <button class="secondary-button" :disabled="reportBusy || !!qmdDependencyError" :title="qmdDependencyError" @click="renderReport('pdf')">
                PDF
              </button>
              <button class="primary-button" :disabled="reportBusy || !!qmdDependencyError" :title="qmdDependencyError" @click="renderReport('html')">
                <AppIcon name="play" :size="15" />
                {{ reportBusy && reportFormat === "html" ? "生成中…" : "渲染 HTML" }}
              </button>
            </div>
          </div>

          <div class="qmd-runtime-strip" :class="{ blocked: qmdDependencyError }">
            <div class="qmd-runtime-copy">
              <span class="runtime-badge">QMD</span>
              <p v-if="qmdDependencyError">{{ qmdDependencyError }}</p>
              <p v-else-if="qmdUsesR">检测到 R 代码块，将使用 {{ state?.environment.rVersion || "Rscript" }} 执行。</p>
              <p v-else-if="qmdUsesPython">检测到 Python 代码块，将使用 Python + Jupyter 执行。</p>
              <p v-else>当前模板没有 R/Python 代码块；正文可直接渲染。数据分析代码应直接写在 QMD 中。</p>
            </div>
            <span v-if="qmdUsesR" class="runtime-chip" :class="{ success: state?.environment.rPath }">R</span>
            <span v-if="qmdUsesPython" class="runtime-chip" :class="{ success: state?.environment.jupyterAvailable }">Python / Jupyter</span>
          </div>

          <div
            ref="reportSplitRef"
            class="report-layout"
            :class="{ 'editor-only': reportPreviewCollapsed }"
          >
            <section
              v-if="!reportEditorCollapsed"
              class="editor-card report-editor"
              :style="{ flexBasis: `${reportEditorRatio}%` }"
            >
              <div class="editor-topline">
                <span :class="{ 'scratch-document': !selectedTemplate }">
                  {{ selectedTemplate || "临时 QMD（未保存）" }}
                </span>
                <div class="pane-head-actions">
                  <span>{{ reportContent.split("\n").length }} lines</span>
                  <button type="button" class="text-button" @click="toggleReportPane('editor')">
                    收起
                  </button>
                </div>
              </div>
              <QmdEditor
                ref="qmdEditorRef"
                v-model="reportContent"
                :busy="reportBusy"
                @render="renderReportFromEditor"
                @save="saveTemplate"
                @save-as="saveQMDAs"
                @new="newQMDFile"
              />
            </section>
            <button
              v-else
              type="button"
              class="pane-collapsed-strip vertical"
              @click="toggleReportPane('editor')"
            >
              <AppIcon name="file" :size="14" />
              展开 QMD 编辑器
            </button>

            <div
              v-if="!reportEditorCollapsed && !reportPreviewCollapsed"
              class="split-divider vertical"
              role="separator"
              aria-label="调整 QMD 与 HTML 预览宽度"
              aria-orientation="vertical"
              tabindex="0"
              @pointerdown="beginSplitResize('report', $event)"
              @keydown.left.prevent="nudgeSplit('report', -3)"
              @keydown.right.prevent="nudgeSplit('report', 3)"
            >
              <span></span>
            </div>

            <section v-if="!reportPreviewCollapsed" class="report-preview-card">
              <div class="preview-toolbar">
                <div class="preview-tabs">
                  <button class="active">{{ reportFormat.toUpperCase() }}</button>
                  <span>{{ activeRunID || "尚未渲染" }}</span>
                </div>
                <div class="preview-actions">
                  <button type="button" class="text-button" @click="toggleReportPane('preview')">
                    收起
                  </button>
                  <button class="text-button" :disabled="!activeRunID" @click="openLatestOutput">
                    <AppIcon name="external" :size="14" /> 打开产物
                  </button>
                  <button class="text-button" :disabled="!activeRunID" @click="downloadOutput">
                    <AppIcon name="download" :size="14" /> 下载文件
                  </button>
                </div>
              </div>
              <div v-if="reportBusy" class="preview-loading">
                <div class="duck-loader small"><span></span><span></span><span></span></div>
                <p>{{ reportBusyLabel }}</p>
              </div>
              <div v-else-if="previewError" class="preview-error">
                <div class="empty-mark"><AppIcon name="report" :size="26" /></div>
                <h3>本次报告渲染失败</h3>
                <pre>{{ previewError }}</pre>
                <p>原预览已清除。请修复 QMD、数据或运行时依赖后重新渲染。</p>
              </div>
              <iframe
                v-else-if="previewUrl"
                :src="previewUrl"
                class="report-frame"
                title="Quarto 报告预览"
              ></iframe>
              <div v-else class="preview-placeholder">
                <div class="empty-mark"><AppIcon name="report" :size="28" /></div>
                <h3>渲染并预览报告</h3>
                <p>Quarto 会生成自包含 HTML 或 PDF，并保留运行目录中的源码与日志。</p>
              </div>
            </section>
            <button
              v-else
              type="button"
              class="pane-collapsed-strip vertical"
              @click="toggleReportPane('preview')"
            >
              <AppIcon name="report" :size="14" />
              展开报告预览
            </button>
          </div>
        </section>

        <section v-else class="view-panel settings-view">
          <div class="page-head settings-page-head">
            <div>
              <span class="eyebrow">APPLICATION SETTINGS</span>
              <h1>设置</h1>
              <p>配置应用名称、界面外观和报告运行时。工作区切换与数据库导出位于左侧数据资产栏。</p>
            </div>
            <button class="primary-button" :disabled="settingsBusy" @click="saveSettings">
              <AppIcon name="save" :size="15" />
              {{ settingsBusy ? "保存中…" : "保存设置" }}
            </button>
          </div>

          <div class="settings-layout">
            <div class="settings-column">
              <section class="settings-card">
                <div class="settings-card-head">
                  <div class="settings-card-icon"><AppIcon name="report" :size="19" /></div>
                  <div>
                    <span class="eyebrow">GENERAL</span>
                    <h2>应用名称</h2>
                  </div>
                </div>
                <p class="settings-description">
                  修改窗口标题、页面品牌和 About 信息。可执行文件及 Bundle ID 仍需在构建配置中修改。
                </p>
                <label class="form-field">
                  <span>显示名称</span>
                  <input v-model="settingsDraft.displayName" maxlength="80" placeholder="DuckDB Report Studio" />
                </label>
                <div class="settings-readonly-grid">
                  <div><span>当前版本</span><code>{{ state?.settings.version || "未知" }}</code></div>
                  <div><span>配置文件</span><code>{{ state?.settings.settingsPath }}</code></div>
                </div>
              </section>

              <section class="settings-card settings-about">
                <span class="eyebrow">ABOUT</span>
                <h2>{{ state?.settings.displayName }}</h2>
                <p>本地 DuckDB + SQL + Quarto 分析工作台。数据与报告默认不会上传到外部服务。</p>
                <code>duckdb_report {{ state?.settings.version }} · Go + Vue 3 + Wails</code>
              </section>

              <section class="settings-card settings-update-card">
                <div class="settings-card-head">
                  <div class="settings-card-icon"><AppIcon name="download" :size="19" /></div>
                  <div>
                    <span class="eyebrow">SOFTWARE UPDATE</span>
                    <h2>软件更新</h2>
                  </div>
                  <span
                    class="update-state-pill"
                    role="status"
                    aria-live="polite"
                    :class="{
                      available: updateInfo?.available,
                      current: updateInfo && !updateInfo.available,
                      error: updateError,
                    }"
                  >
                    {{
                      updateBusy
                        ? "检查中"
                        : updateError
                          ? "操作失败"
                          : updateInfo?.available
                            ? "有新版本"
                            : updateInfo
                              ? "已是最新"
                              : "尚未检查"
                    }}
                  </span>
                </div>
                <p class="settings-description">
                  应用会静默跟踪 GitHub Release，并在发现新版时点亮左下角设置按钮的角标。
                  更新包下载后会自动执行 SHA-256 校验。
                </p>

                <div class="update-version-row">
                  <div class="update-version-cell">
                    <span>当前版本</span>
                    <strong>{{ releaseVersion(state?.settings.version) }}</strong>
                  </div>
                  <AppIcon name="chevron" :size="16" />
                  <div class="update-version-cell latest">
                    <span>GitHub 最新正式版</span>
                    <strong>{{ releaseVersion(updateInfo?.latestVersion) }}</strong>
                  </div>
                  <div class="update-actions">
                    <button
                      class="ghost-button small"
                      :disabled="updateBusy"
                      @click="checkForUpdates()"
                    >
                      <AppIcon name="refresh" :size="14" />
                      {{ updateBusy ? "检查中…" : "检查更新" }}
                    </button>
                    <button
                      class="ghost-button small"
                      :disabled="!updateInfo?.releaseUrl"
                      @click="openUpdateRelease"
                    >
                      <AppIcon name="external" :size="14" />
                      Release
                    </button>
                  </div>
                </div>

                <div v-if="updateError" class="update-error" role="alert">
                  {{ updateError }}
                </div>

                <div v-if="updateInfo?.available" class="update-release">
                  <div class="update-release-head">
                    <div>
                      <span class="eyebrow">LATEST RELEASE</span>
                      <h3>{{ updateInfo.releaseName || releaseVersion(updateInfo.latestVersion) }}</h3>
                    </div>
                    <span v-if="updateInfo.publishedAt">
                      发布于 {{ formatReleaseDate(updateInfo.publishedAt) }}
                    </span>
                  </div>
                  <pre v-if="updateInfo.releaseNotes" class="update-release-notes">{{ updateInfo.releaseNotes }}</pre>
                  <p v-else class="update-release-empty">该 Release 未提供更新说明。</p>

                  <div class="update-download-row">
                    <div>
                      <strong>{{ updateInfo.assetName || "当前平台暂无匹配安装包" }}</strong>
                      <small v-if="updateInfo.assetName">
                        {{ formatBytes(updateInfo.assetSize) }} · 下载至 Downloads/duckdb-report-updates
                      </small>
                    </div>
                    <div class="update-actions">
                      <button
                        class="primary-button"
                        :disabled="!updateInfo.assetName || updateDownloadBusy"
                        @click="downloadUpdate"
                      >
                        <AppIcon name="download" :size="15" />
                        {{ updateDownloadBusy ? "下载并校验中…" : "下载更新包" }}
                      </button>
                      <button
                        v-if="updateDownloaded"
                        class="secondary-button"
                        @click="openDownloadedUpdate"
                      >
                        <AppIcon name="external" :size="15" />
                        打开更新包
                      </button>
                    </div>
                  </div>
                  <p v-if="updateDownloaded" class="update-downloaded">
                    <strong>已校验：</strong>
                    <code>{{ updateDownloaded.path }}</code>
                  </p>
                </div>
              </section>
            </div>

            <div class="settings-column">
              <section class="settings-card">
                <div class="settings-card-head">
                  <div class="settings-card-icon"><AppIcon name="sun" :size="19" /></div>
                  <div>
                    <span class="eyebrow">APPEARANCE</span>
                    <h2>界面主题</h2>
                  </div>
                </div>
                <div class="theme-options">
                  <button :class="{ active: theme === 'dark' }" @click="setTheme('dark')">
                    <span class="theme-preview dark"></span>
                    <strong>深色</strong>
                    <small>适合长时间分析</small>
                  </button>
                  <button :class="{ active: theme === 'light' }" @click="setTheme('light')">
                    <span class="theme-preview light"></span>
                    <strong>明亮</strong>
                    <small>适合打印和展示</small>
                  </button>
                </div>
                <div class="ui-font-size-setting">
                  <div class="ui-font-size-head">
                    <div>
                      <strong>界面字体大小</strong>
                      <small>影响菜单、表格、按钮和设置页面文字</small>
                    </div>
                    <span>{{ settingsDraft.uiFontSize }} px</span>
                  </div>
                  <input
                    v-model.number="settingsDraft.uiFontSize"
                    type="range"
                    min="11"
                    max="17"
                    step="1"
                    aria-label="界面字体大小"
                  />
                  <div class="ui-font-size-presets">
                    <button
                      v-for="option in uiFontSizeOptions"
                      :key="option.value"
                      type="button"
                      :class="{ active: settingsDraft.uiFontSize === option.value }"
                      @click="settingsDraft.uiFontSize = option.value"
                    >
                      <strong>{{ option.label }}</strong>
                      <small>{{ option.value }} px</small>
                    </button>
                  </div>
                  <p>编辑器文字大小可在 SQL/QMD 编辑栏使用快捷键单独调整。</p>
                </div>
              </section>

              <section class="settings-card">
                <div class="settings-card-head">
                  <div class="settings-card-icon"><AppIcon name="terminal" :size="19" /></div>
                  <div>
                    <span class="eyebrow">RUNTIME</span>
                    <h2>报告运行时</h2>
                  </div>
                </div>
                <div class="runtime-checks">
                  <div class="runtime-check">
                    <span class="env-dot ready"></span>
                    <div><strong>DuckDB</strong><small>{{ state?.environment.duckdbVersion }}</small></div>
                    <span class="runtime-check-status">可用</span>
                  </div>
                  <div class="runtime-check">
                    <span class="env-dot" :class="{ ready: state?.environment.quartoPath }"></span>
                    <div><strong>Quarto</strong><small>{{ state?.environment.quartoVersion || "未找到" }}</small></div>
                    <span class="runtime-check-status">{{ state?.environment.quartoPath ? "可用" : "缺失" }}</span>
                  </div>
                  <div class="runtime-check">
                    <span class="env-dot" :class="{ ready: state?.environment.rPath }"></span>
                    <div><strong>R / Rscript</strong><small>{{ state?.environment.rVersion || "未安装" }}</small></div>
                    <span class="runtime-check-status">{{ state?.environment.rPath ? "可用" : "可选" }}</span>
                  </div>
                  <div class="runtime-check">
                    <span class="env-dot" :class="{ ready: state?.environment.pythonPath }"></span>
                    <div><strong>Python / Jupyter</strong><small>{{ state?.environment.pythonVersion || "未安装" }}</small></div>
                    <span class="runtime-check-status">{{ state?.environment.jupyterAvailable ? "可用" : "可选" }}</span>
                  </div>
                  <div class="runtime-check">
                    <span class="env-dot" :class="{ ready: state?.environment.pdfAvailable }"></span>
                    <div><strong>PDF / Typst</strong><small>Quarto 报告导出能力</small></div>
                    <span class="runtime-check-status">{{ state?.environment.pdfAvailable ? "可用" : "缺失" }}</span>
                  </div>
                </div>
              </section>
            </div>

          </div>
        </section>

        <section v-if="logsOpen" class="log-drawer">
          <div class="log-head">
            <div>
              <span class="terminal-glyph">&gt;_</span>
              <strong>运行日志</strong>
              <span>{{ logs.length }} events</span>
            </div>
            <div>
              <button class="text-button" @click="logs = []">清空</button>
              <button class="text-button" @click="logsOpen = false">收起</button>
            </div>
          </div>
          <div class="log-lines">
            <div v-for="(entry, index) in logs" :key="index" class="log-line" :class="entry.level">
              <span>{{ entry.time }}</span>
              <p>{{ entry.text }}</p>
            </div>
            <div v-if="logs.length === 0" class="log-placeholder">运行 SQL 或生成报告后，日志会出现在这里。</div>
          </div>
        </section>
      </main>

      <aside v-if="inspectorOpen && activeView !== 'settings'" class="inspector">
        <div class="inspector-head">
          <div>
            <span class="eyebrow">INSPECTOR</span>
            <h2>工作区状态</h2>
          </div>
          <button class="icon-button compact" @click="inspectorOpen = false">
            <AppIcon name="chevron" :size="15" />
          </button>
        </div>

        <section class="inspector-section">
          <span class="inspector-label">环境</span>
          <div class="env-item">
            <span class="env-dot ready"></span>
            <div>
              <strong>DuckDB</strong>
              <small>{{ state?.environment.duckdbVersion }}</small>
            </div>
          </div>
          <div class="env-item">
            <span class="env-dot" :class="{ ready: state?.environment.quartoPath }"></span>
            <div>
              <strong>Quarto</strong>
              <small>{{ state?.environment.quartoVersion || "未安装" }}</small>
            </div>
          </div>
          <div class="env-item">
            <span class="env-dot" :class="{ ready: state?.environment.pdfAvailable }"></span>
            <div>
              <strong>PDF / Typst</strong>
              <small>{{ state?.environment.pdfAvailable ? "可用" : "不可用" }}</small>
            </div>
          </div>
          <div class="env-item">
            <span class="env-dot" :class="{ ready: state?.environment.rPath }"></span>
            <div>
              <strong>R / Rscript</strong>
              <small>{{ state?.environment.rVersion || "未安装（不影响 Go 数据注入）" }}</small>
            </div>
          </div>
          <div class="env-item">
            <span class="env-dot" :class="{ ready: state?.environment.pythonPath }"></span>
            <div>
              <strong>Python</strong>
              <small>{{ state?.environment.pythonVersion || "未安装" }}{{ state?.environment.jupyterAvailable ? " · Jupyter 可用" : "" }}</small>
            </div>
          </div>
        </section>

        <section v-if="activeView === 'workflow'" class="inspector-section">
          <div class="inspector-section-title">
            <span class="inspector-label">步骤参数</span>
            <span class="count-badge">{{ workflow?.steps.length }}</span>
          </div>
          <div
            v-for="step in workflow?.steps.filter((item) => item.enabled)"
            :key="step.id"
            class="param-preview"
          >
            <div class="param-preview-head">
              <span :class="`status-dot ${currentStepState(step).status}`"></span>
              <strong>{{ step.name }}</strong>
            </div>
            <div
              v-for="(value, key) in presetFor(step)?.params"
              :key="key"
              class="param-row"
            >
              <span>{{ key }}</span>
              <code>{{ value }}</code>
            </div>
          </div>
        </section>

        <section class="inspector-section">
          <div class="inspector-section-title">
            <span class="inspector-label">最近产物</span>
            <button class="text-button" @click="refreshState()">刷新</button>
          </div>
          <button
            v-for="run in runs.slice(0, 5)"
            :key="run.id"
            class="run-item"
            @click="openRun(run)"
          >
            <AppIcon name="report" :size="15" />
            <span>
              <strong>{{ run.name }}</strong>
              <small>{{ formatTime(run.modifiedAt) }} · {{ run.pdfPath ? "HTML + PDF" : "HTML" }}</small>
            </span>
          </button>
          <div v-if="runs.length === 0" class="sidebar-empty">尚无报告产物</div>
        </section>

        <section class="inspector-section shortcut-card">
          <span class="inspector-label">快捷操作</span>
          <button class="shortcut-row" @click="chooseDataFile">
            <AppIcon name="database" :size="15" /><span>导入数据 / 连接数据库</span><kbd>⌘ O</kbd>
          </button>
          <button class="shortcut-row" @click="activeView = 'sql'">
            <AppIcon name="terminal" :size="15" /><span>打开 SQL 编辑器</span><kbd>⌘ 2</kbd>
          </button>
          <button class="shortcut-row" @click="activeView = 'report'">
            <AppIcon name="report" :size="15" /><span>生成报告</span>
          </button>
        </section>
      </aside>
    </div>

    <div v-if="toast" class="toast" :class="toast.kind">{{ toast.text }}</div>

    <div v-if="clearDataOpen" class="modal-backdrop" @click.self="clearDataOpen = false">
      <div class="modal-card destructive-modal">
        <div class="modal-head">
          <div class="modal-icon danger"><AppIcon name="trash" :size="22" /></div>
          <div>
            <span class="eyebrow">CLEAR WORKSPACE</span>
            <h2>清空工作库？</h2>
          </div>
        </div>
        <p class="settings-description">
          此操作会清空当前工作库中的用户表、视图和外部 DuckDB 连接。SQL、QMD 和已生成的报告文件都会保留。
        </p>
        <ul class="destructive-list">
          <li>SQL、QMD 文件和已生成的报告不会删除。</li>
          <li>重新运行 SQL 或导入数据后，可以再次建立所需结果表。</li>
          <li>外部 CSV/Excel/DuckDB 原文件不会删除。</li>
          <li>工作库中的分析中间表和结果表会被删除。</li>
        </ul>
        <div class="modal-actions">
          <button class="ghost-button" @click="clearDataOpen = false">取消</button>
          <button class="danger-button" :disabled="sourceBusy" @click="clearWorkspaceData">
            {{ sourceBusy ? "正在清理…" : "确认清空工作库" }}
          </button>
        </div>
      </div>
    </div>

    <div v-if="presetEditorOpen && presetDraft" class="modal-backdrop" @click.self="presetEditorOpen = false">
      <form class="modal-card preset-editor-card" @submit.prevent="savePreset(false)">
        <div class="modal-head">
          <div class="modal-icon"><AppIcon :name="presetIcon(presetDraft.type)" :size="22" /></div>
          <div>
            <span class="eyebrow">{{ presetIsNew ? "NEW PRESET" : "EDIT PRESET" }}</span>
            <h2>{{ presetIsNew ? "新建组合参数" : "查看与编辑 Preset" }}</h2>
          </div>
        </div>

        <div class="preset-editor-grid">
          <label class="form-field">
            <span>类型</span>
            <select v-model="presetDraft.type" @change="changePresetType">
              <option v-for="option in presetTypeOptions" :key="option.value" :value="option.value">
                {{ option.label }}
              </option>
            </select>
          </label>
          <label class="form-field">
            <span>Preset ID</span>
            <input v-model="presetDraft.id" required />
          </label>
        </div>
        <label class="form-field">
          <span>名称</span>
          <input v-model="presetDraft.name" required />
        </label>
        <label class="form-field">
          <span>说明</span>
          <input v-model="presetDraft.description" placeholder="说明这套参数解决什么问题" />
        </label>

        <div v-if="presetDraft.type === 'sqlset'" class="preset-sql-source">
          <div class="preset-sql-mode">
            <span class="inspector-label">SQL 来源</span>
            <div class="segmented-control">
              <button
                type="button"
                :class="{ active: (presetDraft.params.mode || 'file') === 'file' }"
                @click="setPresetSQLMode('file')"
              >
                文件模式
              </button>
              <button
                type="button"
                :class="{ active: presetDraft.params.mode === 'inline' }"
                @click="setPresetSQLMode('inline')"
              >
                Inline 模式
              </button>
            </div>
          </div>

          <div v-if="(presetDraft.params.mode || 'file') === 'file'" class="sql-file-picker">
            <div>
              <span>SQL 文件</span>
              <code>{{ presetDraft.params.file || "尚未选择 .sql 文件" }}</code>
            </div>
            <button type="button" class="ghost-button" @click="pickPresetSQLFile">
              <AppIcon name="file" :size="14" /> 选择 SQL 文件
            </button>
          </div>
          <label v-else class="form-field">
            <span>完整 SQL 查询</span>
            <textarea
              v-model="presetDraft.params.inline_sql"
              class="inline-sql-editor"
              spellcheck="false"
              placeholder="CREATE OR REPLACE TABLE ...;&#10;&#10;SELECT ..."
            ></textarea>
          </label>
          <p class="preset-sql-note">
            文件模式适合版本管理；Inline 模式会把完整 SQL 保存在 Preset 中。两种模式都支持
            <code v-pre>{{变量}}</code>、多条语句和 <code>.read</code>。
          </p>
        </div>

        <div v-if="presetDraft.type === 'output'" class="preset-output-source">
          <div class="preset-output-grid">
            <label class="form-field">
              <span>发布包文件名</span>
              <input
                v-model="presetDraft.params.filename"
                placeholder="report-package"
              />
            </label>
            <label class="form-field">
              <span>工作区内输出目录</span>
              <input
                v-model="presetDraft.params.directory"
                placeholder="exports"
              />
            </label>
          </div>
          <div class="preset-output-options">
            <label>
              <span>报告成果</span>
              <select v-model="presetDraft.params.include_report">
                <option value="true">包含 HTML / PDF</option>
                <option value="false">不包含</option>
              </select>
            </label>
            <label>
              <span>数据库快照</span>
              <select v-model="presetDraft.params.include_database">
                <option value="true">包含 analysis.duckdb</option>
                <option value="false">不包含</option>
              </select>
            </label>
            <label>
              <span>报告源文件</span>
              <select v-model="presetDraft.params.include_source">
                <option value="true">包含 QMD 与 assets</option>
                <option value="false">不包含</option>
              </select>
            </label>
          </div>
          <p class="preset-sql-note">
            导出节点必须放在报告渲染节点之后。数据库来自该次报告的
            <code>analysis.duckdb</code> 快照，不打包正在使用的工作库文件。
          </p>
        </div>

        <div class="preset-param-editor">
          <div class="inspector-section-title">
            <span class="inspector-label">参数</span>
            <button type="button" class="text-button" @click="addPresetParam">
              <AppIcon name="plus" :size="13" /> 添加参数
            </button>
          </div>
          <div class="preset-param-head">
            <span>KEY</span><span>VALUE</span><span></span>
          </div>
          <div v-for="(row, index) in presetParamRows" :key="index" class="preset-param-row">
            <input v-model="row.key" placeholder="例如 file" />
            <input v-model="row.value" placeholder="参数值或 {{variable}}" />
            <button type="button" class="mini-icon danger" title="删除参数" @click="removePresetParam(index)">
              <AppIcon name="trash" :size="13" />
            </button>
          </div>
          <div v-if="presetParamRows.length === 0" class="sidebar-empty">还没有参数</div>
        </div>

        <p class="modal-note">
          数据源 Preset 的参数通常为 <code>kind/path/table/sheet</code>；SQL Preset 常用
          <code>file/query_file/limit</code>；报告 Preset 常用
          <code>template/format/query</code>，其中 <code>query</code> 输出到
          <code v-pre>{{table}}</code>，<code>query_xxx</code> 输出到
          <code v-pre>{{table_xxx}}</code>；输出 Preset 可配置 ZIP 文件名、目录和打包内容。
        </p>
        <div class="modal-actions spread">
          <button
            v-if="!presetIsNew"
            type="button"
            class="text-button danger-text"
            @click="deletePreset"
          >
            删除 Preset
          </button>
          <span v-else></span>
          <div class="modal-action-group">
            <button type="button" class="ghost-button" @click="presetEditorOpen = false">取消</button>
            <button type="button" class="ghost-button" @click="savePreset(false)">仅保存</button>
            <button type="button" class="primary-button" @click="savePreset(true)">保存并加入步骤</button>
          </div>
        </div>
      </form>
    </div>

    <div v-if="importOpen" class="modal-backdrop" @click.self="importOpen = false">
      <form class="modal-card" @submit.prevent="confirmImport">
        <div class="modal-head">
          <div class="modal-icon"><AppIcon name="database" :size="22" /></div>
          <div>
            <span class="eyebrow">IMPORT DATA</span>
            <h2>导入数据或连接数据库</h2>
          </div>
        </div>
        <div class="modal-file">
          <AppIcon name="file" :size="18" />
          <div>
            <strong>{{ baseName(importPath) }}</strong>
            <small>{{ importPath }}</small>
          </div>
          <span class="kind-chip">{{ importKind }}</span>
        </div>
        <div class="format-chips">
          <span>CSV</span><span>Excel</span><span>JSON</span><span>Parquet</span><span>DuckDB</span>
        </div>
        <label class="form-field">
          <span>{{ importKind === "duckdb" || importKind === "db" ? "连接别名" : "目标表名" }}</span>
          <input v-model="importTable" required autofocus />
        </label>
        <label v-if="importSheets.length" class="form-field">
          <span>Excel Sheet</span>
          <select v-model="importSheet">
            <option v-for="sheet in importSheets" :key="sheet" :value="sheet">{{ sheet }}</option>
          </select>
        </label>
        <p class="modal-note">
          CSV、JSON、Parquet 和 Excel 会复制/转换为 DuckDB 工作库中的表；DuckDB 文件以外部
          catalog 附加，并保留原文件路径。这里的“分析资产”仅指可查询的数据连接。
        </p>
        <div class="modal-actions">
          <button type="button" class="ghost-button" @click="importOpen = false">取消</button>
          <button type="submit" class="primary-button" :disabled="importBusy">
            {{ importBusy ? "正在导入…" : "导入数据" }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>
