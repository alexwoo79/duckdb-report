package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"github.com/alexwoo79/duckdb-report/internal/database"
	"github.com/alexwoo79/duckdb-report/internal/executil"
	"github.com/alexwoo79/duckdb-report/internal/model"
	"github.com/alexwoo79/duckdb-report/internal/preview"
	"github.com/alexwoo79/duckdb-report/internal/report"
	"github.com/alexwoo79/duckdb-report/internal/runner"
	"github.com/alexwoo79/duckdb-report/internal/settings"
	appversion "github.com/alexwoo79/duckdb-report/internal/version"
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
	exampleFS         fs.FS
	quarto            string
	seedData          bool
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
	seedData := sameFilePath(root, configRoot)
	var store *workspace.Store
	if seedData {
		store, err = workspace.New(root, exampleFS)
	} else {
		store, err = workspace.Open(root, exampleFS)
	}
	if err != nil {
		app.initErr = err
		return app
	}
	db, err := database.OpenWithBaseDir(context.Background(), store.Database, store.Root)
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
	app.exampleFS = exampleFS
	app.quarto = quarto
	app.seedData = seedData
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
	if a.seedData {
		if err := a.ensureDemoData(ctx); err != nil {
			a.initErr = err
			return
		}
		if err := a.ensureExampleData(ctx); err != nil {
			a.initErr = err
		}
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
	databases, err := a.database.ListDatabases(a.operationContext())
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
	tables = a.filterLegacySampleTables(tables)
	if sqlFiles == nil {
		sqlFiles = []model.FileRef{}
	}
	if templates == nil {
		templates = []model.FileRef{}
	}
	if workflows == nil {
		workflows = []model.FileRef{}
	}
	if databases == nil {
		databases = []model.DatabaseRef{}
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
		Databases:   databases,
		Tables:      tables,
		Sources:     sources,
		Runs:        runs,
		Environment: a.renderer.Environments(a.operationContext()),
		Settings:    a.settingsInfoLocked(),
	}, nil
}

func (a *App) LoadSQLCompletions() (model.SQLCompletionCatalog, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.initErr != nil {
		return model.SQLCompletionCatalog{}, a.initErr
	}
	ctx, cancel := context.WithTimeout(a.operationContext(), 30*time.Second)
	defer cancel()
	catalog, err := a.database.SQLCompletions(ctx)
	if err != nil {
		return model.SQLCompletionCatalog{}, err
	}
	if !a.hideLegacySampleTables() {
		return catalog, nil
	}
	filtered := make([]model.SQLCompletionTable, 0, len(catalog.Tables))
	for _, table := range catalog.Tables {
		if isLegacySampleTable(table.Catalog, table.Schema, table.Name) {
			continue
		}
		filtered = append(filtered, table)
	}
	catalog.Tables = filtered
	return catalog, nil
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
	a.mu.Lock()
	defer a.mu.Unlock()
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
	a.mu.Lock()
	defer a.mu.Unlock()
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
	a.mu.Lock()
	defer a.mu.Unlock()
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
	workspacePath := strings.TrimSpace(value.Workspace)
	if workspacePath == "" {
		return model.SettingsInfo{}, errors.New("workspace path is empty")
	}
	workspaceRoot, err := filepath.Abs(workspacePath)
	if err != nil {
		return model.SettingsInfo{}, fmt.Errorf("resolve workspace path: %w", err)
	}
	value.Workspace = workspaceRoot
	switchWorkspace := !sameFilePath(workspaceRoot, a.store.Root)
	if switchWorkspace && a.workspaceOverride {
		return model.SettingsInfo{}, errors.New("workspace is controlled by DUCKDB_REPORT_HOME")
	}

	var candidate *appWorkspaceRuntime
	nextSeedData := a.seedData
	if switchWorkspace {
		seedData := sameFilePath(workspaceRoot, a.defaultWorkspace)
		ctx, cancel := context.WithTimeout(a.operationContext(), 5*time.Minute)
		prepared, prepareErr := openWorkspaceRuntime(ctx, workspaceRoot, a.exampleFS, a.quarto, seedData)
		cancel()
		if prepareErr != nil {
			return model.SettingsInfo{}, fmt.Errorf("switch workspace: %w", prepareErr)
		}
		value.Workspace = prepared.store.Root
		candidate = &prepared
		nextSeedData = seedData
	}

	if err := a.settings.Save(value); err != nil {
		if candidate != nil {
			if closeErr := candidate.database.Close(); closeErr != nil {
				return model.SettingsInfo{}, errors.Join(
					err,
					fmt.Errorf("close candidate workspace database: %w", closeErr),
				)
			}
		}
		return model.SettingsInfo{}, err
	}
	normalized, err := a.settings.Load()
	if err != nil {
		if candidate != nil {
			if closeErr := candidate.database.Close(); closeErr != nil {
				return model.SettingsInfo{}, errors.Join(
					err,
					fmt.Errorf("close candidate workspace database: %w", closeErr),
				)
			}
		}
		return model.SettingsInfo{}, err
	}

	previousDatabase := a.database
	if candidate != nil {
		a.store = candidate.store
		a.database = candidate.database
		a.renderer = candidate.renderer
		a.runner = candidate.runner
		a.seedData = nextSeedData
	}
	a.config = normalized
	if a.ctx != nil {
		runtime.WindowSetTitle(a.ctx, a.displayName())
	}
	if candidate != nil && previousDatabase != nil {
		if err := previousDatabase.Close(); err != nil {
			slog.Warn("close previous workspace database", "error", err)
		}
	}
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
	a.mu.Lock()
	defer a.mu.Unlock()
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

type workspaceTextFormat struct {
	extension   string
	title       string
	prefix      string
	displayName string
}

func (a *App) SaveSQLAs(content string) (model.FileContent, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.initErr != nil {
		return model.FileContent{}, a.initErr
	}
	return a.saveWorkspaceTextAs(content, workspaceTextFormat{
		extension:   ".sql",
		title:       "新建或另存 SQL 文件",
		prefix:      "query-",
		displayName: "SQL 文件",
	}, a.store.SQL)
}

func (a *App) SaveQMDAs(content string) (model.FileContent, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.initErr != nil {
		return model.FileContent{}, a.initErr
	}
	return a.saveWorkspaceTextAs(content, workspaceTextFormat{
		extension:   ".qmd",
		title:       "新建或另存 QMD 文件",
		prefix:      "report-",
		displayName: "Quarto QMD 文件",
	}, a.store.Templates)
}

func (a *App) saveWorkspaceTextAs(
	content string,
	format workspaceTextFormat,
	defaultDirectory string,
) (model.FileContent, error) {
	kind := strings.ToUpper(strings.TrimPrefix(format.extension, "."))
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:            format.title,
		DefaultDirectory: defaultDirectory,
		DefaultFilename:  format.prefix + time.Now().Format("20060102-150405") + format.extension,
		Filters: []runtime.FileFilter{
			{DisplayName: format.displayName, Pattern: "*" + format.extension},
		},
	})
	if err != nil {
		return model.FileContent{}, fmt.Errorf("choose %s destination: %w", kind, err)
	}
	if path == "" {
		return model.FileContent{}, nil
	}
	if !strings.HasSuffix(strings.ToLower(path), format.extension) {
		path += format.extension
	}
	relative, err := filepath.Rel(a.store.Root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return model.FileContent{}, fmt.Errorf("%s 文件必须保存在当前工作区内", kind)
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

type queryExportFormat struct {
	extension   string
	title       string
	displayName string
	write       func(io.Writer, model.QueryResult) error
}

func (a *App) ExportQueryCSV(result model.QueryResult) (string, error) {
	return a.exportQueryResult(result, queryExportFormat{
		extension:   ".csv",
		title:       "导出查询结果为 CSV",
		displayName: "CSV 文件",
		write:       database.WriteQueryCSV,
	})
}

func (a *App) ExportQueryParquet(result model.QueryResult) (string, error) {
	return a.exportQueryResult(result, queryExportFormat{
		extension:   ".parquet",
		title:       "导出查询结果为 Parquet",
		displayName: "Parquet 文件",
		write:       database.WriteQueryParquet,
	})
}

func (a *App) exportQueryResult(result model.QueryResult, format queryExportFormat) (string, error) {
	if len(result.Columns) == 0 {
		return "", errors.New("query result has no columns to export")
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           format.title,
		DefaultFilename: "query-result-" + time.Now().Format("20060102-150405") + format.extension,
		Filters: []runtime.FileFilter{
			{DisplayName: format.displayName, Pattern: "*" + format.extension},
		},
	})
	if err != nil {
		return "", fmt.Errorf("choose %s output: %w", strings.TrimPrefix(format.extension, "."), err)
	}
	if path == "" {
		return "", nil
	}
	if !strings.HasSuffix(strings.ToLower(path), format.extension) {
		path += format.extension
	}
	file, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("create %s output: %w", strings.TrimPrefix(format.extension, "."), err)
	}
	writeErr := format.write(file, result)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return "", fmt.Errorf(
			"write %s output: %w",
			strings.TrimPrefix(format.extension, "."),
			errors.Join(writeErr, closeErr),
		)
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
	a.mu.Lock()
	defer a.mu.Unlock()
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
	a.mu.Lock()
	defer a.mu.Unlock()
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
	return preview.NewHandler(func() *workspace.Store {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.store
	})
}

func (a *App) operationContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

type appWorkspaceRuntime struct {
	store    *workspace.Store
	database *database.Service
	renderer *report.Renderer
	runner   *runner.Runner
}

func openWorkspaceRuntime(
	ctx context.Context,
	root string,
	exampleFS fs.FS,
	quarto string,
	seedData bool,
) (result appWorkspaceRuntime, err error) {
	var store *workspace.Store
	if seedData {
		store, err = workspace.New(root, exampleFS)
	} else {
		store, err = workspace.Open(root, exampleFS)
	}
	if err != nil {
		return appWorkspaceRuntime{}, err
	}
	db, err := database.OpenWithBaseDir(ctx, store.Database, store.Root)
	if err != nil {
		return appWorkspaceRuntime{}, err
	}
	defer func() {
		if err == nil || db == nil {
			return
		}
		if closeErr := db.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close workspace database after setup failure: %w", closeErr))
		}
	}()

	sources, err := store.LoadSources()
	if err != nil {
		return appWorkspaceRuntime{}, err
	}
	if err = db.Reattach(ctx, sources); err != nil {
		return appWorkspaceRuntime{}, err
	}
	if seedData {
		if err = ensureDemoData(ctx, store, db); err != nil {
			return appWorkspaceRuntime{}, err
		}
		if err = ensureExampleData(ctx, store, db); err != nil {
			return appWorkspaceRuntime{}, err
		}
	}

	renderer := report.NewRenderer(quarto, store, db)
	return appWorkspaceRuntime{
		store:    store,
		database: db,
		renderer: renderer,
		runner:   runner.New(store, db, renderer),
	}, nil
}

func (a *App) ensureDemoData(ctx context.Context) error {
	return ensureDemoData(ctx, a.store, a.database)
}

func ensureDemoData(ctx context.Context, store *workspace.Store, db *database.Service) error {
	tables, err := db.ListTables(ctx)
	if err != nil {
		return err
	}
	samplePath := filepath.Join(store.Data, "sales.csv")
	migrationMarker := filepath.Join(store.Root, ".demo-sales-owners-v1")
	tableExists := false
	for _, table := range tables {
		if table.Catalog == "workspace" && table.Schema == "main" && table.Name == "sales" {
			tableExists = true
			break
		}
	}
	if tableExists {
		if isDemoSalesSource(store, samplePath) {
			if _, err := os.Stat(migrationMarker); err == nil {
				imported, importErr := db.Import(ctx, model.ImportRequest{
					Path: samplePath, Table: "sales",
				})
				if importErr != nil {
					return fmt.Errorf("refresh sanitized starter sales data: %w", importErr)
				}
				if saveErr := store.SaveSource(imported.Source); saveErr != nil {
					return saveErr
				}
				if removeErr := os.Remove(migrationMarker); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
					return fmt.Errorf("finish demo sales owner migration: %w", removeErr)
				}
			}
		}
		return nil
	}
	if _, err := os.Stat(samplePath); err != nil {
		return nil
	}
	imported, err := db.Import(ctx, model.ImportRequest{
		Path: samplePath, Table: "sales",
	})
	if err != nil {
		return fmt.Errorf("import starter sales data: %w", err)
	}
	if err := store.SaveSource(imported.Source); err != nil {
		return err
	}
	if err := os.Remove(migrationMarker); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("finish demo sales owner migration: %w", err)
	}
	return nil
}

func isDemoSalesSource(store *workspace.Store, samplePath string) bool {
	sources, err := store.LoadSources()
	if err != nil {
		return false
	}
	for _, source := range sources {
		if source.Table == "sales" && sameFilePath(source.Path, samplePath) {
			return true
		}
	}
	return false
}

func (a *App) ensureExampleData(ctx context.Context) error {
	return ensureExampleData(ctx, a.store, a.database)
}

func ensureExampleData(ctx context.Context, store *workspace.Store, db *database.Service) error {
	marker := filepath.Join(store.Root, ".monthly-sales-example-v1")
	if _, err := os.Stat(marker); err == nil {
		if _, queryErr := db.Query(ctx, "SELECT customers FROM kpi_summary LIMIT 0", 1); queryErr == nil {
			return nil
		}
	}
	exampleRoot := filepath.Join(store.Root, "examples", "monthly_sales")
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
		imported, err := db.Import(ctx, model.ImportRequest{
			Path: path, Kind: "csv", Table: source.table,
		})
		if err != nil {
			return fmt.Errorf("seed example table %s: %w", source.table, err)
		}
		if err := store.SaveSource(imported.Source); err != nil {
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
		if _, err := db.Query(ctx, query, 500); err != nil {
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
	databases, err := a.database.ListDatabases(a.operationContext())
	if err != nil {
		return model.DataCatalog{}, err
	}
	tables, err := a.database.ListTables(a.operationContext())
	if err != nil {
		return model.DataCatalog{}, err
	}
	if tables == nil {
		tables = []model.TableRef{}
	}
	tables = a.filterLegacySampleTables(tables)
	if databases == nil {
		databases = []model.DatabaseRef{}
	}
	sources, err := a.store.LoadSources()
	if err != nil {
		return model.DataCatalog{}, err
	}
	if sources == nil {
		sources = []model.Source{}
	}
	return model.DataCatalog{
		Databases: databases,
		Tables:    tables,
		Sources:   sources,
	}, nil
}

var legacySampleTableNames = map[string]struct{}{
	"sales":                 {},
	"customers":             {},
	"products":              {},
	"orders":                {},
	"order_items":           {},
	"order_detail":          {},
	"monthly_summary":       {},
	"monthly_kpi_summary":   {},
	"sales_summary":         {},
	"kpi_summary":           {},
	"region_category_pivot": {},
	"validation_results":    {},
	"monthly_trend":         {},
	"product_ranking":       {},
}

func (a *App) filterLegacySampleTables(tables []model.TableRef) []model.TableRef {
	if !a.hideLegacySampleTables() {
		return tables
	}
	filtered := make([]model.TableRef, 0, len(tables))
	for _, table := range tables {
		if isLegacySampleTable(table.Catalog, table.Schema, table.Name) {
			continue
		}
		filtered = append(filtered, table)
	}
	return filtered
}

func (a *App) hideLegacySampleTables() bool {
	if a.seedData || a.store == nil {
		return false
	}
	_, err := os.Stat(filepath.Join(a.store.Root, ".monthly-sales-example-v1"))
	return err == nil
}

func isLegacySampleTable(catalog, schema, name string) bool {
	if catalog != "workspace" || schema != "main" {
		return false
	}
	_, ok := legacySampleTableNames[strings.ToLower(name)]
	return ok
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
		Version:           appversion.Current,
		Workspace:         a.config.Workspace,
		CurrentWorkspace:  currentWorkspace,
		DefaultWorkspace:  a.defaultWorkspace,
		SettingsPath:      settingsPath,
		WorkspaceOverride: a.workspaceOverride,
		UIFontSize:        a.config.UIFontSize,
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
		command = executil.Command("open", path)
	case "windows":
		command = executil.Command("rundll32", "url.dll,FileProtocolHandler", path)
	default:
		command = executil.Command("xdg-open", path)
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
