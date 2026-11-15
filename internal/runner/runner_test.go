package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexwoo79/duckdb-report/internal/database"
	"github.com/alexwoo79/duckdb-report/internal/workspace"
)

func TestRunSQLResolvesFileVariables(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, err := workspace.New(root, nil)
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
		CREATE TABLE sales(order_date DATE);
		INSERT INTO sales VALUES (DATE '2026-01-02'), (DATE '2026-02-03');
	`, 10); err != nil {
		t.Fatal(err)
	}
	sqlPath := filepath.Join(store.SQL, "window.sql")
	if err := os.WriteFile(sqlPath, []byte(`
		SELECT count(*) AS rows
		FROM sales
		WHERE order_date BETWEEN DATE '{{start_date}}' AND DATE '{{end_date}}';
	`), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := New(store, db, nil)
	result, err := runner.runSQL(context.Background(), map[string]string{
		"file":       "sql/window.sql",
		"start_date": "2026-01-01",
		"end_date":   "2026-01-31",
		"limit":      "10",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("rows = %#v", result.Rows)
	}
}

func TestRunSQLInlineMode(t *testing.T) {
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
	runner := New(store, db, nil)
	result, err := runner.runSQL(context.Background(), map[string]string{
		"mode":       "inline",
		"inline_sql": "SELECT {{value}} AS value",
		"value":      "42",
		"limit":      "10",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || fmt.Sprint(result.Rows[0][0]) != "42" {
		t.Fatalf("inline rows = %#v", result.Rows)
	}
}
