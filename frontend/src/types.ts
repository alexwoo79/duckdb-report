export type PresetType = "datasource" | "sqlset" | "report" | "output";
export type StepKind = "import" | "sql" | "render" | "export";
export type ViewName = "workflow" | "sql" | "report" | "settings";

export interface Preset {
  id: string;
  type: PresetType;
  name: string;
  description: string;
  params: Record<string, string>;
}

export interface WorkflowStep {
  id: string;
  name: string;
  kind: StepKind;
  presetId: string;
  overrides?: Record<string, string>;
  enabled: boolean;
}

export interface Workflow {
  id: string;
  name: string;
  description: string;
  runtime: Record<string, string>;
  presets: Preset[];
  steps: WorkflowStep[];
}

export interface FileRef {
  name: string;
  path: string;
}

export interface TableRef {
  catalog: string;
  schema: string;
  name: string;
  type: string;
}

export interface DatabaseRef {
  name: string;
  path: string;
  type: string;
  readOnly: boolean;
  isCurrent: boolean;
}

export interface ColumnRef {
  name: string;
  type: string;
}

export interface SQLCompletionTable extends TableRef {
  columns: ColumnRef[];
}

export interface SQLCompletionCatalog {
  tables: SQLCompletionTable[];
}

export interface DataSource {
  kind: string;
  path: string;
  table: string;
  sheet?: string;
}

export interface Environment {
  duckdbVersion: string;
  quartoPath: string;
  quartoVersion: string;
  rPath: string;
  rVersion: string;
  pythonPath: string;
  pythonVersion: string;
  jupyterAvailable: boolean;
  pdfAvailable: boolean;
}

export interface AppSettings {
  displayName: string;
  workspace: string;
  uiFontSize: number;
}

export interface SettingsInfo {
  displayName: string;
  version: string;
  workspace: string;
  currentWorkspace: string;
  defaultWorkspace: string;
  settingsPath: string;
  workspaceOverride: boolean;
  uiFontSize: number;
}

export interface RunInfo {
  id: string;
  name: string;
  htmlPath: string;
  pdfPath: string;
  modifiedAt: number;
}

export interface Bootstrap {
  workspace: string;
  database: string;
  workflow: Workflow;
  sqlFiles: FileRef[];
  templates: FileRef[];
  workflows: FileRef[];
  databases: DatabaseRef[];
  tables: TableRef[];
  sources: DataSource[];
  runs: RunInfo[];
  environment: Environment;
  settings: SettingsInfo;
}

export interface QueryResult {
  columns: string[];
  rows: unknown[][];
  rowCount: number;
  truncated: boolean;
  elapsedMs: number;
  executedSql: string;
  rowsAffected: number;
}

export interface StepEvent {
  runId: string;
  stepId: string;
  stepName: string;
  status: "running" | "success" | "error";
  message: string;
  durationMs: number;
}

export interface ImportResult {
  source: DataSource;
  tables: TableRef[];
  log: string[];
}

export interface DataCatalog {
  databases: DatabaseRef[];
  tables: TableRef[];
  sources: DataSource[];
}

export interface WorkflowResult {
  runId: string;
  query: QueryResult;
  previewUrl: string;
  previewPath: string;
  packagePath: string;
  log: string[];
  startedAt: string;
  finishedAt: string;
}

export interface RenderResult {
  runId: string;
  previewUrl: string;
  previewPath: string;
  outputPath: string;
  format: "html" | "pdf";
  log: string[];
}

export interface FileContent {
  path: string;
  content: string;
}

export interface Backend {
  Bootstrap(): Promise<Bootstrap>;
  PickDataFile(): Promise<string>;
  PickSQLFile(): Promise<string>;
  PickWorkspaceDirectory(): Promise<string>;
  OpenWorkspace(): Promise<void>;
  ExportWorkspaceDuckDB(): Promise<string>;
  LoadSQLCompletions(): Promise<SQLCompletionCatalog>;
  ListSheets(path: string): Promise<string[]>;
  ImportData(request: {
    path: string;
    kind: string;
    table: string;
    sheet: string;
  }): Promise<ImportResult>;
  DeleteSource(source: DataSource): Promise<DataCatalog>;
  ResetUserData(): Promise<DataCatalog>;
  RunSQL(query: string, limit: number, basePath: string): Promise<QueryResult>;
  ReadText(path: string): Promise<FileContent>;
  SaveText(path: string, content: string): Promise<FileContent>;
  SaveSQLAs(content: string): Promise<FileContent>;
  SaveQMDAs(content: string): Promise<FileContent>;
  SaveWorkflow(workflow: Workflow): Promise<void>;
  ExportWorkflowPackage(): Promise<string>;
  ImportWorkflowPackage(): Promise<Workflow>;
  RunWorkflow(request: {
    workflow: Workflow;
    params: Record<string, string>;
  }): Promise<WorkflowResult>;
  RenderReport(request: {
    content: string;
    format: "html" | "pdf";
    query: string;
    params: Record<string, string>;
    saveAs: string;
    templatePath: string;
  }): Promise<RenderResult>;
  ExportQueryCSV(result: QueryResult): Promise<string>;
  ExportQueryParquet(result: QueryResult): Promise<string>;
  OpenOutput(runId: string, format: "html" | "pdf"): Promise<void>;
  SaveOutput(runId: string, format: "html" | "pdf"): Promise<string>;
  SaveSettings(settings: AppSettings): Promise<SettingsInfo>;
  ValidateWorkflow(workflow: Workflow): Promise<void>;
}

declare global {
  interface Window {
    go?: {
      main?: {
        App?: Backend;
      };
    };
    runtime?: {
      EventsOn(event: string, callback: (payload: StepEvent) => void): () => void;
    };
  }
}
