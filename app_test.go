package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexwoo79/duckdb-report/internal/database"
	"github.com/alexwoo79/duckdb-report/internal/model"
	"github.com/alexwoo79/duckdb-report/internal/report"
	"github.com/alexwoo79/duckdb-report/internal/runner"
	"github.com/alexwoo79/duckdb-report/internal/settings"
	"github.com/alexwoo79/duckdb-report/internal/workflow"
	"github.com/alexwoo79/duckdb-report/internal/workspace"
)

func TestOutputPath(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(runDir, "report.html"), []byte("report"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := outputPath(runDir, "HTML")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "report.html" {
		t.Fatalf("outputPath() = %q", path)
	}
	if _, err := outputPath(runDir, "pdf"); err == nil {
		t.Fatal("expected missing PDF output to fail")
	}
}

func TestCopyFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "source.pdf")
	destination := filepath.Join(root, "destination.pdf")
	if err := os.WriteFile(source, []byte("pdf-data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(source, destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "pdf-data" {
		t.Fatalf("copied data = %q", data)
	}
}

func TestReplaceFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	destination := filepath.Join(root, "workspace.duckdb")
	temporary := filepath.Join(root, ".workspace.tmp")
	if err := os.WriteFile(destination, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(temporary, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := replaceFile(temporary, destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("replaced data = %q", data)
	}
	if _, err := os.Stat(temporary); !os.IsNotExist(err) {
		t.Fatalf("temporary file still exists: %v", err)
	}
}

func TestSameFilePath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "workspace.duckdb")
	if !sameFilePath(path, filepath.Join(root, ".", "workspace.duckdb")) {
		t.Fatal("expected equivalent paths to match")
	}
}

func newWorkspaceSwitchTestApp(t *testing.T, root string) *App {
	t.Helper()
	configRoot := t.TempDir()
	settingsStore := settings.New(configRoot, configRoot)
	if err := settingsStore.Save(model.AppSettings{
		DisplayName: "Switch Test",
		Workspace:   root,
		UIFontSize:  13,
	}); err != nil {
		t.Fatal(err)
	}
	configured, err := settingsStore.Load()
	if err != nil {
		t.Fatal(err)
	}
	store, err := workspace.New(root, os.DirFS("."))
	if err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(t.Context(), store.Database)
	if err != nil {
		t.Fatal(err)
	}
	renderer := report.NewRenderer("", store, db)
	app := &App{
		store:            store,
		database:         db,
		renderer:         renderer,
		runner:           runner.New(store, db, renderer),
		settings:         settingsStore,
		config:           configured,
		defaultWorkspace: configRoot,
		exampleFS:        os.DirFS("."),
	}
	t.Cleanup(func() {
		if closeErr := app.database.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	return app
}

func TestSaveSettingsSwitchesWorkspace(t *testing.T) {
	t.Parallel()
	oldRoot := t.TempDir()
	newRoot := filepath.Join(t.TempDir(), "next")
	app := newWorkspaceSwitchTestApp(t, oldRoot)
	previousDB := app.database

	info, err := app.SaveSettings(model.AppSettings{
		DisplayName: "Switch Test",
		Workspace:   newRoot,
		UIFontSize:  13,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sameFilePath(app.store.Root, newRoot) {
		t.Fatalf("active workspace = %q, want %q", app.store.Root, newRoot)
	}
	if !sameFilePath(app.database.Path(), filepath.Join(newRoot, "workspace.duckdb")) {
		t.Fatalf("active database = %q", app.database.Path())
	}
	if !sameFilePath(info.CurrentWorkspace, newRoot) {
		t.Fatalf("settings current workspace = %q, want %q", info.CurrentWorkspace, newRoot)
	}
	if _, err := app.database.Query(t.Context(), "SELECT 1", 1); err != nil {
		t.Fatalf("query switched workspace: %v", err)
	}
	if _, err := previousDB.Query(t.Context(), "SELECT 1", 1); err == nil {
		t.Fatal("previous workspace database remained open")
	}
}

func TestSaveSettingsKeepsWorkspaceWhenSwitchFails(t *testing.T) {
	t.Parallel()
	oldRoot := t.TempDir()
	app := newWorkspaceSwitchTestApp(t, oldRoot)
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("blocked"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := app.SaveSettings(model.AppSettings{
		DisplayName: "Switch Test",
		Workspace:   filepath.Join(blocker, "workspace"),
		UIFontSize:  13,
	})
	if err == nil {
		t.Fatal("expected workspace switch to fail")
	}
	if !sameFilePath(app.store.Root, oldRoot) {
		t.Fatalf("active workspace changed after failure: %q", app.store.Root)
	}
	if _, err := app.database.Query(t.Context(), "SELECT 1", 1); err != nil {
		t.Fatalf("query previous workspace after failure: %v", err)
	}
	configured, err := app.settings.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !sameFilePath(configured.Workspace, oldRoot) {
		t.Fatalf("saved workspace = %q, want %q", configured.Workspace, oldRoot)
	}
}

func TestSaveSettingsDoesNotSeedExistingWorkspace(t *testing.T) {
	t.Parallel()
	oldRoot := t.TempDir()
	targetRoot := filepath.Join(t.TempDir(), "contract-workspace")
	target, err := workspace.Open(targetRoot, os.DirFS("."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := target.WriteText("sql/contract.sql", "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := target.WriteText("templates/contract.qmd", "# Contract"); err != nil {
		t.Fatal(err)
	}
	app := newWorkspaceSwitchTestApp(t, oldRoot)

	if _, err := app.SaveSettings(model.AppSettings{
		DisplayName: "Switch Test",
		Workspace:   targetRoot,
		UIFontSize:  13,
	}); err != nil {
		t.Fatal(err)
	}
	result, err := app.Bootstrap()
	if err != nil {
		t.Fatal(err)
	}
	if !sameFilePath(result.Workspace, targetRoot) {
		t.Fatalf("workspace = %q, want %q", result.Workspace, targetRoot)
	}
	if len(result.SQLFiles) != 1 || result.SQLFiles[0].Path != "sql/contract.sql" {
		t.Fatalf("SQL files = %#v", result.SQLFiles)
	}
	if len(result.Templates) != 1 || result.Templates[0].Path != "templates/contract.qmd" {
		t.Fatalf("templates = %#v", result.Templates)
	}
	if len(result.Tables) != 0 {
		t.Fatalf("starter tables were imported into existing workspace: %#v", result.Tables)
	}
}

func TestFilterLegacySampleTables(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".monthly-sales-example-v1"), []byte("completed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &App{
		store: &workspace.Store{Root: root},
	}
	tables := []model.TableRef{
		{Catalog: "workspace", Schema: "main", Name: "customers"},
		{Catalog: "workspace", Schema: "main", Name: "contract_source"},
		{Catalog: "external", Schema: "main", Name: "sales"},
	}
	filtered := app.filterLegacySampleTables(tables)
	if len(filtered) != 2 {
		t.Fatalf("filtered tables = %#v", filtered)
	}
	if filtered[0].Name != "contract_source" || filtered[1].Catalog != "external" {
		t.Fatalf("filtered tables = %#v", filtered)
	}
}

func TestExampleWorkflowsAreValid(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"monthly_sales_python.json", "monthly_sales_r.json"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join("example", "workflows", name))
			if err != nil {
				t.Fatal(err)
			}
			var value model.Workflow
			if err := json.Unmarshal(data, &value); err != nil {
				t.Fatal(err)
			}
			if err := workflow.Validate(value); err != nil {
				t.Fatal(err)
			}
			if len(value.Steps) != 10 {
				t.Fatalf("steps = %d, want 10", len(value.Steps))
			}
			if fmt.Sprint(value.Presets[0].Params["path"]) == "" {
				t.Fatal("source preset path is empty")
			}
		})
	}
}

func TestEnsureExampleDataCreatesAnalysisTables(t *testing.T) {
	t.Parallel()
	store, err := workspace.New(t.TempDir(), os.DirFS("."))
	if err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(context.Background(), store.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	app := &App{store: store, database: db}
	if err := app.ensureExampleData(context.Background()); err != nil {
		t.Fatal(err)
	}
	tables, err := db.ListTables(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := make(map[string]bool)
	for _, table := range tables {
		found[table.Name] = true
	}
	for _, expected := range []string{"customers", "orders", "order_detail", "sales_summary", "region_category_pivot"} {
		if !found[expected] {
			t.Fatalf("missing example table %q in %#v", expected, found)
		}
	}
	if _, err := db.Query(context.Background(), "CREATE OR REPLACE TABLE kpi_summary AS SELECT 1 AS orders", 1); err != nil {
		t.Fatal(err)
	}
	if err := app.ensureExampleData(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Query(context.Background(), "SELECT customers FROM kpi_summary LIMIT 1", 1); err != nil {
		t.Fatalf("stale example marker was not repaired: %v", err)
	}
}

func TestEnsureDemoDataSanitizesLegacyOwnerNames(t *testing.T) {
	t.Parallel()
	store, err := workspace.New(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(context.Background(), store.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	if _, err := db.Query(context.Background(), `
		CREATE TABLE sales(owner VARCHAR);
		INSERT INTO sales VALUES ('Legacy Owner');
	`, 1); err != nil {
		t.Fatal(err)
	}
	samplePath := filepath.Join(store.Data, "sales.csv")
	if err := store.SaveSource(model.Source{Kind: "csv", Path: samplePath, Table: "sales"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Root, ".demo-sales-owners-v1"), []byte("reimport-pending\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &App{store: store, database: db}
	if err := app.ensureDemoData(context.Background()); err != nil {
		t.Fatal(err)
	}
	result, err := db.Query(context.Background(), "SELECT owner FROM sales", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) == 0 || fmt.Sprint(result.Rows[0][0]) != "Demo Rep A" {
		t.Fatalf("sanitized owner = %#v", result.Rows)
	}
}
