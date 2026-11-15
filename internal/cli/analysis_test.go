package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/alexwoo79/duckdb-report/internal/model"
)

func TestQueryCommandJSON(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root := newRootCommand(nil)
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--workspace", t.TempDir(),
		"query",
		"--sql", "SELECT 42 AS answer",
		"--json",
	})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v, stderr = %s", err, stderr.String())
	}
	var result model.QueryResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode query result: %v\n%s", err, stdout.String())
	}
	if len(result.Rows) != 1 || len(result.Rows[0]) != 1 {
		t.Fatalf("query rows = %#v", result.Rows)
	}
	if result.Columns[0] != "answer" {
		t.Fatalf("query columns = %#v", result.Columns)
	}
}

func TestStatusCommandJSONUsesArrays(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root := newRootCommand(nil)
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--workspace", t.TempDir(),
		"status",
		"--json",
	})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v, stderr = %s", err, stderr.String())
	}
	var result statusResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode status result: %v\n%s", err, stdout.String())
	}
	if result.Tables == nil || result.Sources == nil {
		t.Fatalf("status arrays must not be null: %#v", result)
	}
}
