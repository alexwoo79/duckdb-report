package database

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexwoo79/duckdb-report/internal/model"
	"github.com/xuri/excelize/v2"
)

func TestServiceQuery(t *testing.T) {
	t.Parallel()
	service, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := service.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})

	result, err := service.Query(context.Background(), `
		CREATE TABLE sample(id INTEGER, label VARCHAR);
		INSERT INTO sample VALUES (1, 'alpha'), (2, 'beta');
		SELECT id, label FROM sample ORDER BY id;
	`, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Columns) != 2 || result.Columns[0] != "id" || result.Columns[1] != "label" {
		t.Fatalf("columns = %#v", result.Columns)
	}
	if result.RowCount != 2 {
		t.Fatalf("row count = %d, want 2", result.RowCount)
	}
}

func TestServiceImportExcel(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	workbook := excelize.NewFile()
	sheet := workbook.GetSheetName(0)
	for cell, value := range map[string]any{
		"A1": "region", "B1": "revenue",
		"A2": "华东", "B2": 1200,
		"A3": "华南", "B3": 900,
	} {
		if err := workbook.SetCellValue(sheet, cell, value); err != nil {
			t.Fatal(err)
		}
	}
	excelPath := filepath.Join(root, "sales.xlsx")
	if err := workbook.SaveAs(excelPath); err != nil {
		t.Fatal(err)
	}
	if err := workbook.Close(); err != nil {
		t.Fatal(err)
	}

	service, err := Open(context.Background(), filepath.Join(root, "workspace.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := service.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	imported, err := service.Import(context.Background(), model.ImportRequest{
		Path: excelPath, Table: "excel_sales", Sheet: sheet,
	})
	if err != nil {
		t.Fatal(err)
	}
	if imported.Source.Table != "excel_sales" {
		t.Fatalf("source table = %q", imported.Source.Table)
	}
	result, err := service.Query(context.Background(), `SELECT count(*) AS rows, sum(revenue) AS revenue FROM excel_sales`, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("rows = %#v", result.Rows)
	}
}

func TestQuoteHelpers(t *testing.T) {
	t.Parallel()
	if got := quoteIdentifier(`we"ird`); got != `"we""ird"` {
		t.Fatalf("quoteIdentifier() = %q", got)
	}
	if got := quoteLiteral(`O'Reilly`); got != `'O''Reilly'` {
		t.Fatalf("quoteLiteral() = %q", got)
	}
}

func TestOpenRejectsEmptyPath(t *testing.T) {
	t.Parallel()
	if _, err := Open(context.Background(), ""); err == nil {
		t.Fatal("expected empty path to fail")
	}
}

func TestSortedSheets(t *testing.T) {
	t.Parallel()
	workbook := excelize.NewFile()
	if _, err := workbook.NewSheet("Zulu"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "sheets.xlsx")
	if err := workbook.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	if err := workbook.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	sheets, err := SortedSheets(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(sheets) != 2 || sheets[0] != "Sheet1" || sheets[1] != "Zulu" {
		t.Fatalf("sheets = %#v", sheets)
	}
}

func TestExpandScript(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "child.sql"), []byte("SELECT 2 AS value;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "SELECT 1 AS value;\n.read child.sql\n"
	expanded, err := ExpandScript(script, root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(expanded, "SELECT 1") || !strings.Contains(expanded, "SELECT 2") {
		t.Fatalf("ExpandScript() = %q", expanded)
	}
}

func TestServiceSnapshot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.duckdb")
	snapshotPath := filepath.Join(root, "snapshot.duckdb")
	source, err := Open(context.Background(), sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := source.Close(); closeErr != nil {
			t.Errorf("source Close() error = %v", closeErr)
		}
	})
	if _, err := source.Query(context.Background(), "CREATE TABLE snapshot_data(value INTEGER); INSERT INTO snapshot_data VALUES (42)", 10); err != nil {
		t.Fatal(err)
	}
	if err := source.Snapshot(context.Background(), snapshotPath); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Open(context.Background(), snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := snapshot.Close(); closeErr != nil {
			t.Errorf("snapshot Close() error = %v", closeErr)
		}
	})
	result, err := snapshot.Query(context.Background(), "SELECT value FROM snapshot_data", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || fmt.Sprint(result.Rows[0][0]) != "42" {
		t.Fatalf("snapshot rows = %#v", result.Rows)
	}
}

func TestServiceDeleteSource(t *testing.T) {
	t.Parallel()
	service, err := Open(context.Background(), filepath.Join(t.TempDir(), "delete.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := service.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	if _, err := service.Query(context.Background(), "CREATE TABLE imported_data(value INTEGER)", 10); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteSource(context.Background(), model.Source{Kind: "csv", Table: "imported_data"}); err != nil {
		t.Fatal(err)
	}
	tables, err := service.ListTables(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		if table.Name == "imported_data" {
			t.Fatalf("deleted table still present: %#v", table)
		}
	}
}

func TestServiceResetUserData(t *testing.T) {
	t.Parallel()
	service, err := Open(context.Background(), filepath.Join(t.TempDir(), "reset.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := service.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	if _, err := service.Query(context.Background(), `
		CREATE TABLE user_table(value INTEGER);
		CREATE VIEW user_view AS SELECT * FROM user_table;
	`, 10); err != nil {
		t.Fatal(err)
	}
	if err := service.ResetUserData(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	tables, err := service.ListTables(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 0 {
		t.Fatalf("tables after reset = %#v", tables)
	}
}
