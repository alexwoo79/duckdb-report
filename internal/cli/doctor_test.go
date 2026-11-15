package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexwoo79/duckdb-report/internal/model"
	"github.com/alexwoo79/duckdb-report/internal/workspace"
)

func TestDoctorCommandSQLOnlyWorkflow(t *testing.T) {
	root := t.TempDir()
	store, err := workspace.New(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	writeWorkflowFixture(t, store.WorkflowFile, sqlOnlyWorkflow())

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := newRootCommand(nil)
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"--workspace", root, "doctor", "--json"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("doctor failed: %v\nstderr=%s\nstdout=%s", err, stderr.String(), stdout.String())
	}
	var result doctorResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode doctor result: %v\n%s", err, stdout.String())
	}
	if result.Summary.Errors != 0 || result.Summary.Status != "ok" {
		t.Fatalf("doctor summary = %#v, issues = %#v", result.Summary, result.Issues)
	}
}

func TestDoctorCommandReportsSQLAndExportProblems(t *testing.T) {
	root := t.TempDir()
	store, err := workspace.New(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	value := sqlOnlyWorkflow()
	value.Presets[0].Params["inline_sql"] = "SELECT FROM"
	value.Presets = append(value.Presets, model.Preset{
		ID:   "output",
		Type: model.PresetOutput,
		Name: "Output",
		Params: map[string]string{
			"directory":        "exports",
			"include_report":   "true",
			"include_database": "true",
			"include_source":   "true",
		},
	})
	value.Steps = append(value.Steps, model.Step{
		ID: "export", Name: "Export", Kind: model.StepExport, PresetID: "output", Enabled: true,
	})
	writeWorkflowFixture(t, store.WorkflowFile, value)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := newRootCommand(nil)
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"--workspace", root, "doctor", "--json"})
	err = command.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected doctor to return findings")
	}
	var result doctorResult
	if decodeErr := json.Unmarshal(stdout.Bytes(), &result); decodeErr != nil {
		t.Fatalf("decode doctor result: %v\n%s", decodeErr, stdout.String())
	}
	assertDoctorCode(t, result, "sql.syntax")
	assertDoctorCode(t, result, "workflow.export_before_render")
	if result.Summary.Errors < 2 {
		t.Fatalf("doctor summary = %#v", result.Summary)
	}
}

func TestDoctorCommandReportsQMDTableMapping(t *testing.T) {
	root := t.TempDir()
	store, err := workspace.New(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	templatePath := filepath.Join(store.Templates, "mapping.qmd")
	if err := os.WriteFile(templatePath, []byte(`---
title: Mapping
format: html
---

{{table_products}}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	value := model.Workflow{
		ID:   "mapping",
		Name: "Mapping",
		Presets: []model.Preset{{
			ID:   "report",
			Type: model.PresetReport,
			Name: "Report",
			Params: map[string]string{
				"template": "templates/mapping.qmd",
				"format":   "html",
			},
		}},
		Steps: []model.Step{{
			ID: "render", Name: "Render", Kind: model.StepRender, PresetID: "report", Enabled: true,
		}},
	}
	writeWorkflowFixture(t, store.WorkflowFile, value)

	var stdout bytes.Buffer
	command := newRootCommand(nil)
	command.SetOut(&stdout)
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"--workspace", root, "doctor", "--json"})
	_ = command.ExecuteContext(context.Background())

	var result doctorResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode doctor result: %v\n%s", err, stdout.String())
	}
	assertDoctorCode(t, result, "report.named_query_missing")
}

func assertDoctorCode(t *testing.T, result doctorResult, code string) {
	t.Helper()
	for _, issue := range result.Issues {
		if issue.Code == code {
			return
		}
	}
	t.Fatalf("doctor issues do not contain %q: %#v", code, result.Issues)
}
