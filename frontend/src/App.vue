<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import AppIcon from "./components/AppIcon.vue";
import ResultGrid from "./components/ResultGrid.vue";
import {
  backend,
  baseName,
  errorMessage,
  fileKind,
  fileStem,
  formatTime,
  resolveTemplate,
} from "./api";
import type {
  AppSettings,
  Bootstrap,
  DataCatalog,
  DataSource,
  Preset,
  PresetType,
  QueryResult,
  RenderResult,
  StepEvent,
  TableRef,
  ViewName,
  Workflow,
  WorkflowStep,
} from "./types";

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
const activeView = ref<ViewName>("workflow");
const inspectorOpen = ref(true);
const logsOpen = ref(true);
const settingsBusy = ref(false);
const exportWorkspaceBusy = ref(false);
const settingsDraft = ref<AppSettings>({ displayName: "", workspace: "" });
const logs = ref<LogEntry[]>([]);
const workflowBusy = ref(false);
const workflowTransferBusy = ref(false);
const sqlBusy = ref(false);
const reportBusy = ref(false);
const theme = ref<"dark" | "light">("dark");
const toast = ref<{ text: string; kind: "info" | "error" | "success" } | null>(null);
let toastTimer: number | undefined;

const sqlFiles = computed(() => state.value?.sqlFiles ?? []);
const templates = computed(() => state.value?.templates ?? []);
const tables = computed(() => state.value?.tables ?? []);
const sources = computed(() => state.value?.sources ?? []);
const runs = computed(() => state.value?.runs ?? []);
const presets = computed(() => workflow.value?.presets ?? []);
const runtimeParams = computed(() => workflow.value?.runtime ?? {});

const selectedSQL = ref("sql/monthly_analysis.sql");
const sqlContent = ref("");
const sqlResult = ref<QueryResult | null>(null);
const sqlFilter = ref("");
const selectedTemplate = ref("templates/monthly_report.qmd");
const reportContent = ref("");
const reportFormat = ref<"html" | "pdf">("html");
const reportBusyLabel = ref("正在渲染报告…");
const previewUrl = ref("");
const previewError = ref("");
const activeRunID = ref("");
const renderResult = ref<RenderResult | null>(null);
const sqlDisplayName = computed(
  () => selectedSQL.value || "临时查询（未保存）",
);

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

const visibleTables = computed(() => {
  const query = sqlFilter.value.trim().toLowerCase();
  if (!query) return tables.value;
  return tables.value.filter((table) =>
    `${table.catalog}.${table.schema}.${table.name}`.toLowerCase().includes(query),
  );
});

const workflowCompleted = computed(() => {
  if (!workflow.value) return 0;
  return workflow.value.steps.filter((step) => stepStates[step.id]?.status === "success").length;
});

const latestRun = computed(() => runs.value[0] ?? null);
const brandPrimary = computed(
  () => state.value?.settings.displayName || "DuckDB Report Studio",
);
const workspaceRestartRequired = computed(() =>
  Boolean(
    state.value?.settings.currentWorkspace &&
      settingsDraft.value.workspace &&
      settingsDraft.value.workspace !== state.value.settings.currentWorkspace,
  ),
);
const qmdUsesR = computed(() => /```\s*\{\s*r\b/i.test(reportContent.value));
const qmdUsesPython = computed(() => /```\s*\{\s*python\b/i.test(reportContent.value));
const qmdUsesDataBridge = computed(() => reportContent.value.includes("{{table}}"));
const qmdNeedsJupyter = computed(() => qmdUsesPython.value);
const qmdDependencyError = computed(() => {
  if (qmdUsesR.value && !state.value?.environment.rPath) {
    return "当前 QMD 含 R 代码块，但未检测到 Rscript。请安装 R，或改用 {{table}} 数据注入。";
  }
  if (qmdNeedsJupyter.value && !state.value?.environment.jupyterAvailable) {
    return "当前 QMD 含 Python 代码块，但未检测到 Jupyter。请完成 Quarto/Jupyter 配置，或改用 {{table}}。";
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
        template: "templates/monthly_report.qmd",
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

async function loadInitial() {
  loading.value = true;
  fatalError.value = "";
  try {
    const result = await backend().Bootstrap();
    state.value = result;
    settingsDraft.value = {
      displayName: result.settings.displayName,
      workspace: result.settings.workspace,
    };
    workflow.value = clone(result.workflow);
    loadStepStates();
    if (ensureWorkflowArchitecture()) {
      await saveWorkflow(true);
    }
    const firstSQL =
      result.sqlFiles.find((file) => file.path === "sql/monthly_analysis.sql") ??
      result.sqlFiles[0];
    if (firstSQL) {
      selectedSQL.value = firstSQL.path;
      await loadSQL(firstSQL.path);
    }
    const firstTemplate =
      result.templates.find((file) => file.path === "templates/monthly_report.qmd") ??
      result.templates[0];
    if (firstTemplate) {
      selectedTemplate.value = firstTemplate.path;
      await loadTemplate(firstTemplate.path);
    }
    if (result.runs[0]?.htmlPath) {
      previewUrl.value = `/preview/${result.runs[0].id}/report.html`;
      previewError.value = "";
      activeRunID.value = result.runs[0].id;
    }
    pushLog("success", "工作区已连接，DuckDB 会话就绪。");
  } catch (error) {
    fatalError.value = errorMessage(error);
    pushLog("error", fatalError.value);
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
      };
    }
    if (current) workflow.value = current;
  } catch (error) {
    notify(errorMessage(error), "error");
  }
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
    const preset: Preset = {
      id: `source-${Date.now().toString(36)}`,
      type: "datasource",
      name: baseName(importPath.value),
      description: `已接入 ${imported.source.kind.toUpperCase()} · ${imported.source.table}`,
      params: {
        kind: imported.source.kind,
        path: imported.source.path,
        table: imported.source.table,
        sheet: imported.source.sheet ?? "",
      },
    };
    workflow.value?.presets.push(preset);
    addPresetStep(preset);
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
  state.value.tables = catalog.tables;
  state.value.sources = catalog.sources;
}

async function removeSource(source: DataSource) {
  if (sourceBusy.value) return;
  sourceBusy.value = true;
  try {
    const catalog = await backend().DeleteSource(source);
    applyDataCatalog(catalog);
    notify(`已移除数据源：${source.table}；工作流结构已保留`, "success");
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
    notify("工作库中的数据已清空，工作流结构已保留", "success");
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
  await runSQL(false);
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

async function saveSQL() {
  if (!selectedSQL.value) {
    await saveSQLAs();
    return;
  }
  try {
    const saved = await backend().SaveText(selectedSQL.value, sqlContent.value);
    selectedSQL.value = saved.path;
    notify("SQL 已保存", "success");
    await refreshState();
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
  await saveSQLAs();
}

async function saveSQLAs() {
  try {
    const saved = await backend().SaveSQLAs(sqlContent.value);
    if (!saved.path) return;
    selectedSQL.value = saved.path;
    sqlContent.value = saved.content;
    await refreshState(false);
    notify(`SQL 已另存为：${saved.path}`, "success");
  } catch (error) {
    notify(errorMessage(error), "error");
  }
}

async function runSQL(showNotification = true) {
  if (sqlBusy.value || !sqlContent.value.trim()) return;
  sqlBusy.value = true;
  const started = performance.now();
  try {
    const query = resolveTemplate(sqlContent.value, runtimeParams.value);
    sqlResult.value = await backend().RunSQL(query, 500, selectedSQL.value);
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
    // DDL and scripts can change the catalog even when a later statement fails.
    await refreshState();
  }
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
  return "执行前会将 {{variables}} 替换为工作流运行参数。";
}

async function loadTemplate(path: string) {
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

function reportQuery(): string {
  const reportPreset = presets.value.find((preset) => preset.type === "report");
  return reportPreset?.params.query ?? "SELECT * FROM monthly_summary";
}

async function renderReport(format: "html" | "pdf") {
  if (!workflow.value || !reportContent.value.trim() || reportBusy.value) return;
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
      content: reportContent.value,
      format,
      query: reportQuery(),
      params: workflow.value.runtime,
      saveAs: workflow.value.name,
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

async function saveTemplate() {
  try {
    const saved = await backend().SaveText(selectedTemplate.value, reportContent.value);
    selectedTemplate.value = saved.path;
    notify("QMD 已保存", "success");
    await refreshState();
  } catch (error) {
    notify(errorMessage(error), "error");
  }
}

async function chooseWorkspaceDirectory() {
  if (state.value?.settings.workspaceOverride) {
    notify("当前工作区由 DUCKDB_REPORT_HOME 环境变量控制", "error");
    return;
  }
  try {
    const path = await backend().PickWorkspaceDirectory();
    if (path) settingsDraft.value.workspace = path;
  } catch (error) {
    notify(errorMessage(error), "error");
  }
}

function resetWorkspacePath() {
  if (state.value?.settings.defaultWorkspace) {
    settingsDraft.value.workspace = state.value.settings.defaultWorkspace;
  }
}

async function saveSettings() {
  if (settingsBusy.value) return;
  settingsBusy.value = true;
  try {
    const saved = await backend().SaveSettings(settingsDraft.value);
    if (state.value) state.value.settings = saved;
    settingsDraft.value = {
      displayName: saved.displayName,
      workspace: saved.workspace,
    };
    notify(
      workspaceRestartRequired.value
        ? "设置已保存；工作区将在下次启动时切换"
        : "设置已保存",
      "success",
    );
  } catch (error) {
    notify(errorMessage(error), "error");
  } finally {
    settingsBusy.value = false;
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

function lineNumbers(content: string): string {
  return content.split("\n").map((_, index) => index + 1).join("\n");
}

watch(
  () => workflow.value?.name,
  (name, previous) => {
    if (!workflow.value || !name) return;
    if (!workflow.value.runtime.title || workflow.value.runtime.title === previous) {
      workflow.value.runtime.title = name;
    }
  },
);

onMounted(async () => {
  const savedTheme = localStorage.getItem("duckdb-report-theme");
  const preferredDark = window.matchMedia("(prefers-color-scheme: dark)").matches;
  setTheme(savedTheme === "light" || savedTheme === "dark" ? savedTheme : preferredDark ? "dark" : "light");
  window.runtime?.EventsOn("workflow:event", onWorkflowEvent);
  await loadInitial();
});
</script>

<template>
  <div class="app-shell" :data-theme="theme">
    <header class="titlebar">
      <div class="brand">
        <div class="brand-mark">
          <span></span><span></span><span></span>
        </div>
        <div class="brand-copy">
          <strong>{{ brandPrimary }}</strong>
        </div>
      </div>
      <div class="titlebar-breadcrumb">
        <span>{{ workflow?.name ?? "Workspace" }}</span>
        <AppIcon name="chevron" :size="13" />
        <strong>{{ activeView === "workflow" ? "工作流" : activeView === "sql" ? "SQL 分析" : activeView === "settings" ? "设置" : "报告预览" }}</strong>
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

    <div v-else class="app-body" :class="{ 'logs-visible': logsOpen, 'settings-mode': activeView === 'settings' }">
      <nav class="activity-rail">
        <button
          v-for="item in [
            { id: 'workflow', icon: 'workflow', label: '工作流' },
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
          :class="{ active: activeView === 'settings' }"
          title="设置"
          @click="activeView = 'settings'"
        >
          <AppIcon name="settings" :size="19" />
          <span>设置</span>
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
          <p class="side-help">CSV、Excel、JSON、Parquet 导入为本地表；DuckDB 文件作为外部 catalog 连接。</p>
          <label class="search-field">
            <AppIcon name="search" :size="14" />
            <input v-model="sqlFilter" type="search" placeholder="筛选表名" />
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
          <div class="table-tree">
            <button
              v-for="table in visibleTables"
              :key="`${table.catalog}.${table.schema}.${table.name}`"
              class="tree-row table-row"
              @click="previewTable(table)"
            >
              <AppIcon name="table" :size="15" />
              <span>{{ table.name }}</span>
              <small>{{ table.schema }}</small>
            </button>
            <div v-if="visibleTables.length === 0" class="sidebar-empty">
              尚未接入数据表
            </div>
          </div>
        </section>

        <section class="side-section presets-section">
          <div class="section-title">
            <span>可组合 Preset</span>
            <div class="section-actions">
              <span class="count-badge">{{ presets.length }}</span>
              <button class="text-button" @click="openNewPreset()">
                <AppIcon name="plus" :size="13" /> 新建
              </button>
            </div>
          </div>
          <div class="preset-stack">
            <div
              v-for="preset in presets"
              :key="preset.id"
              class="preset-item"
            >
              <button class="preset-main" :title="`查看或编辑「${preset.name}」`" @click="openPreset(preset)">
                <span class="preset-icon">
                  <AppIcon :name="presetIcon(preset.type)" :size="16" />
                </span>
                <span class="preset-copy">
                  <strong>{{ preset.name }}</strong>
                  <small>{{ presetTypeLabel(preset.type) }} · {{ preset.params.table || preset.params.file || preset.params.template || "参数集" }}</small>
                </span>
              </button>
              <button class="preset-add" :title="`添加「${preset.name}」步骤`" @click="addPresetStep(preset)">
                <AppIcon name="plus" :size="14" />
              </button>
            </div>
          </div>
        </section>

        <div class="sidebar-footer">
          <span>WORKSPACE</span>
          <code>{{ state?.database }}</code>
        </div>
      </aside>

      <main class="main-workspace">
        <section v-if="activeView === 'workflow'" class="view-panel">
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
              <button class="ghost-button" @click="saveSQL">
                <AppIcon name="save" :size="15" /> 保存
              </button>
              <button class="ghost-button" @click="saveSQLAs">
                <AppIcon name="file" :size="15" /> 另存为
              </button>
              <button class="primary-button" :disabled="sqlBusy" @click="runSQL()">
                <AppIcon name="play" :size="15" />
                {{ sqlBusy ? "执行中…" : "运行" }}
              </button>
            </div>
          </div>

          <div class="editor-card sql-editor-card">
            <div class="editor-topline">
              <span :class="{ 'scratch-document': !selectedSQL }">{{ sqlDisplayName }}</span>
              <span>{{ explainSelectedSQL() }}</span>
            </div>
            <div class="code-editor sql-code-editor">
              <pre class="line-numbers" aria-hidden="true">{{ lineNumbers(sqlContent) }}</pre>
              <textarea v-model="sqlContent" spellcheck="false" aria-label="SQL 编辑器"></textarea>
            </div>
          </div>

          <div class="result-card">
            <div class="result-header">
              <div>
                <span class="eyebrow">RESULT SET</span>
                <h3>查询结果</h3>
              </div>
              <button v-if="sqlResult" class="text-button" @click="sqlResult = null">清空</button>
            </div>
            <ResultGrid :result="sqlResult" />
          </div>
        </section>

        <section v-else-if="activeView === 'report'" class="view-panel report-view">
          <div class="page-head compact-head">
            <div>
              <span class="eyebrow">QUARTO COMPOSER</span>
              <h1>报告预览</h1>
            </div>
            <div class="page-actions">
              <select v-model="selectedTemplate" class="select-control wide" @change="loadTemplate(selectedTemplate)">
                <option v-for="file in templates" :key="file.path" :value="file.path">{{ file.path }}</option>
              </select>
              <button class="ghost-button" @click="saveTemplate">
                <AppIcon name="save" :size="15" /> 保存
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
              <p v-else>当前模板没有 R/Python 代码块；正文可直接渲染。</p>
            </div>
            <span v-if="qmdUsesDataBridge" class="runtime-chip success">Go 注入 <code v-pre>{{table}}</code></span>
            <span v-if="qmdUsesR" class="runtime-chip" :class="{ success: state?.environment.rPath }">R</span>
            <span v-if="qmdUsesPython" class="runtime-chip" :class="{ success: state?.environment.jupyterAvailable }">Python / Jupyter</span>
          </div>

          <div class="report-layout">
            <section class="editor-card report-editor">
              <div class="editor-topline">
                <span>QMD SOURCE</span>
                <span>{{ reportContent.split("\n").length }} lines</span>
              </div>
              <div class="code-editor report-code-editor">
                <pre class="line-numbers" aria-hidden="true">{{ lineNumbers(reportContent) }}</pre>
                <textarea v-model="reportContent" spellcheck="false" aria-label="QMD 编辑器"></textarea>
              </div>
            </section>
            <section class="report-preview-card">
              <div class="preview-toolbar">
                <div class="preview-tabs">
                  <button class="active">{{ reportFormat.toUpperCase() }}</button>
                  <span>{{ activeRunID || "尚未渲染" }}</span>
                </div>
                <div class="preview-actions">
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
          </div>
        </section>

        <section v-else class="view-panel settings-view">
          <div class="page-head settings-page-head">
            <div>
              <span class="eyebrow">APPLICATION SETTINGS</span>
              <h1>设置</h1>
              <p>配置应用显示名称、默认工作区和界面外观。工作区切换会在下次启动时生效。</p>
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
                  <div><span>当前版本</span><code>0.1.2</code></div>
                  <div><span>配置文件</span><code>{{ state?.settings.settingsPath }}</code></div>
                </div>
              </section>

              <section class="settings-card">
                <div class="settings-card-head">
                  <div class="settings-card-icon"><AppIcon name="database" :size="19" /></div>
                  <div>
                    <span class="eyebrow">WORKSPACE</span>
                    <h2>工作区</h2>
                  </div>
                </div>
                <p class="settings-description">
                  DuckDB 工作库、SQL、QMD、工作流和报告产物都保存在工作区目录中。
                </p>
                <label class="form-field">
                  <span>下次启动使用的工作区</span>
                  <input
                    v-model="settingsDraft.workspace"
                    :disabled="state?.settings.workspaceOverride"
                    spellcheck="false"
                  />
                </label>
                <div class="setting-actions">
                  <button
                    class="ghost-button"
                    :disabled="state?.settings.workspaceOverride"
                    @click="chooseWorkspaceDirectory"
                  >
                    <AppIcon name="file" :size="14" /> 选择目录
                  </button>
                  <button class="ghost-button" @click="resetWorkspacePath">恢复默认路径</button>
                  <button class="text-button" @click="openWorkspaceFolder">打开当前工作区</button>
                  <button class="text-button" :disabled="exportWorkspaceBusy" @click="exportWorkspaceDuckDB">
                    <AppIcon name="download" :size="14" />
                    {{ exportWorkspaceBusy ? "正在导出…" : "导出工作库 .duckdb" }}
                  </button>
                </div>
                <div class="workspace-current">
                  <span>当前运行中的工作区</span>
                  <code>{{ state?.settings.currentWorkspace }}</code>
                </div>
                <div v-if="state?.settings.workspaceOverride" class="settings-warning">
                  当前工作区由 <code>DUCKDB_REPORT_HOME</code> 控件；取消该环境变量后才能使用保存的工作区。
                </div>
                <div v-else-if="workspaceRestartRequired" class="settings-warning restart">
                  工作区已改变，关闭并重新启动应用后生效。现有数据不会自动迁移。
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

              <section class="settings-card settings-about">
                <span class="eyebrow">ABOUT</span>
                <h2>{{ state?.settings.displayName }}</h2>
                <p>本地 DuckDB + SQL + Quarto 分析工作台。数据与报告默认不会上传到外部服务。</p>
                <code>duckdb_report · Go + Vue 3 + Wails</code>
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
            <div v-if="logs.length === 0" class="log-placeholder">运行 SQL、工作流或报告后，日志会出现在这里。</div>
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
          <button class="shortcut-row" @click="runWorkflow">
            <AppIcon name="play" :size="15" /><span>运行整条工作流</span><kbd>⌘ ↵</kbd>
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
          此操作会清空当前工作库中的用户表、视图和外部 DuckDB 连接。工作流、Preset、SQL、QMD 和报告文件都会保留。
        </p>
        <ul class="destructive-list">
          <li>Preset、步骤、SQL、QMD、工作流文件和已生成的报告不会删除。</li>
          <li>之后运行工作流时，导入步骤会重新建立所需数据。</li>
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
          <code>template/format/query</code>；输出 Preset 可配置 ZIP 文件名、目录和打包内容。
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
            {{ importBusy ? "正在导入…" : "导入并加入工作流" }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>
