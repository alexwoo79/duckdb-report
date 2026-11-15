package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"github.com/alexwoo79/duckdb-report/internal/database"
	"github.com/alexwoo79/duckdb-report/internal/model"
	"github.com/alexwoo79/duckdb-report/internal/preview"
	"github.com/alexwoo79/duckdb-report/internal/report"
	"github.com/alexwoo79/duckdb-report/internal/runner"
	"github.com/alexwoo79/duckdb-report/internal/settings"
	"github.com/alexwoo79/duckdb-report/internal/workflow"
	"github.com/alexwoo79/duckdb-report/internal/workflowbundle"
	"github.com/alexwoo79/duckdb-report/internal/workspace"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx               context.Context
	store             *workspace.Store
	database          *database.Service
	renderer          *report.Renderer
	runner            *runner.Runner
	settings          *settings.Store
	config            model.AppSettings
	defaultWorkspace  string
	workspaceOverride bool
	initErr           error
	mu                sync.Mutex
}

func NewApp(exampleFS fs.FS) *App {
	app := &App{}
	report.EnsureRuntimePath()
	configDir, err := os.UserConfigDir()
	if err != nil {
		app.initErr = fmt.Errorf("find application data directory: %w", err)
		return app
	}
	configRoot := filepath.Join(configDir, "duckdb_report")
	settingsStore := settings.New(configRoot, configRoot)
	savedSettings, err := settingsStore.Load()
	if err != nil {
		app.initErr = err
		return app
	}
	root := savedSettings.Workspace
	override := strings.TrimSpace(os.Getenv("DUCKDB_REPORT_HOME"))
	if override != "" {
		root = override
	}
	if root == "" {
		root = configRoot
	}
	store, err := workspace.New(root, exampleFS)
	if err != nil {
		app.initErr = err
		return app
	}
	db, err := database.Open(context.Background(), store.Database)
	if err != nil {
		app.initErr = err
		return app
	}
	quarto, _ := exec.LookPath("quarto")
	renderer := report.NewRenderer(quarto, store, db)
	app.store = store
	app.database = db
	app.renderer = renderer
	app.runner = runner.New(store, db, renderer)
	app.settings = settingsStore
	app.config = savedSettings
	app.defaultWorkspace = configRoot
	app.workspaceOverride = override != ""
	return app
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if a.initErr != nil {
		return
	}
	sources, err := a.store.LoadSources()
	if err != nil {
		a.initErr = err
		return
	}
	if err := a.database.Reattach(ctx, sources); err != nil {
		a.initErr = err
		return
	}
	if err := a.ensureDemoData(ctx); err != nil {
		a.initErr = err
		return
	}
	if err := a.ensureExampleData(ctx); err != nil {
		a.initErr = err
	}
}

func (a *App) shutdown(context.Context) {
	if a.database != nil {
		_ = a.database.Close()
	}
}

func (a *App) Bootstrap() (model.Bootstrap, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.initErr != nil {
		return model.Bootstrap{}, a.initErr
	}
	current, err := a.store.LoadWorkflow()
	if err != nil {
		return model.Bootstrap{}, err
	}
	sqlFiles, err := a.store.ListSQL()
	if err != nil {
		return model.Bootstrap{}, err
	}
	templates, err := a.store.ListTemplates()
	if err != nil {
		return model.Bootstrap{}, err
	}
	workflows, err := a.store.ListWorkflows()
	if err != nil {
		return model.Bootstrap{}, err
	}
	tables, err := a.database.ListTables(a.operationContext())
	if err != nil {
		return model.Bootstrap{}, err
	}
	sources, err := a.store.LoadSources()
	if err != nil {
		return model.Bootstrap{}, err
	}
	runs, err := a.store.ListRuns()
	if err != nil {
		return model.Bootstrap{}, err
	}
	if tables == nil {
		tables = []model.TableRef{}
	}
	if sources == nil {
		sources = []model.Source{}
	}
	if runs == nil {
		runs = []model.RunInfo{}
	}
	return model.Bootstrap{
		Workspace:   a.store.Root,
		Database:    a.store.Database,
		Workflow:    current,
		SQLFiles:    sqlFiles,
		Templates:   templates,
		Workflows:   workflows,
		Tables:      tables,
		Sources:     sources,
		Runs:        runs,
		Environment: a.renderer.Environments(a.operationContext()),
		Settings:    a.settingsInfoLocked(),
	}, nil
}

func (a *App) displayName() string {
	if strings.TrimSpace(a.config.DisplayName) == "" {
		return "DuckDB Report Studio"
	}
	return a.config.DisplayName
}

func (a *App) PickDataFile() (string, error) {
	if a.initErr != nil {
		return "", a.initErr
	}
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择 CSV、Excel、Parquet 或 DuckDB 文件",
		Filters: []runtime.FileFilter{
			{DisplayName: "数据文件", Pattern: "*.csv;*.xlsx;*.xlsm;*.json;*.parquet;*.duckdb;*.db"},
			{DisplayName: "所有文件", Pattern: "*.*"},
		},
	})
}

func (a *App) PickSQLFile() (string, error) {
	if a.initErr != nil {
		return "", a.initErr
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "选择 SQL 文件",
		DefaultDirectory: a.store.SQL,
		Filters: []runtime.FileFilter{
			{DisplayName: "SQL 文件", Pattern: "*.sql"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("choose sql file: %w", err)
	}
	if path == "" {
		return "", nil
	}
	relative, err := filepath.Rel(a.store.Root, path)
	if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(relative), nil
	}
	return path, nil
}

func (a *App) PickWorkspaceDirectory() (string, error) {
	if a.initErr != nil {
		return "", a.initErr
	}
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:                "选择 DuckDB Report Studio 工作区",
		DefaultDirectory:     a.store.Root,
		CanCreateDirectories: true,
	})
}

func (a *App) OpenWorkspace() error {
	if a.initErr != nil {
		return a.initErr
	}
	return openWithSystem(a.store.Root)
}

func (a *App) ExportWorkspaceDuckDB() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.initErr != nil {
		return "", a.initErr
	}
	destination, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "导出工作空间 DuckDB",
		DefaultFilename: "duckdb-report-workspace-" + time.Now().Format("20060102-150405") + ".duckdb",
		Filters: []runtime.FileFilter{
			{DisplayName: "DuckDB 数据库", Pattern: "*.duckdb"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("choose duckdb export destination: %w", err)
	}
	if destination == "" {
		return "", nil
	}
	if !strings.HasSuffix(strings.ToLower(destination), ".duckdb") {
		destination += ".duckdb"
	}
	if sameFilePath(destination, a.store.Database) {
		return "", errors.New("cannot overwrite the active workspace database")
	}
	temporary := filepath.Join(
		filepath.Dir(destination),
		"."+filepath.Base(destination)+".tmp-"+time.Now().Format("20060102150405.000000000"),
	)
	if err := os.Remove(temporary); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("clear temporary duckdb export: %w", err)
	}
	ctx, cancel := context.WithTimeout(a.operationContext(), 10*time.Minute)
	defer cancel()
	if err := a.database.Snapshot(ctx, temporary); err != nil {
		return "", fmt.Errorf("export workspace duckdb: %w", err)
	}
	if err := replaceFile(temporary, destination); err != nil {
		os.Remove(temporary)
		return "", err
	}
	return destination, nil
}

func (a *App) SaveSettings(value model.AppSettings) (model.SettingsInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.initErr != nil {
		return model.SettingsInfo{}, a.initErr
	}
	if a.settings == nil {
		return model.SettingsInfo{}, errors.New("settings store is not available")
	}
	if err := a.settings.Save(value); err != nil {
		return model.SettingsInfo{}, err
	}
	normalized, err := a.settings.Load()
	if err != nil {
		return model.SettingsInfo{}, err
	}
	a.config = normalized
	runtime.WindowSetTitle(a.ctx, a.displayName())
	return a.settingsInfoLocked(), nil
}

func (a *App) ListSheets(path string) ([]string, error) {
	extension := strings.ToLower(filepath.Ext(path))
	if extension != ".xlsx" && extension != ".xlsm" {
		return nil, nil
	}
	return database.SortedSheets(path)
}

func (a *App) ImportData(request model.ImportRequest) (model.ImportResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.initErr != nil {
		return model.ImportResult{}, a.initErr
	}
	result, err := a.database.Import(a.operationContext(), request)
	if err != nil {
		return model.ImportResult{}, err
	}
	if err := a.store.SaveSource(result.Source); err != nil {
		return model.ImportResult{}, err
	}
	return result, nil
}

func (a *App) DeleteSource(source model.Source) (model.DataCatalog, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.initErr != nil {
		return model.DataCatalog{}, a.initErr
	}
	if err := a.database.DeleteSource(a.operationContext(), source); err != nil {
		return model.DataCatalog{}, err
	}
	if err := a.store.RemoveSource(source); err != nil {
		return model.DataCatalog{}, err
	}
	return a.dataCatalogLocked()
}

func (a *App) ResetUserData() (model.DataCatalog, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.initErr != nil {
		return model.DataCatalog{}, a.initErr
	}
	sources, err := a.store.LoadSources()
	if err != nil {
		return model.DataCatalog{}, err
	}
	if err := a.database.ResetUserData(a.operationContext(), sources); err != nil {
		return model.DataCatalog{}, err
	}
	if err := a.store.ClearSources(); err != nil {
		return model.DataCatalog{}, err
	}
	return a.dataCatalogLocked()
}

func (a *App) RunSQL(query string, limit int, basePath string) (model.QueryResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.initErr != nil {
		return model.QueryResult{}, a.initErr
	}
	baseDir := a.store.Root
	if strings.TrimSpace(basePath) != "" {
		resolved, err := a.store.ResolveExisting(basePath)
		if err != nil {
			return model.QueryResult{}, err
		}
		baseDir = filepath.Dir(resolved)
	}
	expanded, err := database.ExpandScript(query, baseDir)
	if err != nil {
		return model.QueryResult{}, err
	}
	ctx, cancel := context.WithTimeout(a.operationContext(), 3*time.Minute)
	defer cancel()
	return a.database.Query(ctx, expanded, limit)
}

func (a *App) ReadText(path string) (model.FileContent, error) {
	if a.initErr != nil {
		return model.FileContent{}, a.initErr
	}
	return a.store.ReadText(path)
}

func (a *App) SaveText(path, content string) (model.FileContent, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.initErr != nil {
		return model.FileContent{}, a.initErr
	}
	return a.store.WriteText(path, content)
}

func (a *App) SaveSQLAs(content string) (model.FileContent, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.initErr != nil {
		return model.FileContent{}, a.initErr
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:            "新建或另存 SQL 文件",
		DefaultDirectory: a.store.SQL,
		DefaultFilename:  "query-" + time.Now().Format("20060102-150405") + ".sql",
		Filters: []runtime.FileFilter{
			{DisplayName: "SQL 文件", Pattern: "*.sql"},
		},
	})
	if err != nil {
		return model.FileContent{}, fmt.Errorf("choose sql destination: %w", err)
	}
	if path == "" {
		return model.FileContent{}, nil
	}
	if !strings.HasSuffix(strings.ToLower(path), ".sql") {
		path += ".sql"
	}
	relative, err := filepath.Rel(a.store.Root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return model.FileContent{}, errors.New("SQL 文件必须保存在当前工作区内")
	}
	return a.store.WriteText(filepath.ToSlash(relative), content)
}

func (a *App) SaveWorkflow(value model.Workflow) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.initErr != nil {
		return a.initErr
	}
	return a.store.SaveWorkflow(value)
}

func (a *App) ExportWorkflowPackage() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.initErr != nil {
		return "", a.initErr
	}
	current, err := a.store.LoadWorkflow()
	if err != nil {
		return "", err
	}
	destination, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "导出工作流包",
		DefaultFilename: current.ID + "-workflow.zip",
		Filters: []runtime.FileFilter{
			{DisplayName: "DuckDB Report Workflow", Pattern: "*.zip"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("choose workflow package destination: %w", err)
	}
	if destination == "" {
		return "", nil
	}
	if !strings.HasSuffix(strings.ToLower(destination), ".zip") {
		destination += ".zip"
	}
	temporary := destination + ".tmp-" + time.Now().Format("20060102150405.000000000")
	file, err := os.Create(temporary)
	if err != nil {
		return "", fmt.Errorf("create workflow package: %w", err)
	}
	exportErr := workflowbundle.Export(file, a.store, current)
	closeErr := file.Close()
	if exportErr != nil {
		os.Remove(temporary)
		return "", exportErr
	}
	if closeErr != nil {
		os.Remove(temporary)
		return "", fmt.Errorf("close workflow package: %w", closeErr)
	}
	if err := replaceFile(temporary, destination); err != nil {
		os.Remove(temporary)
		return "", err
	}
	return destination, nil
}

func (a *App) ImportWorkflowPackage() (model.Workflow, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.initErr != nil {
		return model.Workflow{}, a.initErr
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "导入工作流包",
		Filters: []runtime.FileFilter{
			{DisplayName: "工作流文件", Pattern: "*.zip;*.json"},
		},
	})
	if err != nil {
		return model.Workflow{}, fmt.Errorf("choose workflow package: %w", err)
	}
	if path == "" {
		return model.Workflow{}, nil
	}
	if strings.HasSuffix(strings.ToLower(path), ".json") {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return model.Workflow{}, fmt.Errorf("read workflow JSON: %w", readErr)
		}
		var imported model.Workflow
		if decodeErr := json.Unmarshal(data, &imported); decodeErr != nil {
			return model.Workflow{}, fmt.Errorf("decode workflow JSON: %w", decodeErr)
		}
		if validateErr := workflow.Validate(imported); validateErr != nil {
			return model.Workflow{}, validateErr
		}
		if saveErr := a.store.SaveWorkflow(imported); saveErr != nil {
			return model.Workflow{}, saveErr
		}
		return imported, nil
	}
	return workflowbundle.Import(path, a.store)
}

func (a *App) RenderReport(request model.RenderRequest) (model.RenderResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.initErr != nil {
		return model.RenderResult{}, a.initErr
	}
	ctx, cancel := context.WithTimeout(a.operationContext(), 8*time.Minute)
	defer cancel()
	return a.renderer.Render(ctx, request)
}

func (a *App) ExportQueryCSV(result model.QueryResult) (string, error) {
	if len(result.Columns) == 0 {
		return "", errors.New("query result has no columns to export")
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "导出查询结果为 CSV",
		DefaultFilename: "query-result-" + time.Now().Format("20060102-150405") + ".csv",
		Filters: []runtime.FileFilter{
			{DisplayName: "CSV 文件", Pattern: "*.csv"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("choose csv output: %w", err)
	}
	if path == "" {
		return "", nil
	}
	if !strings.HasSuffix(strings.ToLower(path), ".csv") {
		path += ".csv"
	}
	file, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("create csv output: %w", err)
	}
	if err := database.WriteQueryCSV(file, result); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close csv output: %w", err)
	}
	return path, nil
}

func (a *App) RunWorkflow(request model.WorkflowRequest) (model.WorkflowResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.initErr != nil {
		return model.WorkflowResult{}, a.initErr
	}
	if err := a.store.SaveWorkflow(request.Workflow); err != nil {
		return model.WorkflowResult{}, err
	}
	ctx, cancel := context.WithTimeout(a.operationContext(), 20*time.Minute)
	defer cancel()
	return a.runner.Run(ctx, request, func(event model.StepEvent) {
		runtime.EventsEmit(a.ctx, "workflow:event", event)
	})
}

func (a *App) OpenOutput(runID, format string) error {
	if a.initErr != nil {
		return a.initErr
	}
	runDir, err := a.store.RunDir(runID)
	if err != nil {
		return err
	}
	output, err := outputPath(runDir, format)
	if err != nil {
		return err
	}
	return openWithSystem(output)
}

func (a *App) SaveOutput(runID, format string) (string, error) {
	if a.initErr != nil {
		return "", a.initErr
	}
	runDir, err := a.store.RunDir(runID)
	if err != nil {
		return "", err
	}
	source, err := outputPath(runDir, format)
	if err != nil {
		return "", err
	}
	extension := strings.ToLower(strings.TrimPrefix(filepath.Ext(source), "."))
	destination, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "保存报告文件",
		DefaultFilename: "duckdb-report-" + runID + "." + extension,
		Filters: []runtime.FileFilter{
			{DisplayName: strings.ToUpper(extension) + " 文件", Pattern: "*." + extension},
		},
	})
	if err != nil {
		return "", fmt.Errorf("choose report destination: %w", err)
	}
	if destination == "" {
		return "", nil
	}
	if !strings.EqualFold(filepath.Ext(destination), "."+extension) {
		destination += "." + extension
	}
	if err := copyFile(source, destination); err != nil {
		return "", err
	}
	return destination, nil
}

func (a *App) previewHandler() *preview.Handler {
	if a.store == nil {
		return nil
	}
	return preview.NewHandler(a.store)
}

func (a *App) operationContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *App) ensureDemoData(ctx context.Context) error {
	tables, err := a.database.ListTables(ctx)
	if err != nil {
		return err
	}
	for _, table := range tables {
		if table.Catalog == "workspace" && table.Schema == "main" && table.Name == "sales" {
			return nil
		}
	}
	samplePath := filepath.Join(a.store.Data, "sales.csv")
	if _, err := os.Stat(samplePath); err != nil {
		return nil
	}
	imported, err := a.database.Import(ctx, model.ImportRequest{
		Path: samplePath, Table: "sales",
	})
	if err != nil {
		return fmt.Errorf("import starter sales data: %w", err)
	}
	return a.store.SaveSource(imported.Source)
}

func (a *App) ensureExampleData(ctx context.Context) error {
	marker := filepath.Join(a.store.Root, ".monthly-sales-example-v1")
	if _, err := os.Stat(marker); err == nil {
		if _, queryErr := a.database.Query(ctx, "SELECT customers FROM kpi_summary LIMIT 0", 1); queryErr == nil {
			return nil
		}
	}
	exampleRoot := filepath.Join(a.store.Root, "examples", "monthly_sales")
	dataFiles := []struct {
		name  string
		table string
	}{
		{name: "customers.csv", table: "customers"},
		{name: "products.csv", table: "products"},
		{name: "orders.csv", table: "orders"},
		{name: "order_items.csv", table: "order_items"},
	}
	for _, source := range dataFiles {
		path := filepath.Join(exampleRoot, "data", source.name)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return fmt.Errorf("inspect example data: %w", err)
		}
	}
	for _, source := range dataFiles {
		path := filepath.Join(exampleRoot, "data", source.name)
		imported, err := a.database.Import(ctx, model.ImportRequest{
			Path: path, Kind: "csv", Table: source.table,
		})
		if err != nil {
			return fmt.Errorf("seed example table %s: %w", source.table, err)
		}
		if err := a.store.SaveSource(imported.Source); err != nil {
			return err
		}
	}
	replacements := strings.NewReplacer(
		"{{start_date}}", "2026-01-01",
		"{{end_date}}", "2026-12-31",
		"{{top_n}}", "8",
	)
	for _, name := range []string{"01_join.sql", "02_summary.sql", "03_pivot.sql", "04_validation.sql"} {
		path := filepath.Join(exampleRoot, "sql", name)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read example SQL %s: %w", name, err)
		}
		query := replacements.Replace(string(data))
		if _, err := a.database.Query(ctx, query, 500); err != nil {
			return fmt.Errorf("execute example SQL %s: %w", name, err)
		}
	}
	if err := os.WriteFile(marker, []byte("completed\n"), 0o644); err != nil {
		return fmt.Errorf("write example initialization marker: %w", err)
	}
	return nil
}

func (a *App) ValidateWorkflow(value model.Workflow) error {
	return workflow.Validate(value)
}

func (a *App) dataCatalogLocked() (model.DataCatalog, error) {
	tables, err := a.database.ListTables(a.operationContext())
	if err != nil {
		return model.DataCatalog{}, err
	}
	if tables == nil {
		tables = []model.TableRef{}
	}
	sources, err := a.store.LoadSources()
	if err != nil {
		return model.DataCatalog{}, err
	}
	if sources == nil {
		sources = []model.Source{}
	}
	return model.DataCatalog{Tables: tables, Sources: sources}, nil
}

func (a *App) settingsInfoLocked() model.SettingsInfo {
	settingsPath := ""
	if a.settings != nil {
		settingsPath = a.settings.Path
	}
	currentWorkspace := ""
	if a.store != nil {
		currentWorkspace = a.store.Root
	}
	return model.SettingsInfo{
		DisplayName:       a.displayName(),
		Workspace:         a.config.Workspace,
		CurrentWorkspace:  currentWorkspace,
		DefaultWorkspace:  a.defaultWorkspace,
		SettingsPath:      settingsPath,
		WorkspaceOverride: a.workspaceOverride,
	}
}

func outputPath(runDir, format string) (string, error) {
	format = strings.ToLower(strings.TrimSpace(format))
	if format != "html" && format != "pdf" {
		return "", fmt.Errorf("unsupported report format %q", format)
	}
	path := filepath.Join(runDir, "report."+format)
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("report %s output was not found: %w", format, err)
	}
	if info.IsDir() {
		return "", errors.New("report output is a directory")
	}
	return path, nil
}

func openWithSystem(path string) error {
	var command *exec.Cmd
	switch goruntime.GOOS {
	case "darwin":
		command = exec.Command("open", path)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	default:
		command = exec.Command("xdg-open", path)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("open report with system application: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open report output: %w", err)
	}
	defer input.Close()
	output, err := os.Create(destination)
	if err != nil {
		return fmt.Errorf("create report destination: %w", err)
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		return fmt.Errorf("copy report output: %w", err)
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("close report destination: %w", err)
	}
	return nil
}

func sameFilePath(left, right string) bool {
	leftAbsolute, leftErr := filepath.Abs(left)
	rightAbsolute, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return filepath.Clean(left) == filepath.Clean(right)
	}
	return filepath.Clean(leftAbsolute) == filepath.Clean(rightAbsolute)
}

func replaceFile(temporary, destination string) error {
	backup := ""
	if _, err := os.Stat(destination); err == nil {
		backup = destination + ".previous-" + time.Now().Format("20060102150405")
		if err := os.Rename(destination, backup); err != nil {
			return fmt.Errorf("back up existing duckdb export: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect duckdb export destination: %w", err)
	}
	if err := os.Rename(temporary, destination); err != nil {
		if backup != "" {
			_ = os.Rename(backup, destination)
		}
		return fmt.Errorf("replace duckdb export: %w", err)
	}
	if backup != "" {
		if err := os.Remove(backup); err != nil {
			return fmt.Errorf("remove previous duckdb export: %w", err)
		}
	}
	return nil
}
