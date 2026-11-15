package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexwoo79/duckdb-report/internal/model"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		input   model.Workflow
		wantErr bool
	}{
		{
			name:  "default workflow",
			input: DefaultWorkflow(),
		},
		{
			name:    "missing steps",
			input:   model.Workflow{Name: "empty"},
			wantErr: true,
		},
		{
			name: "missing preset",
			input: model.Workflow{
				Name:  "broken",
				Steps: []model.Step{{ID: "one", Kind: model.StepSQL, PresetID: "missing"}},
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := Validate(test.input)
			if (err != nil) != test.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestParamsForStep(t *testing.T) {
	w := model.Workflow{
		Presets: []model.Preset{{
			ID: "sql",
			Params: map[string]string{
				"title": "preset",
				"limit": "100",
			},
		}},
	}
	step := model.Step{
		PresetID: "sql",
		Overrides: map[string]string{
			"title": "step",
		},
	}
	params, err := ParamsForStep(w, step, map[string]string{"title": "runtime"})
	if err != nil {
		t.Fatal(err)
	}
	if params["title"] != "runtime" {
		t.Fatalf("title = %q, want runtime", params["title"])
	}
	if params["limit"] != "100" {
		t.Fatalf("limit = %q, want 100", params["limit"])
	}
}

func TestResolveTemplate(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		params  map[string]string
		want    string
		wantErr bool
	}{
		{
			name:   "replace variables",
			input:  "SELECT * FROM {{ table }} WHERE d >= '{{start}}'",
			params: map[string]string{"table": "sales", "start": "2026-01-01"},
			want:   "SELECT * FROM sales WHERE d >= '2026-01-01'",
		},
		{
			name:    "missing variable",
			input:   "SELECT {{missing}}",
			params:  map[string]string{},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := ResolveTemplate(test.input, test.params)
			if test.wantErr {
				if !errors.Is(err, ErrVariableMissing) {
					t.Fatalf("ResolveTemplate() error = %v, want ErrVariableMissing", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("ResolveTemplate() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestResolveWorkspacePath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "query.sql"), []byte("SELECT 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveWorkspacePath(root, "query.sql"); err != nil {
		t.Fatalf("expected workspace file to resolve: %v", err)
	}
	if _, err := ResolveWorkspacePath(root, "../outside.sql"); err == nil {
		t.Fatal("expected path outside workspace to fail")
	}
}
