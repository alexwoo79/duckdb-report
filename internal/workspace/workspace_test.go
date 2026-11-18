package workspace

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

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

func TestOpenDoesNotSeedStarterAssets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		prepare func(t *testing.T, root string)
	}{
		{
			name: "empty workspace",
			prepare: func(t *testing.T, _ string) {
				t.Helper()
			},
		},
		{
			name: "existing workspace",
			prepare: func(t *testing.T, root string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(root, "sql"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "sql", "contract.sql"), []byte("SELECT 1"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			test.prepare(t, root)
			store, err := Open(root, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.LoadWorkflow(); err != nil {
				t.Fatalf("load default workflow: %v", err)
			}
			if _, err := os.Stat(filepath.Join(store.Data, "sales.csv")); !os.IsNotExist(err) {
				t.Fatalf("starter data was seeded: %v", err)
			}
			for _, path := range []string{"monthly_analysis.sql", "monthly_result.sql"} {
				if _, err := os.Stat(filepath.Join(store.SQL, path)); !os.IsNotExist(err) {
					t.Fatalf("starter SQL %q was seeded: %v", path, err)
				}
			}
			if _, err := os.Stat(filepath.Join(store.Templates, "monthly_report.qmd")); !os.IsNotExist(err) {
				t.Fatalf("starter template was seeded: %v", err)
			}
			if _, err := os.Stat(filepath.Join(store.Root, "examples", "monthly_sales")); !os.IsNotExist(err) {
				t.Fatalf("starter example was seeded: %v", err)
			}
		})
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

func TestMigrateLegacyDemoSalesFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "sales.csv")
	legacy := []byte("legacy synthetic sales sample\n")
	if err := os.WriteFile(path, legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	seed := fstest.MapFS{
		"data/sales.csv": &fstest.MapFile{Data: []byte("owner\nDemo Rep A\n")},
	}
	marker := filepath.Join(root, ".migration")
	legacyHash := fmt.Sprintf("%x", sha256.Sum256(legacy))
	if err := migrateLegacyDemoSalesFile(path, seed, legacyHash, marker); err != nil {
		t.Fatal(err)
	}
	upgraded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(upgraded), "Demo Rep A") {
		t.Fatalf("legacy demo data was not replaced: %s", upgraded)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("migration marker was not created: %v", err)
	}
}
