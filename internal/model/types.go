package model

const (
	PresetDataSource = "datasource"
	PresetSQLSet     = "sqlset"
	PresetReport     = "report"
	PresetOutput     = "output"

	StepImport = "import"
	StepSQL    = "sql"
	StepRender = "render"
	StepExport = "export"
)

type Preset struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Params      map[string]string `json:"params"`
}

type Step struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Kind      string            `json:"kind"`
	PresetID  string            `json:"presetId"`
	Overrides map[string]string `json:"overrides,omitempty"`
	Enabled   bool              `json:"enabled"`
}

type Workflow struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Runtime     map[string]string `json:"runtime"`
	Steps       []Step            `json:"steps"`
	Presets     []Preset          `json:"presets"`
}

type FileRef struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type Source struct {
	Kind  string `json:"kind"`
	Path  string `json:"path"`
	Table string `json:"table"`
	Sheet string `json:"sheet,omitempty"`
}

type TableRef struct {
	Catalog string `json:"catalog"`
	Schema  string `json:"schema"`
	Name    string `json:"name"`
	Type    string `json:"type"`
}

type Environment struct {
	DuckDBVersion    string `json:"duckdbVersion"`
	QuartoPath       string `json:"quartoPath"`
	QuartoVersion    string `json:"quartoVersion"`
	RPath            string `json:"rPath"`
	RVersion         string `json:"rVersion"`
	PythonPath       string `json:"pythonPath"`
	PythonVersion    string `json:"pythonVersion"`
	JupyterAvailable bool   `json:"jupyterAvailable"`
	PDFAvailable     bool   `json:"pdfAvailable"`
}

type AppSettings struct {
	DisplayName string `json:"displayName"`
	Workspace   string `json:"workspace"`
}

type SettingsInfo struct {
	DisplayName       string `json:"displayName"`
	Version           string `json:"version"`
	Workspace         string `json:"workspace"`
	CurrentWorkspace  string `json:"currentWorkspace"`
	DefaultWorkspace  string `json:"defaultWorkspace"`
	SettingsPath      string `json:"settingsPath"`
	WorkspaceOverride bool   `json:"workspaceOverride"`
}

type Bootstrap struct {
	Workspace   string       `json:"workspace"`
	Database    string       `json:"database"`
	Workflow    Workflow     `json:"workflow"`
	SQLFiles    []FileRef    `json:"sqlFiles"`
	Templates   []FileRef    `json:"templates"`
	Workflows   []FileRef    `json:"workflows"`
	Tables      []TableRef   `json:"tables"`
	Sources     []Source     `json:"sources"`
	Runs        []RunInfo    `json:"runs"`
	Environment Environment  `json:"environment"`
	Settings    SettingsInfo `json:"settings"`
}

type QueryResult struct {
	Columns      []string `json:"columns"`
	Rows         [][]any  `json:"rows"`
	RowCount     int      `json:"rowCount"`
	Truncated    bool     `json:"truncated"`
	ElapsedMS    int64    `json:"elapsedMs"`
	ExecutedSQL  string   `json:"executedSql"`
	RowsAffected int64    `json:"rowsAffected"`
}

type ImportRequest struct {
	Path  string `json:"path"`
	Kind  string `json:"kind"`
	Table string `json:"table"`
	Sheet string `json:"sheet"`
}

type ImportResult struct {
	Source Source     `json:"source"`
	Tables []TableRef `json:"tables"`
	Log    []string   `json:"log"`
}

type DataCatalog struct {
	Tables  []TableRef `json:"tables"`
	Sources []Source   `json:"sources"`
}

type WorkflowRequest struct {
	Workflow Workflow          `json:"workflow"`
	Params   map[string]string `json:"params"`
}

type StepEvent struct {
	RunID      string `json:"runId"`
	StepID     string `json:"stepId"`
	StepName   string `json:"stepName"`
	Status     string `json:"status"`
	Message    string `json:"message"`
	DurationMS int64  `json:"durationMs"`
}

type WorkflowResult struct {
	RunID       string      `json:"runId"`
	Query       QueryResult `json:"query"`
	PreviewURL  string      `json:"previewUrl"`
	PreviewPath string      `json:"previewPath"`
	PackagePath string      `json:"packagePath"`
	Log         []string    `json:"log"`
	StartedAt   string      `json:"startedAt"`
	FinishedAt  string      `json:"finishedAt"`
}

type RenderRequest struct {
	Content      string            `json:"content"`
	Format       string            `json:"format"`
	Query        string            `json:"query"`
	Params       map[string]string `json:"params"`
	SaveAs       string            `json:"saveAs"`
	TemplatePath string            `json:"templatePath"`
}

type RenderResult struct {
	RunID       string   `json:"runId"`
	PreviewURL  string   `json:"previewUrl"`
	PreviewPath string   `json:"previewPath"`
	OutputPath  string   `json:"outputPath"`
	Format      string   `json:"format"`
	Log         []string `json:"log"`
}

type RunInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	HTMLPath   string `json:"htmlPath"`
	PDFPath    string `json:"pdfPath"`
	ModifiedAt int64  `json:"modifiedAt"`
}

type FileContent struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}
