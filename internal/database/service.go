package database

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alexwoo79/duckdb-report/internal/model"
	"github.com/xuri/excelize/v2"

	duckdb "github.com/duckdb/duckdb-go/v2"
)

const maxScriptDepth = 24

type Service struct {
	mu   sync.Mutex
	db   *sql.DB
	path string
}

func Open(ctx context.Context, path string) (*Service, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("database path is empty")
	}
	db, err := sql.Open("duckdb", path)
	if err != nil {
		return nil, fmt.Errorf("open duckdb: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect duckdb: %w", err)
	}
	return &Service{db: db, path: path}, nil
}

func (s *Service) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close duckdb: %w", err)
	}
	return nil
}

func (s *Service) Path() string {
	return s.path
}

func (s *Service) Version(ctx context.Context) (string, error) {
	var version string
	if err := s.db.QueryRowContext(ctx, "SELECT version()").Scan(&version); err != nil {
		return "", fmt.Errorf("read duckdb version: %w", err)
	}
	return version, nil
}

func (s *Service) Snapshot(ctx context.Context, destination string) error {
	if strings.TrimSpace(destination) == "" {
		return errors.New("snapshot destination is empty")
	}
	if _, err := os.Stat(destination); err == nil {
		return fmt.Errorf("snapshot destination already exists: %s", destination)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect snapshot destination: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var databaseName string
	if err := s.db.QueryRowContext(ctx, "SELECT current_database()").Scan(&databaseName); err != nil {
		return fmt.Errorf("read current database name: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, "CHECKPOINT"); err != nil {
		return fmt.Errorf("checkpoint before snapshot: %w", err)
	}
	alias := "_duckdb_report_snapshot"
	attach := fmt.Sprintf(
		"ATTACH %s AS %s (STORAGE_VERSION 'v1.0.0')",
		quoteLiteral(destination),
		quoteIdentifier(alias),
	)
	if _, err := s.db.ExecContext(ctx, attach); err != nil {
		return fmt.Errorf("attach report snapshot: %w", err)
	}
	copyErr := func() error {
		query := fmt.Sprintf(
			"COPY FROM DATABASE %s TO %s",
			quoteIdentifier(databaseName),
			quoteIdentifier(alias),
		)
		_, err := s.db.ExecContext(ctx, query)
		return err
	}()
	_, detachErr := s.db.ExecContext(ctx, "DETACH "+quoteIdentifier(alias))
	if copyErr != nil {
		return fmt.Errorf("copy report snapshot: %w", errors.Join(copyErr, detachErr))
	}
	if detachErr != nil {
		return fmt.Errorf("detach report snapshot: %w", detachErr)
	}
	return nil
}

func (s *Service) Query(ctx context.Context, query string, limit int) (model.QueryResult, error) {
	if strings.TrimSpace(query) == "" {
		return model.QueryResult{}, errors.New("sql is empty")
	}
	if limit <= 0 {
		limit = 500
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	started := time.Now()
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return model.QueryResult{}, fmt.Errorf("execute sql: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return model.QueryResult{}, fmt.Errorf("read result columns: %w", err)
	}
	result := model.QueryResult{
		Columns:     columns,
		Rows:        make([][]any, 0),
		ExecutedSQL: query,
	}
	if len(columns) == 0 {
		result.ElapsedMS = time.Since(started).Milliseconds()
		return result, nil
	}

	for rows.Next() {
		if len(result.Rows) >= limit {
			result.Truncated = true
			break
		}
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for i := range values {
			destinations[i] = &values[i]
		}
		if err := rows.Scan(destinations...); err != nil {
			return model.QueryResult{}, fmt.Errorf("scan query result: %w", err)
		}
		for i := range values {
			values[i] = normalizeValue(values[i])
		}
		result.Rows = append(result.Rows, values)
	}
	if err := rows.Err(); err != nil {
		return model.QueryResult{}, fmt.Errorf("iterate query result: %w", err)
	}
	result.RowCount = len(result.Rows)
	result.ElapsedMS = time.Since(started).Milliseconds()
	return result, nil
}

// ValidateSyntax parses SQL without executing statements.
func (s *Service) ValidateSyntax(ctx context.Context, query string) error {
	if strings.TrimSpace(query) == "" {
		return errors.New("sql is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.tableNamesLocked(ctx, query)
	if err != nil {
		return err
	}
	return nil
}

// TableNames parses SQL and extracts referenced table names without executing it.
func (s *Service) TableNames(ctx context.Context, query string) ([]string, error) {
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("sql is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tableNamesLocked(ctx, query)
}

func (s *Service) tableNamesLocked(ctx context.Context, query string) ([]string, error) {
	connection, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire duckdb connection: %w", err)
	}
	defer connection.Close()
	names, err := duckdb.GetTableNames(connection, query, true)
	if err != nil {
		return nil, fmt.Errorf("parse sql: %w", err)
	}
	return names, nil
}

// Explain validates query binding and returns a plan without executing the query.
func (s *Service) Explain(ctx context.Context, query string) error {
	if strings.TrimSpace(query) == "" {
		return errors.New("sql is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.QueryContext(ctx, "EXPLAIN "+query)
	if err != nil {
		return fmt.Errorf("explain sql: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate explain plan: %w", err)
	}
	return nil
}

func ExpandScript(script, baseDir string) (string, error) {
	visited := make(map[string]struct{})
	return expandScript(script, baseDir, visited, 0)
}

func (s *Service) Import(ctx context.Context, request model.ImportRequest) (model.ImportResult, error) {
	path, err := filepath.Abs(strings.TrimSpace(request.Path))
	if err != nil {
		return model.ImportResult{}, fmt.Errorf("resolve source path: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return model.ImportResult{}, fmt.Errorf("open source file: %w", err)
	}
	if info.IsDir() {
		return model.ImportResult{}, errors.New("source path is a directory")
	}

	kind := strings.ToLower(strings.TrimSpace(request.Kind))
	if kind == "" {
		kind = strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	}
	table := strings.TrimSpace(request.Table)
	if table == "" {
		table = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	source := model.Source{Kind: kind, Path: path, Table: table, Sheet: request.Sheet}
	logEntries := make([]string, 0, 3)

	s.mu.Lock()
	switch kind {
	case "csv", "json", "parquet":
		query := fmt.Sprintf(
			"CREATE OR REPLACE TABLE %s AS SELECT * FROM %s(%s)",
			quoteIdentifier(table),
			readerForKind(kind),
			quoteLiteral(path),
		)
		if _, err := s.db.ExecContext(ctx, query); err != nil {
			s.mu.Unlock()
			return model.ImportResult{}, fmt.Errorf("import %s: %w", kind, err)
		}
		logEntries = append(logEntries, fmt.Sprintf("Imported %s as table %s", filepath.Base(path), table))
	case "xlsx", "xlsm":
		if err := s.importExcelLocked(ctx, path, table, request.Sheet); err != nil {
			s.mu.Unlock()
			return model.ImportResult{}, err
		}
		if source.Sheet == "" {
			source.Sheet = firstSheet(path)
		}
		logEntries = append(logEntries, fmt.Sprintf("Imported %s as table %s", filepath.Base(path), table))
	case "duckdb", "db":
		alias := table
		query := fmt.Sprintf("ATTACH IF NOT EXISTS %s AS %s", quoteLiteral(path), quoteIdentifier(alias))
		if _, err := s.db.ExecContext(ctx, query); err != nil {
			s.mu.Unlock()
			return model.ImportResult{}, fmt.Errorf("attach duckdb database: %w", err)
		}
		source.Kind = "duckdb"
		logEntries = append(logEntries, fmt.Sprintf("Attached %s as catalog %s", filepath.Base(path), alias))
	default:
		s.mu.Unlock()
		return model.ImportResult{}, fmt.Errorf("unsupported source type %q", kind)
	}
	s.mu.Unlock()

	tables, err := s.ListTables(ctx)
	if err != nil {
		return model.ImportResult{}, err
	}
	return model.ImportResult{Source: source, Tables: tables, Log: logEntries}, nil
}

func (s *Service) DeleteSource(ctx context.Context, source model.Source) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deleteSourceLocked(ctx, source)
}

func (s *Service) ResetUserData(ctx context.Context, sources []model.Source) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var currentDatabase string
	if err := s.db.QueryRowContext(ctx, "SELECT current_database()").Scan(&currentDatabase); err != nil {
		return fmt.Errorf("read workspace database: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT table_schema, table_name, table_type
		FROM information_schema.tables
		WHERE table_catalog = ?
		  AND table_schema NOT IN ('information_schema', 'pg_catalog')
	`, currentDatabase)
	if err != nil {
		return fmt.Errorf("list workspace tables for reset: %w", err)
	}
	type tableDrop struct {
		schema string
		name   string
		kind   string
	}
	var tables []tableDrop
	for rows.Next() {
		var table tableDrop
		if err := rows.Scan(&table.schema, &table.name, &table.kind); err != nil {
			rows.Close()
			return fmt.Errorf("scan workspace table for reset: %w", err)
		}
		tables = append(tables, table)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close workspace table list: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate workspace tables for reset: %w", err)
	}
	sort.SliceStable(tables, func(i, j int) bool {
		return strings.EqualFold(tables[i].kind, "VIEW") && !strings.EqualFold(tables[j].kind, "VIEW")
	})
	var failures []error
	for _, table := range tables {
		kind := "TABLE"
		if strings.EqualFold(table.kind, "VIEW") {
			kind = "VIEW"
		}
		query := fmt.Sprintf(
			"DROP %s IF EXISTS %s.%s",
			kind,
			quoteIdentifier(table.schema),
			quoteIdentifier(table.name),
		)
		if _, err := s.db.ExecContext(ctx, query); err != nil {
			failures = append(failures, fmt.Errorf("drop %s.%s: %w", table.schema, table.name, err))
		}
	}
	for _, source := range sources {
		if source.Kind != "duckdb" {
			continue
		}
		if err := s.deleteSourceLocked(ctx, source); err != nil {
			failures = append(failures, err)
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("reset workspace data: %w", errors.Join(failures...))
	}
	return nil
}

func (s *Service) deleteSourceLocked(ctx context.Context, source model.Source) error {
	switch source.Kind {
	case "duckdb":
		_, err := s.db.ExecContext(ctx, "DETACH "+quoteIdentifier(source.Table))
		if err != nil && !strings.Contains(strings.ToLower(err.Error()), "not attached") {
			return fmt.Errorf("detach source %q: %w", source.Table, err)
		}
		return nil
	case "csv", "json", "parquet", "xlsx", "xlsm":
		_, err := s.db.ExecContext(ctx, "DROP TABLE IF EXISTS "+quoteIdentifier(source.Table))
		if err != nil {
			return fmt.Errorf("drop imported table %q: %w", source.Table, err)
		}
		return nil
	default:
		return fmt.Errorf("cannot delete unsupported source kind %q", source.Kind)
	}
}

func (s *Service) Reattach(ctx context.Context, sources []model.Source) error {
	for _, source := range sources {
		if source.Kind != "duckdb" {
			continue
		}
		s.mu.Lock()
		query := fmt.Sprintf(
			"ATTACH IF NOT EXISTS %s AS %s",
			quoteLiteral(source.Path),
			quoteIdentifier(source.Table),
		)
		_, err := s.db.ExecContext(ctx, query)
		s.mu.Unlock()
		if err != nil {
			return fmt.Errorf("reattach duckdb source %q: %w", source.Table, err)
		}
	}
	return nil
}

func (s *Service) ListTables(ctx context.Context) ([]model.TableRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT table_catalog, table_schema, table_name, table_type
		FROM information_schema.tables
		WHERE table_schema NOT IN ('information_schema', 'pg_catalog')
		ORDER BY table_catalog, table_schema, table_name
	`)
	if err != nil {
		return nil, fmt.Errorf("list duckdb tables: %w", err)
	}
	defer rows.Close()

	var result []model.TableRef
	for rows.Next() {
		var table model.TableRef
		if err := rows.Scan(&table.Catalog, &table.Schema, &table.Name, &table.Type); err != nil {
			return nil, fmt.Errorf("scan duckdb table: %w", err)
		}
		result = append(result, table)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate duckdb tables: %w", err)
	}
	return result, nil
}

func (s *Service) importExcelLocked(ctx context.Context, path, table, sheet string) error {
	workbook, err := excelize.OpenFile(path)
	if err != nil {
		return fmt.Errorf("open excel workbook: %w", err)
	}
	defer workbook.Close()

	sheets := workbook.GetSheetList()
	if len(sheets) == 0 {
		return errors.New("excel workbook has no sheets")
	}
	if strings.TrimSpace(sheet) == "" {
		sheet = sheets[0]
	}
	rows, err := workbook.GetRows(sheet)
	if err != nil {
		return fmt.Errorf("read excel sheet %q: %w", sheet, err)
	}
	if len(rows) == 0 {
		return fmt.Errorf("excel sheet %q is empty", sheet)
	}

	temp, err := os.CreateTemp("", "duckdb-report-*.csv")
	if err != nil {
		return fmt.Errorf("create excel staging file: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	writer := csv.NewWriter(temp)
	writeErr := writer.WriteAll(rows)
	closeErr := temp.Close()
	if writeErr != nil {
		return fmt.Errorf("write excel staging csv: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close excel staging csv: %w", closeErr)
	}

	query := fmt.Sprintf(
		"CREATE OR REPLACE TABLE %s AS SELECT * FROM read_csv_auto(%s, header = true)",
		quoteIdentifier(table),
		quoteLiteral(tempPath),
	)
	if _, err := s.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("load excel sheet %q into duckdb: %w", sheet, err)
	}
	return nil
}

func firstSheet(path string) string {
	workbook, err := excelize.OpenFile(path)
	if err != nil {
		return ""
	}
	defer workbook.Close()
	sheets := workbook.GetSheetList()
	if len(sheets) == 0 {
		return ""
	}
	return sheets[0]
}

func readerForKind(kind string) string {
	switch kind {
	case "json":
		return "read_json_auto"
	case "parquet":
		return "read_parquet"
	default:
		return "read_csv_auto"
	}
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func quoteLiteral(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}

func normalizeValue(value any) any {
	switch typed := value.(type) {
	case []byte:
		return string(typed)
	case time.Time:
		return typed.Format(time.RFC3339)
	case fmt.Stringer:
		return typed.String()
	case nil:
		return nil
	default:
		return typed
	}
}

func SortedSheets(path string) ([]string, error) {
	workbook, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("open excel workbook: %w", err)
	}
	defer workbook.Close()
	sheets := workbook.GetSheetList()
	sort.Strings(sheets)
	return sheets, nil
}

func expandScript(script, baseDir string, visited map[string]struct{}, depth int) (string, error) {
	if depth > maxScriptDepth {
		return "", fmt.Errorf("sql include depth exceeds %d", maxScriptDepth)
	}
	var builder strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(script))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, ".read ") {
			builder.WriteString(line)
			builder.WriteByte('\n')
			continue
		}
		includeValue := strings.TrimSpace(strings.TrimPrefix(trimmed, ".read "))
		includeValue = strings.Trim(includeValue, `"'`)
		if includeValue == "" {
			return "", fmt.Errorf("expand sql line %d: empty .read path", lineNumber)
		}
		if !filepath.IsAbs(includeValue) {
			includeValue = filepath.Join(baseDir, includeValue)
		}
		includeValue = filepath.Clean(includeValue)
		if _, exists := visited[includeValue]; exists {
			return "", fmt.Errorf("expand sql line %d: cyclic .read for %q", lineNumber, includeValue)
		}
		visited[includeValue] = struct{}{}
		included, err := os.ReadFile(includeValue)
		if err != nil {
			return "", fmt.Errorf("expand sql line %d: read %q: %w", lineNumber, includeValue, err)
		}
		expanded, err := expandScript(string(included), filepath.Dir(includeValue), visited, depth+1)
		delete(visited, includeValue)
		if err != nil {
			return "", err
		}
		builder.WriteString(expanded)
		if !strings.HasSuffix(expanded, "\n") {
			builder.WriteByte('\n')
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan sql script: %w", err)
	}
	return builder.String(), nil
}
