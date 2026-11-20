package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/alexwoo79/duckdb-report/internal/workspace"
)

func TestDoctorCommandIgnoresLegacyWorkflow(t *testing.T) {
	root := t.TempDir()
	store, err := workspace.Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(store.Workflows, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.WorkflowFile, []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := executeDoctor(t, root)
	if err != nil {
		t.Fatalf("doctor failed: %v\nresult=%#v", err, result)
	}
	if result.Summary.Status != "ok" {
		t.Fatalf("doctor summary = %#v, issues = %#v", result.Summary, result.Issues)
	}
}

func TestDoctorCommandReportsWorkspaceProblems(t *testing.T) {
	root := t.TempDir()
	store, err := workspace.Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteText("sql/broken.sql", "SELECT FROM\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteText("templates/broken.qmd", `---
title: Broken
format: html
---

![](assets/missing.css)
`); err != nil {
		t.Fatal(err)
	}

	result, err := executeDoctor(t, root)
	if err == nil {
		t.Fatal("expected doctor to return findings")
	}
	assertDoctorCode(t, result, "sql.syntax")
	assertDoctorCode(t, result, "report.asset_missing")
}

func executeDoctor(t *testing.T, root string) (doctorResult, error) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := newRootCommand(nil)
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"--workspace", root, "doctor", "--json"})
	runErr := command.ExecuteContext(context.Background())

	var result doctorResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode doctor result: %v\nstderr=%s\nstdout=%s", err, stderr.String(), stdout.String())
	}
	return result, runErr
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
