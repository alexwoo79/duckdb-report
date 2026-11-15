package workflowbundle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexwoo79/duckdb-report/internal/model"
	"github.com/alexwoo79/duckdb-report/internal/workspace"
)

func TestExportImportWorkflowPackage(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source, err := workspace.New(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	sqlPath := filepath.Join(source.SQL, "custom.sql")
	templateDir := filepath.Join(source.Templates, "custom")
	if err := os.MkdirAll(filepath.Join(templateDir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sqlPath, []byte("SELECT 1;"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(templateDir, "report.qmd"), []byte("# Report\n\n{{table}}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(templateDir, "assets", "style.css"), []byte("body{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	value := model.Workflow{
		ID:   "portable",
		Name: "Portable workflow",
		Presets: []model.Preset{
			{ID: "sql", Type: model.PresetSQLSet, Name: "SQL", Params: map[string]string{"file": "sql/custom.sql"}},
			{ID: "report", Type: model.PresetReport, Name: "Report", Params: map[string]string{"template": "templates/custom/report.qmd"}},
		},
		Steps: []model.Step{
			{ID: "sql-step", Name: "SQL", Kind: model.StepSQL, PresetID: "sql", Enabled: true},
			{ID: "report-step", Name: "Report", Kind: model.StepRender, PresetID: "report", Enabled: true},
		},
	}
	packagePath := filepath.Join(t.TempDir(), "workflow.zip")
	file, err := os.Create(packagePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := Export(file, source, value); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	target, err := workspace.New(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := Import(packagePath, target)
	if err != nil {
		t.Fatal(err)
	}
	sqlReference := imported.Presets[0].Params["file"]
	reportReference := imported.Presets[1].Params["template"]
	for _, reference := range []string{sqlReference, reportReference} {
		if !strings.HasPrefix(reference, "workflows/imports/") {
			t.Fatalf("asset path was not rewritten: %q", reference)
		}
		if _, err := target.ResolveExisting(reference); err != nil {
			t.Fatalf("imported asset missing: %v", err)
		}
	}
}
