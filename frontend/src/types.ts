export type PresetType = "datasource" | "sqlset" | "report" | "output";
export type StepKind = "import" | "sql" | "render" | "export";
export type ViewName = "workflow" | "data" | "sql" | "report" | "settings";

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
  nullable: boolean;
  defaultValue: string;
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

export interface UpdateInfo {
  currentVersion: string;
  latestVersion: string;
  available: boolean;
  releaseName: string;
  releaseUrl: string;
  publishedAt: string;
  releaseNotes: string;
  assetName: string;
  assetSize: number;
}

export interface UpdateDownload {
  name: string;
  path: string;
  size: number;
}

export type UpdateProgressPhase =
  | "preparing"
  | "downloading"
  | "verifying"
  | "complete";

export interface UpdateProgress {
  phase: UpdateProgressPhase;
  downloaded: number;
  total: number;
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

export type TableWriteOperation = "insert" | "update";

export interface TableValue {
  column: string;
  value: string;
  null: boolean;
}

export interface TableFilter {
  column: string;
  operator:
    | "eq"
    | "ne"
    | "lt"
    | "lte"
    | "gt"
    | "gte"
    | "like"
    | "ilike"
    | "is_null"
    | "is_not_null";
  value: string;
}

export interface TableWriteRequest {
  operation: TableWriteOperation;
  table: TableRef;
  values: TableValue[];
  filters: TableFilter[];
}

export interface TableWriteResult {
  table: TableRef;
  operation: TableWriteOperation;
  rowsAffected: number;
  sql: string;
}

export type TableRelationDirection = "incoming" | "outgoing";

export interface TableRelation {
  name: string;
  direction: TableRelationDirection;
  source: TableRef;
  sourceColumns: string[];
  target: TableRef;
  targetColumns: string[];
}

export interface TableKey {
  name: string;
  type: "PRIMARY KEY" | "UNIQUE";
  columns: string[];
}

export interface TableEditorInfo {
  table: TableRef;
  columns: ColumnRef[];
  keys: TableKey[];
  keyColumns: string[];
  keyType: string;
  writable: boolean;
  estimatedRows: number;
  columnCount: number;
  indexCount: number;
  comment: string;
  sql: string;
  relations: TableRelation[];
}

export type TableStructureOperation =
  | "rename_table"
  | "add_column"
  | "rename_column"
  | "alter_column_type"
  | "drop_column"
  | "set_not_null"
  | "drop_not_null"
  | "drop_default"
  | "add_primary_key"
  | "add_unique_key";

export interface TableStructureRequest {
  operation: TableStructureOperation;
  table: TableRef;
  column: string;
  columns: string[];
  newName: string;
  columnType: string;
}

export interface TableStructurePreview {
  operation: TableStructureOperation;
  sql: string;
  warnings: string[];
}

export interface TableStructureResult {
  table: TableRef;
  operation: TableStructureOperation;
  sql: string;
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
  LoadDataCatalog(): Promise<DataCatalog>;
  DeleteSource(source: DataSource): Promise<DataCatalog>;
  ResetUserData(): Promise<DataCatalog>;
  RunSQL(query: string, limit: number, basePath: string): Promise<QueryResult>;
  WriteTableRow(request: TableWriteRequest): Promise<TableWriteResult>;
  LoadTableEditorInfo(table: TableRef): Promise<TableEditorInfo>;
  PreviewTableStructure(
    request: TableStructureRequest,
  ): Promise<TableStructurePreview>;
  AlterTableStructure(
    request: TableStructureRequest,
  ): Promise<TableStructureResult>;
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
  CheckForUpdates(): Promise<UpdateInfo>;
  DownloadUpdate(): Promise<UpdateDownload>;
  OpenUpdateRelease(): Promise<void>;
  OpenDownloadedUpdate(): Promise<void>;
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
      EventsOn<T = unknown>(
        event: string,
        callback: (payload: T) => void,
      ): () => void;
    };
  }
}
