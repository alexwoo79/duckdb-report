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

func TestServiceWriteTableRow(t *testing.T) {
	t.Parallel()
	service, err := Open(context.Background(), filepath.Join(t.TempDir(), "write.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := service.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})

	if _, err := service.Query(context.Background(), `
		CREATE TABLE editor_people(
			id BIGINT,
			name VARCHAR,
			active BOOLEAN,
			note VARCHAR DEFAULT 'seed'
		);
	`, 1); err != nil {
		t.Fatal(err)
	}
	tables, err := service.ListTables(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var table model.TableRef
	for _, candidate := range tables {
		if candidate.Name == "editor_people" {
			table = candidate
			break
		}
	}
	if table.Name == "" {
		t.Fatalf("editor_people table not found: %#v", tables)
	}

	maliciousName := `x'); DROP TABLE editor_people; --`
	inserted, err := service.WriteTableRow(context.Background(), model.TableWriteRequest{
		Operation: model.TableWriteInsert,
		Table:     table,
		Values: []model.TableValue{
			{Column: "id", Value: "1"},
			{Column: "name", Value: maliciousName},
			{Column: "active", Value: "true"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if inserted.RowsAffected != 1 {
		t.Fatalf("inserted rows = %d, want 1", inserted.RowsAffected)
	}
	if inserted.SQL != `INSERT INTO "write"."main"."editor_people" ("id", "name", "active") VALUES (?, ?, ?)` {
		t.Fatalf("insert SQL = %q", inserted.SQL)
	}

	updated, err := service.WriteTableRow(context.Background(), model.TableWriteRequest{
		Operation: model.TableWriteUpdate,
		Table:     table,
		Values: []model.TableValue{
			{Column: "active", Value: "false"},
			{Column: "note", Null: true},
		},
		Filters: []model.TableFilter{
			{Column: "id", Operator: "eq", Value: "1"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.RowsAffected != 1 {
		t.Fatalf("updated rows = %d, want 1", updated.RowsAffected)
	}
	if updated.SQL != `UPDATE "write"."main"."editor_people" SET "active" = ?, "note" = ? WHERE "id" = ?` {
		t.Fatalf("update SQL = %q", updated.SQL)
	}

	result, err := service.Query(context.Background(), `
		SELECT id, name, active, note
		FROM editor_people
	`, 10)
	if err != nil {
		t.Fatal(err)
	}
	if result.RowCount != 1 {
		t.Fatalf("row count = %d, want 1", result.RowCount)
	}
	if result.Rows[0][0] != int64(1) ||
		result.Rows[0][1] != maliciousName ||
		result.Rows[0][2] != false ||
		result.Rows[0][3] != nil {
		t.Fatalf("stored row = %#v", result.Rows[0])
	}
}

func TestServiceWriteTableRowRejectsUnsafeRequests(t *testing.T) {
	t.Parallel()
	service, err := Open(context.Background(), filepath.Join(t.TempDir(), "write-errors.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := service.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	if _, err := service.Query(context.Background(), `
		CREATE TABLE guarded(id INTEGER, label VARCHAR);
		CREATE VIEW guarded_view AS SELECT * FROM guarded;
	`, 1); err != nil {
		t.Fatal(err)
	}
	tables, err := service.ListTables(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	refs := make(map[string]model.TableRef)
	for _, table := range tables {
		refs[table.Name] = table
	}

	tests := []struct {
		name    string
		request model.TableWriteRequest
	}{
		{
			name: "update without filter",
			request: model.TableWriteRequest{
				Operation: model.TableWriteUpdate,
				Table:     refs["guarded"],
				Values:    []model.TableValue{{Column: "label", Value: "changed"}},
			},
		},
		{
			name: "unknown column",
			request: model.TableWriteRequest{
				Operation: model.TableWriteInsert,
				Table:     refs["guarded"],
				Values:    []model.TableValue{{Column: "missing", Value: "value"}},
			},
		},
		{
			name: "unsupported filter operator",
			request: model.TableWriteRequest{
				Operation: model.TableWriteUpdate,
				Table:     refs["guarded"],
				Values:    []model.TableValue{{Column: "label", Value: "changed"}},
				Filters:   []model.TableFilter{{Column: "id", Operator: "id = 1 OR 1", Value: "1"}},
			},
		},
		{
			name: "view write",
			request: model.TableWriteRequest{
				Operation: model.TableWriteInsert,
				Table:     refs["guarded_view"],
				Values:    []model.TableValue{{Column: "id", Value: "1"}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := service.WriteTableRow(context.Background(), test.request); err == nil {
				t.Fatal("WriteTableRow() error = nil")
			}
		})
	}
}

func TestServiceLoadTableEditorInfo(t *testing.T) {
	t.Parallel()
	service, err := Open(context.Background(), filepath.Join(t.TempDir(), "editor.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := service.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	if _, err := service.Query(context.Background(), `
		CREATE TABLE editor_parent(
			id BIGINT PRIMARY KEY,
			name VARCHAR
		);
		CREATE TABLE editor_child(
			id BIGINT PRIMARY KEY,
			parent_id BIGINT REFERENCES editor_parent(id),
			note VARCHAR DEFAULT 'new'
		);
		CREATE VIEW editor_child_view AS SELECT * FROM editor_child;
	`, 1); err != nil {
		t.Fatal(err)
	}
	tables, err := service.ListTables(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	refs := make(map[string]model.TableRef)
	for _, table := range tables {
		refs[table.Name] = table
	}

	child, err := service.LoadTableEditorInfo(context.Background(), refs["editor_child"])
	if err != nil {
		t.Fatal(err)
	}
	if !child.Writable {
		t.Fatal("child table should be writable")
	}
	if child.KeyType != "PRIMARY KEY" ||
		len(child.KeyColumns) != 1 ||
		child.KeyColumns[0] != "id" {
		t.Fatalf("child key = %q %#v", child.KeyType, child.KeyColumns)
	}
	if len(child.Keys) != 1 ||
		child.Keys[0].Name == "" ||
		child.Keys[0].Type != "PRIMARY KEY" {
		t.Fatalf("child keys = %#v", child.Keys)
	}
	if len(child.Columns) != 3 || child.Columns[2].DefaultValue != "'new'" {
		t.Fatalf("child columns = %#v", child.Columns)
	}
	if len(child.Relations) != 1 ||
		child.Relations[0].Direction != "outgoing" ||
		child.Relations[0].Target.Name != "editor_parent" {
		t.Fatalf("child relations = %#v", child.Relations)
	}

	parent, err := service.LoadTableEditorInfo(context.Background(), refs["editor_parent"])
	if err != nil {
		t.Fatal(err)
	}
	if len(parent.Relations) != 1 ||
		parent.Relations[0].Direction != "incoming" ||
		parent.Relations[0].Source.Name != "editor_child" {
		t.Fatalf("parent relations = %#v", parent.Relations)
	}

	view, err := service.LoadTableEditorInfo(context.Background(), refs["editor_child_view"])
	if err != nil {
		t.Fatal(err)
	}
	if view.Writable {
		t.Fatal("view should be read-only")
	}
	if view.SQL == "" {
		t.Fatal("view SQL should be available")
	}
}

func TestServiceAlterTableStructure(t *testing.T) {
	t.Parallel()
	service, err := Open(context.Background(), filepath.Join(t.TempDir(), "structure.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := service.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	if _, err := service.Query(context.Background(), `
		CREATE TABLE structure_items(
			id INTEGER,
			label VARCHAR DEFAULT 'draft',
			amount INTEGER
		);
		CREATE VIEW structure_view AS SELECT * FROM structure_items;
	`, 1); err != nil {
		t.Fatal(err)
	}
	tables, err := service.ListTables(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	refs := make(map[string]model.TableRef)
	for _, table := range tables {
		refs[table.Name] = table
	}
	table := refs["structure_items"]

	preview, err := service.PreviewTableStructure(context.Background(), model.TableStructureRequest{
		Operation:  model.TableStructureAddColumn,
		Table:      table,
		Column:     "note",
		ColumnType: "VARCHAR",
	})
	if err != nil {
		t.Fatal(err)
	}
	if preview.SQL != `ALTER TABLE "structure"."main"."structure_items" ADD COLUMN "note" VARCHAR` {
		t.Fatalf("preview SQL = %q", preview.SQL)
	}
	before, err := service.LoadTableEditorInfo(context.Background(), table)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Columns) != 3 {
		t.Fatalf("preview changed table structure: %#v", before.Columns)
	}

	steps := []model.TableStructureRequest{
		{
			Operation:  model.TableStructureAddColumn,
			Table:      table,
			Column:     "note",
			ColumnType: "VARCHAR",
		},
		{
			Operation: model.TableStructureRenameColumn,
			Table:     table,
			Column:    "amount",
			NewName:   "total",
		},
		{
			Operation:  model.TableStructureAlterType,
			Table:      table,
			Column:     "total",
			ColumnType: "BIGINT",
		},
		{
			Operation: model.TableStructureDropDefault,
			Table:     table,
			Column:    "label",
		},
		{
			Operation: model.TableStructureSetNotNull,
			Table:     table,
			Column:    "label",
		},
		{
			Operation: model.TableStructureDropNotNull,
			Table:     table,
			Column:    "label",
		},
		{
			Operation: model.TableStructureDropColumn,
			Table:     table,
			Column:    "note",
		},
		{
			Operation: model.TableStructureAddUniqueKey,
			Table:     table,
			Columns:   []string{"label"},
		},
		{
			Operation: model.TableStructureAddPrimaryKey,
			Table:     table,
			Columns:   []string{"id"},
		},
	}
	for _, request := range steps {
		if _, err := service.AlterTableStructure(context.Background(), request); err != nil {
			t.Fatalf("%s: %v", request.Operation, err)
		}
	}

	renamed, err := service.AlterTableStructure(context.Background(), model.TableStructureRequest{
		Operation: model.TableStructureRenameTable,
		Table:     table,
		NewName:   "structure_items_v2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Table.Name != "structure_items_v2" {
		t.Fatalf("renamed table = %#v", renamed.Table)
	}
	after, err := service.LoadTableEditorInfo(context.Background(), renamed.Table)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Columns) != 3 ||
		after.Columns[1].Name != "label" ||
		after.Columns[2].Name != "total" ||
		after.Columns[1].DefaultValue != "" {
		t.Fatalf("altered columns = %#v", after.Columns)
	}
	if len(after.Keys) != 2 || after.KeyType != "PRIMARY KEY" {
		t.Fatalf("altered keys = %#v, row key = %q", after.Keys, after.KeyType)
	}

	for _, request := range []model.TableStructureRequest{
		{
			Operation:  model.TableStructureAddColumn,
			Table:      renamed.Table,
			Column:     "unsafe",
			ColumnType: "VARCHAR); DROP TABLE structure_items_v2; --",
		},
		{
			Operation: model.TableStructureRenameTable,
			Table:     renamed.Table,
			NewName:   "",
		},
		{
			Operation:  model.TableStructureAddColumn,
			Table:      refs["structure_view"],
			Column:     "note",
			ColumnType: "VARCHAR",
		},
		{
			Operation: model.TableStructureAddPrimaryKey,
			Table:     renamed.Table,
			Columns:   []string{"id"},
		},
		{
			Operation: model.TableStructureAddUniqueKey,
			Table:     renamed.Table,
			Columns:   []string{"label"},
		},
	} {
		if _, err := service.AlterTableStructure(context.Background(), request); err == nil {
			t.Fatalf("%s with invalid request should fail", request.Operation)
		}
	}
}

func TestServiceTableEditorWithRelationalFixture(t *testing.T) {
	t.Parallel()
	service, err := Open(
		context.Background(),
		filepath.Join(t.TempDir(), "table-editor-fixture.duckdb"),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := service.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	fixture, err := os.ReadFile(
		filepath.Join("testdata", "table_editor_fixture.sql"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Query(context.Background(), string(fixture), 1); err != nil {
		t.Fatal(err)
	}

	tables, err := service.ListTables(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	refs := make(map[string]model.TableRef)
	for _, table := range tables {
		refs[table.Name] = table
	}
	for _, name := range []string{
		"customers",
		"products",
		"orders",
		"order_items",
		"campaigns",
	} {
		if refs[name].Name == "" {
			t.Fatalf("fixture table %q not found: %#v", name, tables)
		}
	}

	customers, err := service.LoadTableEditorInfo(
		context.Background(),
		refs["customers"],
	)
	if err != nil {
		t.Fatal(err)
	}
	if customers.KeyType != "PRIMARY KEY" ||
		len(customers.Keys) != 2 ||
		customers.Comment == "" {
		t.Fatalf("customer editor metadata = %#v", customers)
	}
	if len(customers.Relations) != 1 ||
		customers.Relations[0].Direction != "incoming" ||
		customers.Relations[0].Source.Name != "orders" {
		t.Fatalf("customer relations = %#v", customers.Relations)
	}

	orders, err := service.LoadTableEditorInfo(
		context.Background(),
		refs["orders"],
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(orders.Relations) != 2 {
		t.Fatalf("order relations = %#v", orders.Relations)
	}
	outgoingToCustomers := false
	incomingFromOrderItems := false
	for _, relation := range orders.Relations {
		if relation.Direction == "outgoing" &&
			relation.Target.Name == "customers" {
			outgoingToCustomers = true
		}
		if relation.Direction == "incoming" &&
			relation.Source.Name == "order_items" {
			incomingFromOrderItems = true
		}
	}
	if !outgoingToCustomers || !incomingFromOrderItems {
		t.Fatalf("order relation directions = %#v", orders.Relations)
	}
	if len(orders.Keys) != 1 || orders.Keys[0].Type != "PRIMARY KEY" {
		t.Fatalf("order keys = %#v", orders.Keys)
	}

	orderItems, err := service.LoadTableEditorInfo(
		context.Background(),
		refs["order_items"],
	)
	if err != nil {
		t.Fatal(err)
	}
	if orderItems.KeyType != "PRIMARY KEY" ||
		len(orderItems.KeyColumns) != 2 ||
		orderItems.KeyColumns[0] != "order_id" ||
		orderItems.KeyColumns[1] != "product_id" {
		t.Fatalf(
			"order item key = %q %#v",
			orderItems.KeyType,
			orderItems.KeyColumns,
		)
	}
	if len(orderItems.Relations) != 2 {
		t.Fatalf("order item relations = %#v", orderItems.Relations)
	}
	for _, relation := range orderItems.Relations {
		if relation.Direction != "outgoing" {
			t.Fatalf("order item relation direction = %#v", relation)
		}
	}

	counts, err := service.Query(context.Background(), `
		SELECT
			(SELECT count(*) FROM customers) AS customers,
			(SELECT count(*) FROM orders) AS orders,
			(SELECT count(*) FROM order_items) AS order_items
	`, 1)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(counts.Rows[0]) != "[5 4 7]" {
		t.Fatalf("fixture row counts = %#v", counts.Rows[0])
	}

	updated, err := service.WriteTableRow(
		context.Background(),
		model.TableWriteRequest{
			Operation: model.TableWriteInsert,
			Table:     refs["customers"],
			Values: []model.TableValue{
				{Column: "customer_id", Value: "6"},
				{Column: "name", Value: "Margaret Hamilton"},
				{Column: "email", Value: "margaret@example.com"},
				{Column: "region", Null: true},
				{Column: "created_at", Value: "2026-02-10 09:00:00"},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if updated.RowsAffected != 1 {
		t.Fatalf("inserted rows = %d", updated.RowsAffected)
	}
	changed, err := service.WriteTableRow(
		context.Background(),
		model.TableWriteRequest{
			Operation: model.TableWriteUpdate,
			Table:     refs["order_items"],
			Values: []model.TableValue{
				{Column: "quantity", Value: "4"},
			},
			Filters: []model.TableFilter{
				{Column: "order_id", Operator: "eq", Value: "1001"},
				{Column: "product_id", Operator: "eq", Value: "101"},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if changed.RowsAffected != 1 {
		t.Fatalf("updated rows = %d", changed.RowsAffected)
	}
	quantity, err := service.Query(context.Background(), `
		SELECT quantity
		FROM order_items
		WHERE order_id = 1001 AND product_id = 101
	`, 1)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(quantity.Rows[0][0]) != "4" {
		t.Fatalf("updated quantity = %#v", quantity.Rows)
	}

	preview, err := service.PreviewTableStructure(
		context.Background(),
		model.TableStructureRequest{
			Operation: model.TableStructureAddUniqueKey,
			Table:     refs["campaigns"],
			Columns:   []string{"name", "start_date"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview.SQL, `ADD UNIQUE ("name", "start_date")`) {
		t.Fatalf("key preview SQL = %q", preview.SQL)
	}
	if _, err := service.AlterTableStructure(
		context.Background(),
		model.TableStructureRequest{
			Operation: model.TableStructureAddUniqueKey,
			Table:     refs["campaigns"],
			Columns:   []string{"name", "start_date"},
		},
	); err != nil {
		t.Fatal(err)
	}
	campaignsAfterKey, err := service.LoadTableEditorInfo(
		context.Background(),
		refs["campaigns"],
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(campaignsAfterKey.Keys) != 2 ||
		campaignsAfterKey.Keys[1].Type != "UNIQUE" ||
		len(campaignsAfterKey.Keys[1].Columns) != 2 {
		t.Fatalf("campaign keys after alter = %#v", campaignsAfterKey.Keys)
	}
}

func TestServiceValidateSyntax(t *testing.T) {
	t.Parallel()
	service, err := Open(context.Background(), filepath.Join(t.TempDir(), "syntax.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := service.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})

	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{name: "valid select", query: "SELECT 1 AS value"},
		{name: "valid multi statement DDL", query: "CREATE TEMP TABLE syntax_test(value INTEGER); SELECT value FROM syntax_test;"},
		{name: "invalid syntax", query: "SELECT FROM", wantErr: true},
		{name: "empty sql", query: " ", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := service.ValidateSyntax(context.Background(), test.query)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateSyntax() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestServiceTableNames(t *testing.T) {
	t.Parallel()
	service, err := Open(context.Background(), filepath.Join(t.TempDir(), "tables.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := service.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	names, err := service.TableNames(context.Background(), `SELECT * FROM schema1.orders, catalog3."schema.2"."customers"`)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Fatalf("TableNames() = %#v, want orders and customers", names)
	}
}

func TestServiceListDatabases(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	service, err := Open(context.Background(), filepath.Join(root, "current.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := service.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	attachedPath := filepath.Join(root, "attached.duckdb")
	if _, err := service.Query(context.Background(), fmt.Sprintf(
		"ATTACH %s AS attached_db",
		quoteLiteral(attachedPath),
	), 1); err != nil {
		t.Fatal(err)
	}

	databases, err := service.ListDatabases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(databases) != 2 {
		t.Fatalf("databases = %#v", databases)
	}
	if !databases[0].IsCurrent || databases[0].Name != "current" {
		t.Fatalf("current database = %#v", databases[0])
	}
	if databases[1].Name != "attached_db" || databases[1].IsCurrent {
		t.Fatalf("attached database = %#v", databases[1])
	}
}

func TestServiceExplain(t *testing.T) {
	t.Parallel()
	service, err := Open(context.Background(), filepath.Join(t.TempDir(), "explain.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := service.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	if err := service.Explain(context.Background(), "SELECT 1 AS value"); err != nil {
		t.Fatal(err)
	}
	if err := service.Explain(context.Background(), "SELECT * FROM missing_table"); err == nil {
		t.Fatal("expected missing table explain to fail")
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

func TestServiceSQLCompletions(t *testing.T) {
	t.Parallel()
	service, err := Open(context.Background(), filepath.Join(t.TempDir(), "completions.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := service.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	if _, err := service.Query(context.Background(), `
		CREATE TABLE completion_orders(
			order_id BIGINT,
			customer_name VARCHAR,
			total DECIMAL(18, 2),
			status VARCHAR DEFAULT 'new',
			optional_note VARCHAR
		);
	`, 10); err != nil {
		t.Fatal(err)
	}

	catalog, err := service.SQLCompletions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var found *model.SQLCompletionTable
	for i := range catalog.Tables {
		if catalog.Tables[i].Name == "completion_orders" {
			found = &catalog.Tables[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("completion table not found: %#v", catalog.Tables)
	}
	if len(found.Columns) != 5 {
		t.Fatalf("columns = %#v", found.Columns)
	}
	if found.Columns[0].Name != "order_id" || found.Columns[0].Type != "BIGINT" {
		t.Fatalf("first column = %#v", found.Columns[0])
	}
	if !found.Columns[3].Nullable || found.Columns[3].DefaultValue != "'new'" {
		t.Fatalf("defaulted column metadata = %#v", found.Columns[3])
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
