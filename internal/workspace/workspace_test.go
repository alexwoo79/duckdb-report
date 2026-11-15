package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexwoo79/duckdb-report/internal/workflow"
)

func TestNewSeedsWorkspace(t *testing.T) {
	t.Parallel()
	store, err := New(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.Data, "sales.csv")); err != nil {
		t.Fatalf("starter data was not initialized: %v", err)
	}
	current, err := store.LoadWorkflow()
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != workflow.DefaultWorkflow().ID {
		t.Fatalf("workflow id = %q", current.ID)
	}
	sqlFiles, err := store.ListSQL()
	if err != nil {
		t.Fatal(err)
	}
	if len(sqlFiles) < 2 {
		t.Fatalf("seeded SQL files = %#v", sqlFiles)
	}
}

func TestWriteTextRejectsTraversal(t *testing.T) {
	t.Parallel()
	store, err := New(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteText("../outside.sql", "SELECT 1"); err == nil {
		t.Fatal("expected path traversal to fail")
	}
}

func TestClearSourcesReturnsEmptySlice(t *testing.T) {
	t.Parallel()
	store, err := New(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ClearSources(); err != nil {
		t.Fatal(err)
	}
	sources, err := store.LoadSources()
	if err != nil {
		t.Fatal(err)
	}
	if sources == nil || len(sources) != 0 {
		t.Fatalf("LoadSources() = %#v, want non-nil empty slice", sources)
	}
}

func TestNewUpgradesLegacyPrintableTemplate(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	templateDir := filepath.Join(root, "templates")
	if err := os.MkdirAll(templateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := "---\nsubtitle: \"DuckDB 本地分析工作流\"\n---\n\n本报告由 **DuckDB + SQL + Quarto** 自动生成。\n"
	if err := os.WriteFile(filepath.Join(templateDir, "monthly_report.qmd"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := New(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	upgraded, err := os.ReadFile(filepath.Join(store.Templates, "monthly_report.qmd"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(upgraded), `mainfont: "{{report_font}}"`) {
		t.Fatalf("legacy template was not upgraded: %s", upgraded)
	}
}

func TestNewUpgradesFixedMacFontInExistingTemplate(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	templateDir := filepath.Join(root, "templates")
	if err := os.MkdirAll(templateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := "---\nformat:\n  typst:\n    mainfont: \"PingFang SC\"\n---\n"
	if err := os.WriteFile(filepath.Join(templateDir, "monthly_report.qmd"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := New(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	upgraded, err := os.ReadFile(filepath.Join(store.Templates, "monthly_report.qmd"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(upgraded), `mainfont: "{{report_font}}"`) {
		t.Fatalf("fixed report font was not upgraded: %s", upgraded)
	}
}
