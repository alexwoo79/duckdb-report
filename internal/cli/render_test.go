package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alexwoo79/duckdb-report/internal/workspace"
)

func TestLoadQueryFileResolvesParametersAndReadScripts(t *testing.T) {
	root := t.TempDir()
	store, err := workspace.New(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.SQL, "part.sql"), []byte("SELECT 2 AS value;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	queryPath := filepath.Join(store.SQL, "query.sql")
	content := ".read part.sql\nSELECT {{value}} AS value;\n"
	if err := os.WriteFile(queryPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := loadQueryFile(store, "sql/query.sql", map[string]string{"value": "42"})
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT 2 AS value;\nSELECT 42 AS value;\n"
	if got != want {
		t.Fatalf("loadQueryFile() = %q, want %q", got, want)
	}
}
