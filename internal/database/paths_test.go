package database

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexwoo79/duckdb-report/internal/model"
)

func TestResolveSQLPathsUsesBaseDirectory(t *testing.T) {
	t.Parallel()

	baseDir := filepath.Join(t.TempDir(), "workspace")
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "copy target",
			query: `COPY "导入校验" TO 'out/test.csv' (FORMAT CSV, HEADER);`,
			want: `COPY "导入校验" TO ` +
				quoteSQLString(filepath.Join(baseDir, "out", "test.csv")) +
				` (FORMAT CSV, HEADER);`,
		},
		{
			name:  "copy target with quoted table literal",
			query: `COPY '导入校验' TO 'test.csv';`,
			want: `COPY '导入校验' TO ` +
				quoteSQLString(filepath.Join(baseDir, "test.csv")) +
				`;`,
		},
		{
			name:  "copy source",
			query: `COPY target FROM 'data/source.csv' (HEADER);`,
			want: `COPY target FROM ` +
				quoteSQLString(filepath.Join(baseDir, "data", "source.csv")) +
				` (HEADER);`,
		},
		{
			name:  "reader function",
			query: `SELECT * FROM read_csv_auto('data/source.csv');`,
			want: `SELECT * FROM read_csv_auto(` +
				quoteSQLString(filepath.Join(baseDir, "data", "source.csv")) +
				`);`,
		},
		{
			name:  "reader list",
			query: `SELECT * FROM read_parquet(['data/a.parquet', 'data/b.parquet']);`,
			want: `SELECT * FROM read_parquet([` +
				quoteSQLString(filepath.Join(baseDir, "data", "a.parquet")) +
				", " +
				quoteSQLString(filepath.Join(baseDir, "data", "b.parquet")) +
				`]);`,
		},
		{
			name:  "attach",
			query: `ATTACH IF NOT EXISTS 'data/report.duckdb' AS report;`,
			want: `ATTACH IF NOT EXISTS ` +
				quoteSQLString(filepath.Join(baseDir, "data", "report.duckdb")) +
				` AS report;`,
		},
		{
			name:  "export database",
			query: `EXPORT DATABASE 'exports/snapshot';`,
			want: `EXPORT DATABASE ` +
				quoteSQLString(filepath.Join(baseDir, "exports", "snapshot")) +
				`;`,
		},
		{
			name: "absolute and URI paths",
			query: `SELECT * FROM read_csv_auto('/tmp/input.csv');` +
				` SELECT * FROM read_json_auto('https://example.com/input.json');`,
			want: `SELECT * FROM read_csv_auto('/tmp/input.csv');` +
				` SELECT * FROM read_json_auto('https://example.com/input.json');`,
		},
		{
			name:  "unrelated strings",
			query: `COPY (SELECT 'out/test.csv' AS path) TO 'out/result.csv';`,
			want: `COPY (SELECT 'out/test.csv' AS path) TO ` +
				quoteSQLString(filepath.Join(baseDir, "out", "result.csv")) +
				`;`,
		},
		{
			name:  "copy from database alias",
			query: `COPY FROM DATABASE source TO "backup";`,
			want:  `COPY FROM DATABASE source TO "backup";`,
		},
		{
			name:  "path expression",
			query: `COPY source TO (getvariable('output_dir') || '/result.csv');`,
			want:  `COPY source TO (getvariable('output_dir') || '/result.csv');`,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := resolveSQLPaths(test.query, baseDir)
			if got != test.want {
				t.Fatalf("resolveSQLPaths() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestQueryAndImportResolveFromWorkspace(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	service, err := OpenWithBaseDir(context.Background(), filepath.Join(root, "workspace.duckdb"), root)
	if err != nil {
		t.Fatalf("OpenWithBaseDir() error = %v", err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	if err := os.MkdirAll(filepath.Join(root, "out"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if _, err := service.Query(context.Background(), `
		COPY (SELECT 1 AS value)
		TO 'out/result.csv'
		(FORMAT CSV, HEADER);
	`, 1); err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "out", "result.csv")); err != nil {
		t.Fatalf("relative COPY did not write inside workspace: %v", err)
	}

	importRoot := filepath.Join(root, "data")
	if err := os.MkdirAll(importRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	sourcePath := filepath.Join(importRoot, "source.csv")
	if err := os.WriteFile(sourcePath, []byte("value\n1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	result, err := service.Import(context.Background(), model.ImportRequest{
		Path:  "data/source.csv",
		Kind:  "csv",
		Table: "source_table",
	})
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Source.Path != sourcePath {
		t.Fatalf("Import() source path = %q, want %q", result.Source.Path, sourcePath)
	}
}
