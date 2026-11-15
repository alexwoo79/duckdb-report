package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexwoo79/duckdb-report/internal/model"
)

func TestWorkflowValidateCommandJSON(t *testing.T) {
	workspaceRoot := t.TempDir()
	path := filepath.Join(workspaceRoot, "workflow.json")
	writeWorkflowFixture(t, path, sqlOnlyWorkflow())

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root := newRootCommand(nil)
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--workspace", workspaceRoot,
		"workflow", "validate",
		"--file", path,
		"--json",
	})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v, stderr = %s", err, stderr.String())
	}
	var result struct {
		Valid bool `json:"valid"`
		Steps int  `json:"steps"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode validation result: %v\n%s", err, stdout.String())
	}
	if !result.Valid || result.Steps != 1 {
		t.Fatalf("validation result = %#v", result)
	}
}

func TestWorkflowRunCommandSQLOnly(t *testing.T) {
	workspaceRoot := t.TempDir()
	path := filepath.Join(workspaceRoot, "workflow.json")
	value := sqlOnlyWorkflow()
	value.Runtime = map[string]string{"value": "42"}
	value.Presets[0].Params["inline_sql"] = "SELECT {{value}} AS answer"
	writeWorkflowFixture(t, path, value)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root := newRootCommand(nil)
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--workspace", workspaceRoot,
		"--quiet",
		"workflow", "run",
		"--file", path,
		"--json",
	})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v, stderr = %s", err, stderr.String())
	}
	var result model.WorkflowResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode workflow result: %v\n%s", err, stdout.String())
	}
	if result.RunID == "" || len(result.Query.Rows) != 1 {
		t.Fatalf("workflow result = %#v", result)
	}
	if result.Query.Columns[0] != "answer" {
		t.Fatalf("workflow query columns = %#v", result.Query.Columns)
	}
}

func sqlOnlyWorkflow() model.Workflow {
	return model.Workflow{
		ID:   "sql-only",
		Name: "SQL only",
		Presets: []model.Preset{{
			ID:   "sql",
			Type: model.PresetSQLSet,
			Name: "SQL",
			Params: map[string]string{
				"mode":       "inline",
				"inline_sql": "SELECT 1 AS answer",
				"limit":      "10",
			},
		}},
		Steps: []model.Step{{
			ID:       "run",
			Name:     "Run SQL",
			Kind:     model.StepSQL,
			PresetID: "sql",
			Enabled:  true,
		}},
	}
}

func writeWorkflowFixture(t *testing.T, path string, value model.Workflow) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
