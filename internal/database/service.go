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
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alexwoo79/duckdb-report/internal/model"
	"github.com/xuri/excelize/v2"

	duckdb "github.com/duckdb/duckdb-go/v2"
)

const maxScriptDepth = 24
const (
	defaultDuckDBMemoryLimit = "60%"
	maxDuckDBThreads         = 1024
)

var duckDBMemoryLimitPattern = regexp.MustCompile(
	`^[0-9]+(?:\.[0-9]+)?(?:B|KB|MB|GB|TB|%)$`,
)

var tableColumnTypePattern = regexp.MustCompile(
	`(?i)^(?:BOOLEAN|BOOL|TINYINT|SMALLINT|INTEGER|BIGINT|HUGEINT|UTINYINT|USMALLINT|UINTEGER|UBIGINT|UHUGEINT|REAL|FLOAT|DOUBLE|DECIMAL(?:\s*\(\s*[0-9]{1,3}\s*,\s*[0-9]{1,3}\s*\))?|NUMERIC(?:\s*\(\s*[0-9]{1,3}\s*,\s*[0-9]{1,3}\s*\))?|VARCHAR(?:\s*\(\s*[0-9]{1,9}\s*\))?|CHAR(?:\s*\(\s*[0-9]{1,9}\s*\))?|BPCHAR|TEXT|BLOB|BYTEA|BIT|DATE|TIME|TIME WITH TIME ZONE|TIMESTAMP|TIMESTAMP WITH TIME ZONE|TIMESTAMP_S|TIMESTAMP_MS|TIMESTAMP_NS|TIMESTAMPTZ|UUID|JSON|INTERVAL)(?:\s*\[\]){0,3}$`,
)

type Service struct {
	mu      sync.Mutex
	db      *sql.DB
	path    string
	baseDir string
}

func Open(ctx context.Context, path string) (*Service, error) {
	return OpenWithBaseDir(ctx, path, "")
}

// OpenWithBaseDir opens a database and resolves relative SQL file paths against
// baseDir instead of the process working directory.
func OpenWithBaseDir(ctx context.Context, path, baseDir string) (*Service, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("database path is empty")
	}
	if strings.TrimSpace(baseDir) != "" {
		absolute, err := filepath.Abs(baseDir)
		if err != nil {
			return nil, fmt.Errorf("resolve database base directory: %w", err)
		}
		baseDir = absolute
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
	if err := configureResources(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return &Service{db: db, path: path, baseDir: baseDir}, nil
}

func configureResources(ctx context.Context, db *sql.DB) error {
	query := fmt.Sprintf("SET threads = %d", duckDBThreads())
	if _, err := db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("set duckdb threads: %w", err)
	}
	query = "SET memory_limit = " + quoteLiteral(duckDBMemoryLimit())
	if _, err := db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("set duckdb memory limit: %w", err)
	}
	return nil
}

func duckDBThreads() int {
	value := strings.TrimSpace(os.Getenv("DUCKDB_REPORT_THREADS"))
	if value != "" {
		threads, err := strconv.Atoi(value)
		if err == nil && threads >= 1 && threads <= maxDuckDBThreads {
			return threads
		}
	}
	return max(1, runtime.NumCPU()/2)
}

func duckDBMemoryLimit() string {
	value := strings.ToUpper(strings.TrimSpace(
		os.Getenv("DUCKDB_REPORT_MEMORY_LIMIT"),
	))
	if duckDBMemoryLimitPattern.MatchString(value) {
		return value
	}
	return defaultDuckDBMemoryLimit
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
	var err error
	destination, err = s.resolveFilePath(destination)
	if err != nil {
		return fmt.Errorf("resolve snapshot destination: %w", err)
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
	executable := query
	if s.baseDir != "" {
		executable = resolveSQLPaths(query, s.baseDir)
	}
	rows, err := s.db.QueryContext(ctx, executable)
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

func (s *Service) WriteTableRow(
	ctx context.Context,
	request model.TableWriteRequest,
) (model.TableWriteResult, error) {
	request.Operation = strings.ToLower(strings.TrimSpace(request.Operation))
	if request.Operation != model.TableWriteInsert && request.Operation != model.TableWriteUpdate {
		return model.TableWriteResult{}, fmt.Errorf("unsupported table write operation %q", request.Operation)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tableType, columns, err := s.tableColumnsLocked(ctx, request.Table)
	if err != nil {
		return model.TableWriteResult{}, err
	}
	if !strings.EqualFold(tableType, "BASE TABLE") && !strings.EqualFold(tableType, "TABLE") {
		return model.TableWriteResult{}, fmt.Errorf(
			"table %q is %s and cannot be modified through the row editor",
			request.Table.Name,
			tableType,
		)
	}
	readOnly, err := s.databaseReadOnlyLocked(ctx, request.Table.Catalog)
	if err != nil {
		return model.TableWriteResult{}, err
	}
	if readOnly {
		return model.TableWriteResult{}, fmt.Errorf("database %q is read-only", request.Table.Catalog)
	}

	statement, args, err := buildTableWriteStatement(request, columns)
	if err != nil {
		return model.TableWriteResult{}, err
	}
	result, err := s.db.ExecContext(ctx, statement, args...)
	if err != nil {
		return model.TableWriteResult{}, fmt.Errorf("execute table write: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return model.TableWriteResult{}, fmt.Errorf("read table write row count: %w", err)
	}
	return model.TableWriteResult{
		Table:        request.Table,
		Operation:    request.Operation,
		RowsAffected: rowsAffected,
		SQL:          statement,
	}, nil
}

func (s *Service) LoadTableEditorInfo(
	ctx context.Context,
	table model.TableRef,
) (model.TableEditorInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tableType, columns, err := s.tableColumnsLocked(ctx, table)
	if err != nil {
		return model.TableEditorInfo{}, err
	}
	readOnly, err := s.databaseReadOnlyLocked(ctx, table.Catalog)
	if err != nil {
		return model.TableEditorInfo{}, err
	}
	table.Type = tableType
	info := model.TableEditorInfo{
		Table:       table,
		Columns:     columns,
		Keys:        []model.TableKey{},
		KeyColumns:  []string{},
		Relations:   []model.TableRelation{},
		ColumnCount: int64(len(columns)),
		Writable: !readOnly &&
			(strings.EqualFold(tableType, "BASE TABLE") || strings.EqualFold(tableType, "TABLE")),
	}

	var indexCount int64
	err = s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(estimated_size, 0),
			COALESCE(column_count, 0),
			COALESCE(index_count, 0),
			COALESCE(comment, ''),
			COALESCE(sql, '')
		FROM duckdb_tables()
		WHERE database_name = ?
		  AND schema_name = ?
		  AND table_name = ?
	`, table.Catalog, table.Schema, table.Name).Scan(
		&info.EstimatedRows,
		&info.ColumnCount,
		&indexCount,
		&info.Comment,
		&info.SQL,
	)
	switch {
	case err == nil:
	case errors.Is(err, sql.ErrNoRows):
		if err := s.db.QueryRowContext(ctx, `
			SELECT
				COALESCE(column_count, 0),
				COALESCE(comment, ''),
				COALESCE(sql, '')
			FROM duckdb_views()
			WHERE database_name = ?
			  AND schema_name = ?
			  AND view_name = ?
		`, table.Catalog, table.Schema, table.Name).Scan(
			&info.ColumnCount,
			&info.Comment,
			&info.SQL,
		); err != nil {
			return model.TableEditorInfo{}, fmt.Errorf("read view metadata: %w", err)
		}
	default:
		return model.TableEditorInfo{}, fmt.Errorf("read table properties: %w", err)
	}
	info.IndexCount = indexCount

	info.Keys, err = s.tableKeysLocked(ctx, table)
	if err != nil {
		return model.TableEditorInfo{}, err
	}
	info.KeyType, info.KeyColumns = rowKeyFromKeys(info.Keys, columns)
	info.Relations, err = s.tableRelationsLocked(ctx, table)
	if err != nil {
		return model.TableEditorInfo{}, err
	}
	return info, nil
}

func (s *Service) PreviewTableStructure(
	ctx context.Context,
	request model.TableStructureRequest,
) (model.TableStructurePreview, error) {
	request.Operation = strings.ToLower(strings.TrimSpace(request.Operation))
	s.mu.Lock()
	defer s.mu.Unlock()

	statement, _, warnings, err := s.prepareTableStructureLocked(ctx, request)
	if err != nil {
		return model.TableStructurePreview{}, err
	}
	return model.TableStructurePreview{
		Operation: request.Operation,
		SQL:       statement,
		Warnings:  warnings,
	}, nil
}

func (s *Service) AlterTableStructure(
	ctx context.Context,
	request model.TableStructureRequest,
) (model.TableStructureResult, error) {
	request.Operation = strings.ToLower(strings.TrimSpace(request.Operation))
	s.mu.Lock()
	defer s.mu.Unlock()

	statement, resultTable, _, err := s.prepareTableStructureLocked(ctx, request)
	if err != nil {
		return model.TableStructureResult{}, err
	}
	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		return model.TableStructureResult{}, fmt.Errorf("alter table structure: %w", err)
	}
	return model.TableStructureResult{
		Table:     resultTable,
		Operation: request.Operation,
		SQL:       statement,
	}, nil
}

func (s *Service) prepareTableStructureLocked(
	ctx context.Context,
	request model.TableStructureRequest,
) (string, model.TableRef, []string, error) {
	tableType, columns, err := s.tableColumnsLocked(ctx, request.Table)
	if err != nil {
		return "", model.TableRef{}, nil, err
	}
	if !strings.EqualFold(tableType, "BASE TABLE") && !strings.EqualFold(tableType, "TABLE") {
		return "", model.TableRef{}, nil, fmt.Errorf(
			"table %q is %s and its structure cannot be altered here",
			request.Table.Name,
			tableType,
		)
	}
	readOnly, err := s.databaseReadOnlyLocked(ctx, request.Table.Catalog)
	if err != nil {
		return "", model.TableRef{}, nil, err
	}
	if readOnly {
		return "", model.TableRef{}, nil, fmt.Errorf("database %q is read-only", request.Table.Catalog)
	}
	keys, err := s.tableKeysLocked(ctx, request.Table)
	if err != nil {
		return "", model.TableRef{}, nil, err
	}
	return buildTableStructureStatement(request, columns, keys)
}

func buildTableStructureStatement(
	request model.TableStructureRequest,
	columns []model.ColumnRef,
	keys []model.TableKey,
) (string, model.TableRef, []string, error) {
	columnSet := make(map[string]model.ColumnRef, len(columns))
	for _, column := range columns {
		columnSet[column.Name] = column
	}
	requireColumn := func(name string) (model.ColumnRef, error) {
		column, ok := columnSet[name]
		if !ok {
			return model.ColumnRef{}, fmt.Errorf("column %q does not belong to table %q", name, request.Table.Name)
		}
		return column, nil
	}
	qualifiedTable := qualifiedTableName(request.Table)
	resultTable := request.Table
	warnings := []string{}
	requestedColumns := func() ([]string, error) {
		result := make([]string, 0, len(request.Columns))
		seen := make(map[string]struct{}, len(request.Columns))
		for _, value := range request.Columns {
			column := strings.TrimSpace(value)
			if _, err := requireColumn(column); err != nil {
				return nil, err
			}
			if _, duplicate := seen[column]; duplicate {
				return nil, fmt.Errorf("column %q is selected more than once", column)
			}
			seen[column] = struct{}{}
			result = append(result, quoteIdentifier(column))
		}
		if len(result) == 0 {
			return nil, errors.New("at least one key column is required")
		}
		return result, nil
	}

	switch request.Operation {
	case model.TableStructureRenameTable:
		newName := strings.TrimSpace(request.NewName)
		if err := validateNewIdentifier(newName, "table name"); err != nil {
			return "", model.TableRef{}, nil, err
		}
		if newName == request.Table.Name {
			return "", model.TableRef{}, nil, errors.New("new table name must be different")
		}
		resultTable.Name = newName
		return fmt.Sprintf(
				"ALTER TABLE %s RENAME TO %s",
				qualifiedTable,
				quoteIdentifier(newName),
			),
			resultTable,
			[]string{"依赖该表的视图不会自动更新。"},
			nil
	case model.TableStructureAddColumn:
		columnName := strings.TrimSpace(request.Column)
		if err := validateNewIdentifier(columnName, "column name"); err != nil {
			return "", model.TableRef{}, nil, err
		}
		if _, exists := columnSet[columnName]; exists {
			return "", model.TableRef{}, nil, fmt.Errorf("column %q already exists", columnName)
		}
		columnType := strings.TrimSpace(request.ColumnType)
		if !tableColumnTypePattern.MatchString(columnType) {
			return "", model.TableRef{}, nil, fmt.Errorf("unsupported column type %q", request.ColumnType)
		}
		return fmt.Sprintf(
				"ALTER TABLE %s ADD COLUMN %s %s",
				qualifiedTable,
				quoteIdentifier(columnName),
				columnType,
			),
			resultTable,
			[]string{"现有行会在新字段中得到 NULL。"},
			nil
	case model.TableStructureRenameColumn:
		columnName := strings.TrimSpace(request.Column)
		if _, err := requireColumn(columnName); err != nil {
			return "", model.TableRef{}, nil, err
		}
		newName := strings.TrimSpace(request.NewName)
		if err := validateNewIdentifier(newName, "column name"); err != nil {
			return "", model.TableRef{}, nil, err
		}
		if newName == columnName {
			return "", model.TableRef{}, nil, errors.New("new column name must be different")
		}
		if _, exists := columnSet[newName]; exists {
			return "", model.TableRef{}, nil, fmt.Errorf("column %q already exists", newName)
		}
		return fmt.Sprintf(
				"ALTER TABLE %s RENAME COLUMN %s TO %s",
				qualifiedTable,
				quoteIdentifier(columnName),
				quoteIdentifier(newName),
			),
			resultTable,
			[]string{"依赖旧字段名的视图或查询需要同步更新。"},
			nil
	case model.TableStructureAlterType:
		columnName := strings.TrimSpace(request.Column)
		if _, err := requireColumn(columnName); err != nil {
			return "", model.TableRef{}, nil, err
		}
		columnType := strings.TrimSpace(request.ColumnType)
		if !tableColumnTypePattern.MatchString(columnType) {
			return "", model.TableRef{}, nil, fmt.Errorf("unsupported column type %q", request.ColumnType)
		}
		return fmt.Sprintf(
				"ALTER TABLE %s ALTER COLUMN %s TYPE %s",
				qualifiedTable,
				quoteIdentifier(columnName),
				columnType,
			),
			resultTable,
			[]string{
				"现有值会尝试转换为新类型。",
				"如果字段参与索引或 CHECK 约束，DuckDB 可能拒绝该操作。",
			},
			nil
	case model.TableStructureDropColumn:
		columnName := strings.TrimSpace(request.Column)
		if _, err := requireColumn(columnName); err != nil {
			return "", model.TableRef{}, nil, err
		}
		if len(columns) <= 1 {
			return "", model.TableRef{}, nil, errors.New("cannot drop the last column")
		}
		return fmt.Sprintf(
				"ALTER TABLE %s DROP COLUMN %s",
				qualifiedTable,
				quoteIdentifier(columnName),
			),
			resultTable,
			[]string{
				"该字段的数据会被永久删除。",
				"依赖该字段的视图或约束可能导致操作失败。",
			},
			nil
	case model.TableStructureSetNotNull:
		columnName := strings.TrimSpace(request.Column)
		if _, err := requireColumn(columnName); err != nil {
			return "", model.TableRef{}, nil, err
		}
		return fmt.Sprintf(
				"ALTER TABLE %s ALTER COLUMN %s SET NOT NULL",
				qualifiedTable,
				quoteIdentifier(columnName),
			),
			resultTable,
			[]string{"如果现有数据包含 NULL，该操作会失败。"},
			nil
	case model.TableStructureDropNotNull:
		columnName := strings.TrimSpace(request.Column)
		if _, err := requireColumn(columnName); err != nil {
			return "", model.TableRef{}, nil, err
		}
		return fmt.Sprintf(
				"ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL",
				qualifiedTable,
				quoteIdentifier(columnName),
			),
			resultTable,
			warnings,
			nil
	case model.TableStructureDropDefault:
		columnName := strings.TrimSpace(request.Column)
		if _, err := requireColumn(columnName); err != nil {
			return "", model.TableRef{}, nil, err
		}
		return fmt.Sprintf(
				"ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT",
				qualifiedTable,
				quoteIdentifier(columnName),
			),
			resultTable,
			[]string{"已有数据不会改变；后续新增行将不再使用原默认值。"},
			nil
	case model.TableStructureAddPrimaryKey:
		for _, key := range keys {
			if key.Type == "PRIMARY KEY" {
				return "", model.TableRef{}, nil, errors.New("table already has a primary key")
			}
		}
		keyColumns, err := requestedColumns()
		if err != nil {
			return "", model.TableRef{}, nil, err
		}
		return fmt.Sprintf(
				"ALTER TABLE %s ADD PRIMARY KEY (%s)",
				qualifiedTable,
				strings.Join(keyColumns, ", "),
			),
			resultTable,
			[]string{
				"现有数据必须非空且在键字段组合上唯一，否则操作会失败。",
				"主键会创建 ART 索引，并禁止键字段包含 NULL。",
			},
			nil
	case model.TableStructureAddUniqueKey:
		keyColumns, err := requestedColumns()
		if err != nil {
			return "", model.TableRef{}, nil, err
		}
		requested := make(map[string]struct{}, len(request.Columns))
		for _, column := range request.Columns {
			requested[strings.TrimSpace(column)] = struct{}{}
		}
		for _, key := range keys {
			if key.Type != "UNIQUE" || len(key.Columns) != len(requested) {
				continue
			}
			same := true
			for _, column := range key.Columns {
				if _, ok := requested[column]; !ok {
					same = false
					break
				}
			}
			if same {
				return "", model.TableRef{}, nil, errors.New("an identical unique key already exists")
			}
		}
		return fmt.Sprintf(
				"ALTER TABLE %s ADD UNIQUE (%s)",
				qualifiedTable,
				strings.Join(keyColumns, ", "),
			),
			resultTable,
			[]string{
				"现有数据必须在键字段组合上唯一，否则操作会失败。",
				"唯一键会创建 ART 索引。",
			},
			nil
	default:
		return "", model.TableRef{}, nil, fmt.Errorf("unsupported table structure operation %q", request.Operation)
	}
}

func validateNewIdentifier(value, label string) error {
	if value == "" {
		return fmt.Errorf("%s is empty", label)
	}
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("%s contains an invalid character", label)
	}
	return nil
}

func (s *Service) databaseReadOnlyLocked(ctx context.Context, database string) (bool, error) {
	var readOnly bool
	if err := s.db.QueryRowContext(
		ctx,
		"SELECT readonly FROM duckdb_databases() WHERE database_name = ?",
		database,
	).Scan(&readOnly); err != nil {
		return false, fmt.Errorf("read database write mode: %w", err)
	}
	return readOnly, nil
}

func (s *Service) tableKeysLocked(
	ctx context.Context,
	table model.TableRef,
) ([]model.TableKey, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			k.constraint_name,
			k.constraint_type,
			u.column_name
		FROM duckdb_constraints() k,
		LATERAL unnest(k.constraint_column_names)
			WITH ORDINALITY AS u(column_name, ordinal)
		WHERE k.database_name = ?
		  AND k.schema_name = ?
		  AND k.table_name = ?
		  AND k.constraint_type IN ('PRIMARY KEY', 'UNIQUE')
		ORDER BY k.constraint_index, u.ordinal
	`, table.Catalog, table.Schema, table.Name)
	if err != nil {
		return nil, fmt.Errorf("read table keys: %w", err)
	}
	defer rows.Close()

	keys := make([]model.TableKey, 0)
	keyIndexes := make(map[string]int)
	for rows.Next() {
		var keyName, keyType string
		var column string
		if err := rows.Scan(&keyName, &keyType, &column); err != nil {
			return nil, fmt.Errorf("scan table key: %w", err)
		}
		index, exists := keyIndexes[keyName]
		if !exists {
			index = len(keys)
			keyIndexes[keyName] = index
			keys = append(keys, model.TableKey{
				Name:    keyName,
				Type:    keyType,
				Columns: []string{},
			})
		}
		keys[index].Columns = append(keys[index].Columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate table keys: %w", err)
	}
	return keys, nil
}

func rowKeyFromKeys(
	keys []model.TableKey,
	columns []model.ColumnRef,
) (string, []string) {
	columnMetadata := make(map[string]model.ColumnRef, len(columns))
	for _, column := range columns {
		columnMetadata[column.Name] = column
	}
	for _, key := range keys {
		if key.Type == "PRIMARY KEY" && keyColumnsExist(key.Columns, columnMetadata) {
			return key.Type, append([]string(nil), key.Columns...)
		}
	}
	for _, key := range keys {
		if key.Type != "UNIQUE" || !keyColumnsExist(key.Columns, columnMetadata) {
			continue
		}
		nullable := false
		for _, column := range key.Columns {
			if columnMetadata[column].Nullable {
				nullable = true
				break
			}
		}
		if !nullable {
			return key.Type, append([]string(nil), key.Columns...)
		}
	}
	return "", []string{}
}

func keyColumnsExist(
	columns []string,
	metadata map[string]model.ColumnRef,
) bool {
	for _, column := range columns {
		if _, ok := metadata[column]; !ok {
			return false
		}
	}
	return true
}

func (s *Service) tableRelationsLocked(
	ctx context.Context,
	table model.TableRef,
) ([]model.TableRelation, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			tc.table_catalog,
			tc.table_schema,
			tc.table_name,
			tc.constraint_name,
			kcu.column_name,
			rkcu.table_catalog,
			rkcu.table_schema,
			rkcu.table_name,
			rkcu.column_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
			ON kcu.constraint_catalog = tc.constraint_catalog
			AND kcu.constraint_schema = tc.constraint_schema
			AND kcu.constraint_name = tc.constraint_name
		JOIN information_schema.referential_constraints rc
			ON rc.constraint_catalog = tc.constraint_catalog
			AND rc.constraint_schema = tc.constraint_schema
			AND rc.constraint_name = tc.constraint_name
		JOIN information_schema.key_column_usage rkcu
			ON rkcu.constraint_catalog = rc.unique_constraint_catalog
			AND rkcu.constraint_schema = rc.unique_constraint_schema
			AND rkcu.constraint_name = rc.unique_constraint_name
			AND rkcu.ordinal_position = kcu.position_in_unique_constraint
		WHERE tc.constraint_type = 'FOREIGN KEY'
		  AND (
			(tc.table_catalog = ? AND tc.table_schema = ? AND tc.table_name = ?)
			OR
			(rkcu.table_catalog = ? AND rkcu.table_schema = ? AND rkcu.table_name = ?)
		  )
		ORDER BY tc.table_catalog, tc.table_schema, tc.table_name, tc.constraint_name, kcu.ordinal_position
	`,
		table.Catalog,
		table.Schema,
		table.Name,
		table.Catalog,
		table.Schema,
		table.Name,
	)
	if err != nil {
		return nil, fmt.Errorf("list table relations: %w", err)
	}
	defer rows.Close()

	relations := make([]model.TableRelation, 0)
	relationIndexes := make(map[string]int)
	for rows.Next() {
		var (
			source       model.TableRef
			target       model.TableRef
			constraint   string
			sourceColumn string
			targetColumn string
		)
		if err := rows.Scan(
			&source.Catalog,
			&source.Schema,
			&source.Name,
			&constraint,
			&sourceColumn,
			&target.Catalog,
			&target.Schema,
			&target.Name,
			&targetColumn,
		); err != nil {
			return nil, fmt.Errorf("scan table relation: %w", err)
		}
		key := source.Catalog + "\x00" + source.Schema + "\x00" + source.Name + "\x00" + constraint
		index, ok := relationIndexes[key]
		if !ok {
			direction := "outgoing"
			if sameTable(source, table) {
				direction = "outgoing"
			} else {
				direction = "incoming"
			}
			index = len(relations)
			relationIndexes[key] = index
			relations = append(relations, model.TableRelation{
				Name:          constraint,
				Direction:     direction,
				Source:        source,
				SourceColumns: []string{},
				Target:        target,
				TargetColumns: []string{},
			})
		}
		relations[index].SourceColumns = append(relations[index].SourceColumns, sourceColumn)
		relations[index].TargetColumns = append(relations[index].TargetColumns, targetColumn)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate table relations: %w", err)
	}
	return relations, nil
}

func sameTable(left, right model.TableRef) bool {
	return left.Catalog == right.Catalog &&
		left.Schema == right.Schema &&
		left.Name == right.Name
}

func (s *Service) tableColumnsLocked(
	ctx context.Context,
	table model.TableRef,
) (string, []model.ColumnRef, error) {
	var tableType string
	err := s.db.QueryRowContext(ctx, `
		SELECT table_type
		FROM information_schema.tables
		WHERE table_catalog = ?
		  AND table_schema = ?
		  AND table_name = ?
	`, table.Catalog, table.Schema, table.Name).Scan(&tableType)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, fmt.Errorf(
			"table %q.%q.%q does not exist",
			table.Catalog,
			table.Schema,
			table.Name,
		)
	}
	if err != nil {
		return "", nil, fmt.Errorf("read table metadata: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			column_name,
			COALESCE(data_type, ''),
			COALESCE(is_nullable = 'YES', false),
			COALESCE(column_default, '')
		FROM information_schema.columns
		WHERE table_catalog = ?
		  AND table_schema = ?
		  AND table_name = ?
		ORDER BY ordinal_position
	`, table.Catalog, table.Schema, table.Name)
	if err != nil {
		return "", nil, fmt.Errorf("list table columns: %w", err)
	}
	defer rows.Close()

	columns := make([]model.ColumnRef, 0)
	for rows.Next() {
		var column model.ColumnRef
		if err := rows.Scan(
			&column.Name,
			&column.Type,
			&column.Nullable,
			&column.DefaultValue,
		); err != nil {
			return "", nil, fmt.Errorf("scan table column: %w", err)
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return "", nil, fmt.Errorf("iterate table columns: %w", err)
	}
	if len(columns) == 0 {
		return "", nil, fmt.Errorf("table %q has no columns", table.Name)
	}
	return tableType, columns, nil
}

func buildTableWriteStatement(
	request model.TableWriteRequest,
	columns []model.ColumnRef,
) (string, []any, error) {
	columnSet := make(map[string]model.ColumnRef, len(columns))
	for _, column := range columns {
		columnSet[column.Name] = column
	}
	qualifiedTable := qualifiedTableName(request.Table)

	switch request.Operation {
	case model.TableWriteInsert:
		if len(request.Values) == 0 {
			return "INSERT INTO " + qualifiedTable + " DEFAULT VALUES", nil, nil
		}
		names := make([]string, 0, len(request.Values))
		placeholders := make([]string, 0, len(request.Values))
		args := make([]any, 0, len(request.Values))
		seen := make(map[string]struct{}, len(request.Values))
		for _, value := range request.Values {
			if _, ok := columnSet[value.Column]; !ok {
				return "", nil, fmt.Errorf("column %q does not belong to table %q", value.Column, request.Table.Name)
			}
			if _, duplicate := seen[value.Column]; duplicate {
				return "", nil, fmt.Errorf("column %q is specified more than once", value.Column)
			}
			seen[value.Column] = struct{}{}
			names = append(names, quoteIdentifier(value.Column))
			placeholders = append(placeholders, "?")
			if value.Null {
				args = append(args, nil)
			} else {
				args = append(args, value.Value)
			}
		}
		return fmt.Sprintf(
			"INSERT INTO %s (%s) VALUES (%s)",
			qualifiedTable,
			strings.Join(names, ", "),
			strings.Join(placeholders, ", "),
		), args, nil
	case model.TableWriteUpdate:
		if len(request.Values) == 0 {
			return "", nil, errors.New("update requires at least one value")
		}
		if len(request.Filters) == 0 {
			return "", nil, errors.New("update requires at least one filter")
		}
		assignments := make([]string, 0, len(request.Values))
		args := make([]any, 0, len(request.Values)+len(request.Filters))
		seen := make(map[string]struct{}, len(request.Values))
		for _, value := range request.Values {
			if _, ok := columnSet[value.Column]; !ok {
				return "", nil, fmt.Errorf("column %q does not belong to table %q", value.Column, request.Table.Name)
			}
			if _, duplicate := seen[value.Column]; duplicate {
				return "", nil, fmt.Errorf("column %q is specified more than once", value.Column)
			}
			seen[value.Column] = struct{}{}
			assignments = append(assignments, quoteIdentifier(value.Column)+" = ?")
			if value.Null {
				args = append(args, nil)
			} else {
				args = append(args, value.Value)
			}
		}
		conditions := make([]string, 0, len(request.Filters))
		for _, filter := range request.Filters {
			if _, ok := columnSet[filter.Column]; !ok {
				return "", nil, fmt.Errorf("filter column %q does not belong to table %q", filter.Column, request.Table.Name)
			}
			operator, ok := tableFilterOperators[strings.ToLower(strings.TrimSpace(filter.Operator))]
			if !ok {
				return "", nil, fmt.Errorf("unsupported filter operator %q", filter.Operator)
			}
			condition := quoteIdentifier(filter.Column) + " " + operator
			if operator != "IS NULL" && operator != "IS NOT NULL" {
				condition += " ?"
				args = append(args, filter.Value)
			}
			conditions = append(conditions, condition)
		}
		return fmt.Sprintf(
			"UPDATE %s SET %s WHERE %s",
			qualifiedTable,
			strings.Join(assignments, ", "),
			strings.Join(conditions, " AND "),
		), args, nil
	default:
		return "", nil, fmt.Errorf("unsupported table write operation %q", request.Operation)
	}
}

var tableFilterOperators = map[string]string{
	"eq":          "=",
	"ne":          "<>",
	"lt":          "<",
	"lte":         "<=",
	"gt":          ">",
	"gte":         ">=",
	"like":        "LIKE",
	"ilike":       "ILIKE",
	"is_null":     "IS NULL",
	"is_not_null": "IS NOT NULL",
}

func qualifiedTableName(table model.TableRef) string {
	return strings.Join([]string{
		quoteIdentifier(table.Catalog),
		quoteIdentifier(table.Schema),
		quoteIdentifier(table.Name),
	}, ".")
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

	executable := query
	if s.baseDir != "" {
		executable = resolveSQLPaths(query, s.baseDir)
	}
	rows, err := s.db.QueryContext(ctx, "EXPLAIN "+executable)
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
	path, err := s.resolveFilePath(request.Path)
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

func (s *Service) resolveFilePath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("file path is empty")
	}
	if s.baseDir != "" && !filepath.IsAbs(value) && !hasURIScheme(value) {
		value = filepath.Join(s.baseDir, filepath.FromSlash(value))
	}
	return filepath.Abs(value)
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

func (s *Service) ListDatabases(ctx context.Context) ([]model.DatabaseRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var currentDatabase string
	if err := s.db.QueryRowContext(ctx, "SELECT current_database()").Scan(&currentDatabase); err != nil {
		return nil, fmt.Errorf("read current database: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			database_name,
			COALESCE(path, ''),
			COALESCE(type, ''),
			COALESCE(readonly, false)
		FROM duckdb_databases()
		WHERE database_name NOT IN ('system', 'temp')
		ORDER BY database_name
	`)
	if err != nil {
		return nil, fmt.Errorf("list duckdb databases: %w", err)
	}
	defer rows.Close()

	result := []model.DatabaseRef{}
	for rows.Next() {
		var database model.DatabaseRef
		if err := rows.Scan(
			&database.Name,
			&database.Path,
			&database.Type,
			&database.ReadOnly,
		); err != nil {
			return nil, fmt.Errorf("scan duckdb database: %w", err)
		}
		database.IsCurrent = database.Name == currentDatabase
		result = append(result, database)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate duckdb databases: %w", err)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].IsCurrent != result[j].IsCurrent {
			return result[i].IsCurrent
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func (s *Service) SQLCompletions(ctx context.Context) (model.SQLCompletionCatalog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			c.table_catalog,
			c.table_schema,
			c.table_name,
			COALESCE(t.table_type, ''),
			c.column_name,
			COALESCE(c.data_type, ''),
			COALESCE(c.is_nullable = 'YES', false),
			COALESCE(c.column_default, '')
		FROM information_schema.columns c
		LEFT JOIN information_schema.tables t
			ON t.table_catalog = c.table_catalog
			AND t.table_schema = c.table_schema
			AND t.table_name = c.table_name
		WHERE c.table_schema NOT IN ('information_schema', 'pg_catalog')
		ORDER BY c.table_catalog, c.table_schema, c.table_name, c.ordinal_position
	`)
	if err != nil {
		return model.SQLCompletionCatalog{}, fmt.Errorf("list sql completion columns: %w", err)
	}
	defer rows.Close()

	catalog := model.SQLCompletionCatalog{Tables: []model.SQLCompletionTable{}}
	tableIndexes := make(map[string]int)
	for rows.Next() {
		var (
			tableCatalog string
			tableSchema  string
			tableName    string
			tableType    string
			columnName   string
			columnType   string
			nullable     bool
			defaultValue string
		)
		if err := rows.Scan(
			&tableCatalog,
			&tableSchema,
			&tableName,
			&tableType,
			&columnName,
			&columnType,
			&nullable,
			&defaultValue,
		); err != nil {
			return model.SQLCompletionCatalog{}, fmt.Errorf("scan sql completion column: %w", err)
		}
		key := tableCatalog + "\x00" + tableSchema + "\x00" + tableName
		tableIndex, exists := tableIndexes[key]
		if !exists {
			tableIndex = len(catalog.Tables)
			tableIndexes[key] = tableIndex
			catalog.Tables = append(catalog.Tables, model.SQLCompletionTable{
				Catalog: tableCatalog,
				Schema:  tableSchema,
				Name:    tableName,
				Type:    tableType,
				Columns: []model.ColumnRef{},
			})
		}
		catalog.Tables[tableIndex].Columns = append(
			catalog.Tables[tableIndex].Columns,
			model.ColumnRef{
				Name:         columnName,
				Type:         columnType,
				Nullable:     nullable,
				DefaultValue: defaultValue,
			},
		)
	}
	if err := rows.Err(); err != nil {
		return model.SQLCompletionCatalog{}, fmt.Errorf("iterate sql completion columns: %w", err)
	}
	return catalog, nil
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
