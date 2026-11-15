package cli

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
)

func TestResolveWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DUCKDB_REPORT_HOME", filepath.Join(home, "environment-workspace"))
	explicit := filepath.Join(home, "explicit-workspace")

	tests := []struct {
		name     string
		explicit string
		want     string
	}{
		{name: "explicit path", explicit: explicit, want: explicit},
		{name: "environment path", want: filepath.Join(home, "environment-workspace")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveWorkspace(test.explicit)
			if err != nil {
				t.Fatal(err)
			}
			want, err := filepath.Abs(test.want)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("resolveWorkspace() = %q, want %q", got, want)
			}
		})
	}
}

func TestParseKeyValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   []string
		want    map[string]string
		wantErr bool
	}{
		{
			name:  "preserves equals in value",
			input: []string{"title=Monthly", "query=SELECT 1 WHERE 1 = 1"},
			want: map[string]string{
				"title": "Monthly",
				"query": "SELECT 1 WHERE 1 = 1",
			},
		},
		{
			name:    "missing separator",
			input:   []string{"title"},
			wantErr: true,
		},
		{
			name:    "empty key",
			input:   []string{"=value"},
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseKeyValues(test.input)
			if (err != nil) != test.wantErr {
				t.Fatalf("parseKeyValues() error = %v, wantErr %v", err, test.wantErr)
			}
			if test.wantErr {
				var typed *usageError
				if !errors.As(err, &typed) {
					t.Fatalf("parseKeyValues() error type = %T, want usageError", err)
				}
				return
			}
			if len(got) != len(test.want) {
				t.Fatalf("parseKeyValues() = %#v, want %#v", got, test.want)
			}
			for key, want := range test.want {
				if got[key] != want {
					t.Fatalf("parseKeyValues()[%q] = %q, want %q", key, got[key], want)
				}
			}
		})
	}
}

func TestExecuteReturnsUsageExitCode(t *testing.T) {
	t.Parallel()
	exitCode := execute(
		context.Background(),
		[]string{"query", "--sql"},
		nil,
		io.Discard,
		io.Discard,
	)
	if exitCode != 2 {
		t.Fatalf("execute() exit code = %d, want 2", exitCode)
	}
}

func TestExecuteReturnsRuntimeExitCode(t *testing.T) {
	t.Parallel()
	exitCode := execute(
		context.Background(),
		[]string{"--workspace", t.TempDir(), "query", "--sql", "SELECT missing_column"},
		nil,
		io.Discard,
		io.Discard,
	)
	if exitCode != 1 {
		t.Fatalf("execute() exit code = %d, want 1", exitCode)
	}
}
