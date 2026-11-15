package exportbundle

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestExportReport(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	reportDir := filepath.Join(root, "run")
	if err := os.MkdirAll(filepath.Join(reportDir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"report.html":       "<html></html>",
		"report.qmd":        "# Report",
		"analysis.duckdb":   "database",
		"assets/report.css": "body{}",
	} {
		path := filepath.Join(reportDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	destination := filepath.Join(root, "package.zip")
	if err := ExportReport(reportDir, destination, map[string]string{
		"include_report":   "true",
		"include_database": "true",
		"include_source":   "true",
	}); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	found := make(map[string]bool)
	for _, file := range archive.File {
		found[file.Name] = true
	}
	for _, expected := range []string{"report.html", "report.qmd", "analysis.duckdb", "assets/report.css"} {
		if !found[expected] {
			t.Fatalf("missing package entry %q in %#v", expected, found)
		}
	}
}
