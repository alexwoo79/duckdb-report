package database

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexwoo79/duckdb-report/internal/model"
)

func TestWriteQueryParquet(t *testing.T) {
	t.Parallel()
	result := model.QueryResult{
		Columns: []string{"name", "amount", "active"},
		Rows: [][]any{
			{"alpha", 1, true},
			{"含,逗号", 2.5, nil},
			{nil, 3, false},
		},
	}

	root := t.TempDir()
	parquetPath := filepath.Join(root, "query-result.parquet")
	file, err := os.Create(parquetPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteQueryParquet(file, result); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	service, err := Open(context.Background(), filepath.Join(root, "reader.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := service.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})

	schema, err := service.Query(context.Background(), fmt.Sprintf(
		"DESCRIBE SELECT * FROM read_parquet(%s)",
		quoteLiteral(parquetPath),
	), 10)
	if err != nil {
		t.Fatal(err)
	}
	gotTypes := make(map[string]string)
	for _, row := range schema.Rows {
		gotTypes[fmt.Sprint(row[0])] = fmt.Sprint(row[1])
	}
	if gotTypes["name"] != "VARCHAR" || gotTypes["amount"] != "DOUBLE" || gotTypes["active"] != "BOOLEAN" {
		t.Fatalf("column types = %#v", gotTypes)
	}

	rows, err := service.Query(context.Background(), fmt.Sprintf(
		"SELECT name, amount, active FROM read_parquet(%s) ORDER BY amount",
		quoteLiteral(parquetPath),
	), 10)
	if err != nil {
		t.Fatal(err)
	}
	if rows.RowCount != 3 {
		t.Fatalf("row count = %d, want 3", rows.RowCount)
	}
	if got := fmt.Sprint(rows.Rows[0][0]); got != "alpha" {
		t.Fatalf("first name = %q", got)
	}
	if rows.Rows[1][2] != nil {
		t.Fatalf("second active value = %#v, want nil", rows.Rows[1][2])
	}
	if rows.Rows[2][0] != nil {
		t.Fatalf("third name = %#v, want nil", rows.Rows[2][0])
	}
}

func TestWriteQueryParquetEmptyResult(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	parquetPath := filepath.Join(root, "empty-result.parquet")
	file, err := os.Create(parquetPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteQueryParquet(file, model.QueryResult{
		Columns: []string{"name", "value"},
	}); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	service, err := Open(context.Background(), filepath.Join(root, "reader.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := service.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	rows, err := service.Query(context.Background(), fmt.Sprintf(
		"SELECT count(*) AS rows FROM read_parquet(%s)",
		quoteLiteral(parquetPath),
	), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Rows) != 1 || fmt.Sprint(rows.Rows[0][0]) != "0" {
		t.Fatalf("empty parquet rows = %#v", rows.Rows)
	}
}

func TestWriteQueryParquetRejectsMissingColumns(t *testing.T) {
	t.Parallel()
	err := WriteQueryParquet(&bytes.Buffer{}, model.QueryResult{})
	if err == nil || !strings.Contains(err.Error(), "no columns") {
		t.Fatalf("WriteQueryParquet() error = %v", err)
	}
}
