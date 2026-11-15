package database

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"

	"github.com/alexwoo79/duckdb-report/internal/model"
)

func TestWriteQueryCSV(t *testing.T) {
	t.Parallel()
	result := model.QueryResult{
		Columns: []string{"name", "value"},
		Rows: [][]any{
			{"alpha", 1},
			{"含,逗号", nil},
		},
	}
	var output bytes.Buffer
	if err := WriteQueryCSV(&output, result); err != nil {
		t.Fatal(err)
	}
	data := strings.TrimPrefix(output.String(), "\xEF\xBB\xBF")
	records, err := csv.NewReader(strings.NewReader(data)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("records = %#v", records)
	}
	if records[2][0] != "含,逗号" || records[2][1] != "" {
		t.Fatalf("encoded row = %#v", records[2])
	}
}
