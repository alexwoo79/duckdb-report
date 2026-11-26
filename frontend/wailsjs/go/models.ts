export namespace model {
	
	export class AppSettings {
	    displayName: string;
	    workspace: string;
	    uiFontSize: number;
	
	    static createFrom(source: any = {}) {
	        return new AppSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.displayName = source["displayName"];
	        this.workspace = source["workspace"];
	        this.uiFontSize = source["uiFontSize"];
	    }
	}
	export class SettingsInfo {
	    displayName: string;
	    version: string;
	    workspace: string;
	    currentWorkspace: string;
	    defaultWorkspace: string;
	    settingsPath: string;
	    workspaceOverride: boolean;
	    uiFontSize: number;
	
	    static createFrom(source: any = {}) {
	        return new SettingsInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.displayName = source["displayName"];
	        this.version = source["version"];
	        this.workspace = source["workspace"];
	        this.currentWorkspace = source["currentWorkspace"];
	        this.defaultWorkspace = source["defaultWorkspace"];
	        this.settingsPath = source["settingsPath"];
	        this.workspaceOverride = source["workspaceOverride"];
	        this.uiFontSize = source["uiFontSize"];
	    }
	}
	export class Environment {
	    duckdbVersion: string;
	    quartoPath: string;
	    quartoVersion: string;
	    rPath: string;
	    rVersion: string;
	    pythonPath: string;
	    pythonVersion: string;
	    jupyterAvailable: boolean;
	    pdfAvailable: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Environment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.duckdbVersion = source["duckdbVersion"];
	        this.quartoPath = source["quartoPath"];
	        this.quartoVersion = source["quartoVersion"];
	        this.rPath = source["rPath"];
	        this.rVersion = source["rVersion"];
	        this.pythonPath = source["pythonPath"];
	        this.pythonVersion = source["pythonVersion"];
	        this.jupyterAvailable = source["jupyterAvailable"];
	        this.pdfAvailable = source["pdfAvailable"];
	    }
	}
	export class RunInfo {
	    id: string;
	    name: string;
	    htmlPath: string;
	    pdfPath: string;
	    modifiedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new RunInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.htmlPath = source["htmlPath"];
	        this.pdfPath = source["pdfPath"];
	        this.modifiedAt = source["modifiedAt"];
	    }
	}
	export class Source {
	    kind: string;
	    path: string;
	    table: string;
	    sheet?: string;
	
	    static createFrom(source: any = {}) {
	        return new Source(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.path = source["path"];
	        this.table = source["table"];
	        this.sheet = source["sheet"];
	    }
	}
	export class TableRef {
	    catalog: string;
	    schema: string;
	    name: string;
	    type: string;
	
	    static createFrom(source: any = {}) {
	        return new TableRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.catalog = source["catalog"];
	        this.schema = source["schema"];
	        this.name = source["name"];
	        this.type = source["type"];
	    }
	}
	export class DatabaseRef {
	    name: string;
	    path: string;
	    type: string;
	    readOnly: boolean;
	    isCurrent: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DatabaseRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.type = source["type"];
	        this.readOnly = source["readOnly"];
	        this.isCurrent = source["isCurrent"];
	    }
	}
	export class FileRef {
	    name: string;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new FileRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	    }
	}
	export class Preset {
	    id: string;
	    type: string;
	    name: string;
	    description: string;
	    params: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new Preset(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.type = source["type"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.params = source["params"];
	    }
	}
	export class Step {
	    id: string;
	    name: string;
	    kind: string;
	    presetId: string;
	    overrides?: Record<string, string>;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Step(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.presetId = source["presetId"];
	        this.overrides = source["overrides"];
	        this.enabled = source["enabled"];
	    }
	}
	export class Workflow {
	    id: string;
	    name: string;
	    description: string;
	    runtime: Record<string, string>;
	    steps: Step[];
	    presets: Preset[];
	
	    static createFrom(source: any = {}) {
	        return new Workflow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.runtime = source["runtime"];
	        this.steps = this.convertValues(source["steps"], Step);
	        this.presets = this.convertValues(source["presets"], Preset);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Bootstrap {
	    workspace: string;
	    database: string;
	    workflow: Workflow;
	    sqlFiles: FileRef[];
	    templates: FileRef[];
	    workflows: FileRef[];
	    databases: DatabaseRef[];
	    tables: TableRef[];
	    sources: Source[];
	    runs: RunInfo[];
	    environment: Environment;
	    settings: SettingsInfo;
	
	    static createFrom(source: any = {}) {
	        return new Bootstrap(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.workspace = source["workspace"];
	        this.database = source["database"];
	        this.workflow = this.convertValues(source["workflow"], Workflow);
	        this.sqlFiles = this.convertValues(source["sqlFiles"], FileRef);
	        this.templates = this.convertValues(source["templates"], FileRef);
	        this.workflows = this.convertValues(source["workflows"], FileRef);
	        this.databases = this.convertValues(source["databases"], DatabaseRef);
	        this.tables = this.convertValues(source["tables"], TableRef);
	        this.sources = this.convertValues(source["sources"], Source);
	        this.runs = this.convertValues(source["runs"], RunInfo);
	        this.environment = this.convertValues(source["environment"], Environment);
	        this.settings = this.convertValues(source["settings"], SettingsInfo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ColumnRef {
	    name: string;
	    type: string;
	
	    static createFrom(source: any = {}) {
	        return new ColumnRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	    }
	}
	export class DataCatalog {
	    databases: DatabaseRef[];
	    tables: TableRef[];
	    sources: Source[];
	
	    static createFrom(source: any = {}) {
	        return new DataCatalog(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.databases = this.convertValues(source["databases"], DatabaseRef);
	        this.tables = this.convertValues(source["tables"], TableRef);
	        this.sources = this.convertValues(source["sources"], Source);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class FileContent {
	    path: string;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new FileContent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.content = source["content"];
	    }
	}
	
	export class ImportRequest {
	    path: string;
	    kind: string;
	    table: string;
	    sheet: string;
	
	    static createFrom(source: any = {}) {
	        return new ImportRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.kind = source["kind"];
	        this.table = source["table"];
	        this.sheet = source["sheet"];
	    }
	}
	export class ImportResult {
	    source: Source;
	    tables: TableRef[];
	    log: string[];
	
	    static createFrom(source: any = {}) {
	        return new ImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = this.convertValues(source["source"], Source);
	        this.tables = this.convertValues(source["tables"], TableRef);
	        this.log = source["log"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class QueryResult {
	    columns: string[];
	    rows: any[][];
	    rowCount: number;
	    truncated: boolean;
	    elapsedMs: number;
	    executedSql: string;
	    rowsAffected: number;
	
	    static createFrom(source: any = {}) {
	        return new QueryResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.columns = source["columns"];
	        this.rows = source["rows"];
	        this.rowCount = source["rowCount"];
	        this.truncated = source["truncated"];
	        this.elapsedMs = source["elapsedMs"];
	        this.executedSql = source["executedSql"];
	        this.rowsAffected = source["rowsAffected"];
	    }
	}
	export class RenderRequest {
	    content: string;
	    format: string;
	    query: string;
	    params: Record<string, string>;
	    saveAs: string;
	    templatePath: string;
	
	    static createFrom(source: any = {}) {
	        return new RenderRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.content = source["content"];
	        this.format = source["format"];
	        this.query = source["query"];
	        this.params = source["params"];
	        this.saveAs = source["saveAs"];
	        this.templatePath = source["templatePath"];
	    }
	}
	export class RenderResult {
	    runId: string;
	    previewUrl: string;
	    previewPath: string;
	    outputPath: string;
	    format: string;
	    log: string[];
	
	    static createFrom(source: any = {}) {
	        return new RenderResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.runId = source["runId"];
	        this.previewUrl = source["previewUrl"];
	        this.previewPath = source["previewPath"];
	        this.outputPath = source["outputPath"];
	        this.format = source["format"];
	        this.log = source["log"];
	    }
	}
	
	export class SQLCompletionTable {
	    catalog: string;
	    schema: string;
	    name: string;
	    type: string;
	    columns: ColumnRef[];
	
	    static createFrom(source: any = {}) {
	        return new SQLCompletionTable(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.catalog = source["catalog"];
	        this.schema = source["schema"];
	        this.name = source["name"];
	        this.type = source["type"];
	        this.columns = this.convertValues(source["columns"], ColumnRef);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SQLCompletionCatalog {
	    tables: SQLCompletionTable[];
	
	    static createFrom(source: any = {}) {
	        return new SQLCompletionCatalog(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tables = this.convertValues(source["tables"], SQLCompletionTable);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	
	
	export class UpdateDownload {
	    name: string;
	    path: string;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new UpdateDownload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.size = source["size"];
	    }
	}
	export class UpdateInfo {
	    currentVersion: string;
	    latestVersion: string;
	    available: boolean;
	    releaseName: string;
	    releaseUrl: string;
	    publishedAt: string;
	    releaseNotes: string;
	    assetName: string;
	    assetSize: number;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.currentVersion = source["currentVersion"];
	        this.latestVersion = source["latestVersion"];
	        this.available = source["available"];
	        this.releaseName = source["releaseName"];
	        this.releaseUrl = source["releaseUrl"];
	        this.publishedAt = source["publishedAt"];
	        this.releaseNotes = source["releaseNotes"];
	        this.assetName = source["assetName"];
	        this.assetSize = source["assetSize"];
	    }
	}
	
	export class WorkflowRequest {
	    workflow: Workflow;
	    params: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new WorkflowRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.workflow = this.convertValues(source["workflow"], Workflow);
	        this.params = source["params"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class WorkflowResult {
	    runId: string;
	    query: QueryResult;
	    previewUrl: string;
	    previewPath: string;
	    packagePath: string;
	    log: string[];
	    startedAt: string;
	    finishedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkflowResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.runId = source["runId"];
	        this.query = this.convertValues(source["query"], QueryResult);
	        this.previewUrl = source["previewUrl"];
	        this.previewPath = source["previewPath"];
	        this.packagePath = source["packagePath"];
	        this.log = source["log"];
	        this.startedAt = source["startedAt"];
	        this.finishedAt = source["finishedAt"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

