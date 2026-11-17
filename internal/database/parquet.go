package database

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strings"
	"time"

	"github.com/alexwoo79/duckdb-report/internal/model"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet"
	"github.com/apache/arrow-go/v18/parquet/compress"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
)

const maxInt64ExclusiveFloat = float64(1 << 63)

type parquetValueKind uint8

const (
	parquetKindNull parquetValueKind = iota
	parquetKindString
	parquetKindBool
	parquetKindInt64
	parquetKindFloat64
)

type parquetOutputWriter struct {
	io.Writer
}

func WriteQueryParquet(writer io.Writer, result model.QueryResult) error {
	if len(result.Columns) == 0 {
		return errors.New("query result has no columns")
	}

	kinds := inferParquetKinds(result)
	fields := make([]arrow.Field, len(result.Columns))
	for i, column := range result.Columns {
		name := strings.TrimSpace(column)
		if name == "" {
			name = fmt.Sprintf("column_%d", i+1)
		}
		fields[i] = arrow.Field{
			Name:     strings.ToValidUTF8(name, "\uFFFD"),
			Type:     kinds[i].arrowType(),
			Nullable: true,
		}
	}
	schema := arrow.NewSchema(fields, nil)
	arrays, err := buildParquetArrays(kinds, result.Rows)
	if err != nil {
		return err
	}
	defer releaseArrowArrays(arrays)

	record := array.NewRecordBatch(schema, arrays, int64(len(result.Rows)))
	defer record.Release()

	parquetWriter, err := pqarrow.NewFileWriter(
		schema,
		parquetOutputWriter{Writer: writer},
		parquet.NewWriterProperties(parquet.WithCompression(compress.Codecs.Snappy)),
		pqarrow.NewArrowWriterProperties(pqarrow.WithStoreSchema()),
	)
	if err != nil {
		return fmt.Errorf("create parquet writer: %w", err)
	}
	if len(result.Rows) > 0 {
		if err := parquetWriter.Write(record); err != nil {
			closeErr := parquetWriter.Close()
			return fmt.Errorf("write parquet data: %w", errors.Join(err, closeErr))
		}
	}
	if err := parquetWriter.Close(); err != nil {
		return fmt.Errorf("close parquet writer: %w", err)
	}
	return nil
}

func inferParquetKinds(result model.QueryResult) []parquetValueKind {
	kinds := make([]parquetValueKind, len(result.Columns))
	for columnIndex := range result.Columns {
		for _, row := range result.Rows {
			if columnIndex >= len(row) || isNilParquetValue(row[columnIndex]) {
				continue
			}
			kinds[columnIndex] = mergeParquetKinds(
				kinds[columnIndex],
				classifyParquetValue(row[columnIndex]),
			)
		}
		if kinds[columnIndex] == parquetKindNull {
			kinds[columnIndex] = parquetKindString
		}
	}
	return kinds
}

func classifyParquetValue(value any) parquetValueKind {
	switch typed := value.(type) {
	case bool:
		return parquetKindBool
	case string, []byte, time.Time:
		return parquetKindString
	case json.Number:
		if _, err := typed.Int64(); err == nil {
			return parquetKindInt64
		}
		if _, err := typed.Float64(); err == nil {
			return parquetKindFloat64
		}
		return parquetKindString
	case fmt.Stringer:
		return parquetKindString
	}

	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Bool:
		return parquetKindBool
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return parquetKindInt64
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if reflected.Uint() <= math.MaxInt64 {
			return parquetKindInt64
		}
		return parquetKindString
	case reflect.Float32, reflect.Float64:
		number := reflected.Float()
		if !math.IsNaN(number) &&
			!math.IsInf(number, 0) &&
			number == math.Trunc(number) &&
			number >= -maxInt64ExclusiveFloat &&
			number < maxInt64ExclusiveFloat {
			return parquetKindInt64
		}
		return parquetKindFloat64
	case reflect.String:
		return parquetKindString
	default:
		return parquetKindString
	}
}

func mergeParquetKinds(left, right parquetValueKind) parquetValueKind {
	if left == parquetKindNull {
		return right
	}
	if right == parquetKindNull || left == right {
		return left
	}
	if (left == parquetKindInt64 && right == parquetKindFloat64) ||
		(left == parquetKindFloat64 && right == parquetKindInt64) {
		return parquetKindFloat64
	}
	return parquetKindString
}

func (kind parquetValueKind) arrowType() arrow.DataType {
	switch kind {
	case parquetKindBool:
		return arrow.FixedWidthTypes.Boolean
	case parquetKindInt64:
		return arrow.PrimitiveTypes.Int64
	case parquetKindFloat64:
		return arrow.PrimitiveTypes.Float64
	default:
		return arrow.BinaryTypes.String
	}
}

func buildParquetArrays(kinds []parquetValueKind, rows [][]any) ([]arrow.Array, error) {
	arrays := make([]arrow.Array, 0, len(kinds))
	for columnIndex, kind := range kinds {
		values := make([]any, len(rows))
		for rowIndex, row := range rows {
			if columnIndex < len(row) {
				values[rowIndex] = row[columnIndex]
			}
		}

		var (
			column arrow.Array
			err    error
		)
		switch kind {
		case parquetKindBool:
			column, err = buildParquetBoolColumn(values)
		case parquetKindInt64:
			column, err = buildParquetInt64Column(values)
		case parquetKindFloat64:
			column, err = buildParquetFloat64Column(values)
		default:
			column = buildParquetStringColumn(values)
		}
		if err != nil {
			releaseArrowArrays(arrays)
			return nil, fmt.Errorf("build parquet column %d: %w", columnIndex+1, err)
		}
		arrays = append(arrays, column)
	}
	return arrays, nil
}

func buildParquetStringColumn(values []any) arrow.Array {
	builder := array.NewStringBuilder(memory.DefaultAllocator)
	defer builder.Release()
	for _, value := range values {
		if isNilParquetValue(value) {
			builder.AppendNull()
			continue
		}
		builder.Append(strings.ToValidUTF8(parquetString(value), "\uFFFD"))
	}
	return builder.NewStringArray()
}

func buildParquetBoolColumn(values []any) (arrow.Array, error) {
	builder := array.NewBooleanBuilder(memory.DefaultAllocator)
	defer builder.Release()
	for rowIndex, value := range values {
		if isNilParquetValue(value) {
			builder.AppendNull()
			continue
		}
		converted, ok := parquetBool(value)
		if !ok {
			return nil, fmt.Errorf("row %d is not a boolean", rowIndex+1)
		}
		builder.Append(converted)
	}
	return builder.NewBooleanArray(), nil
}

func buildParquetInt64Column(values []any) (arrow.Array, error) {
	builder := array.NewInt64Builder(memory.DefaultAllocator)
	defer builder.Release()
	for rowIndex, value := range values {
		if isNilParquetValue(value) {
			builder.AppendNull()
			continue
		}
		converted, ok := parquetInt64(value)
		if !ok {
			return nil, fmt.Errorf("row %d is not an int64 value", rowIndex+1)
		}
		builder.Append(converted)
	}
	return builder.NewInt64Array(), nil
}

func buildParquetFloat64Column(values []any) (arrow.Array, error) {
	builder := array.NewFloat64Builder(memory.DefaultAllocator)
	defer builder.Release()
	for rowIndex, value := range values {
		if isNilParquetValue(value) {
			builder.AppendNull()
			continue
		}
		converted, ok := parquetFloat64(value)
		if !ok {
			return nil, fmt.Errorf("row %d is not a numeric value", rowIndex+1)
		}
		builder.Append(converted)
	}
	return builder.NewFloat64Array(), nil
}

func parquetString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	case time.Time:
		return typed.Format(time.RFC3339)
	case json.Number:
		return typed.String()
	case fmt.Stringer:
		return typed.String()
	}

	reflected := reflect.ValueOf(value)
	if reflected.Kind() == reflect.String {
		return reflected.String()
	}
	encoded, err := json.Marshal(value)
	if err == nil {
		return string(encoded)
	}
	return fmt.Sprint(value)
}

func parquetBool(value any) (bool, bool) {
	if typed, ok := value.(bool); ok {
		return typed, true
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() == reflect.Bool {
		return reflected.Bool(), true
	}
	return false, false
}

func parquetInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case json.Number:
		number, err := typed.Int64()
		return number, err == nil
	case int:
		return int64(typed), true
	case int8:
		return int64(typed), true
	case int16:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case uint:
		if uint64(typed) > math.MaxInt64 {
			return 0, false
		}
		return int64(typed), true
	case uint8:
		return int64(typed), true
	case uint16:
		return int64(typed), true
	case uint32:
		return int64(typed), true
	case uint64:
		if typed > math.MaxInt64 {
			return 0, false
		}
		return int64(typed), true
	case float32:
		return floatToParquetInt64(float64(typed))
	case float64:
		return floatToParquetInt64(typed)
	}

	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return reflected.Int(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		number := reflected.Uint()
		if number > math.MaxInt64 {
			return 0, false
		}
		return int64(number), true
	case reflect.Float32, reflect.Float64:
		return floatToParquetInt64(reflected.Float())
	default:
		return 0, false
	}
}

func floatToParquetInt64(value float64) (int64, bool) {
	if math.IsNaN(value) ||
		math.IsInf(value, 0) ||
		value != math.Trunc(value) ||
		value < -maxInt64ExclusiveFloat ||
		value >= maxInt64ExclusiveFloat {
		return 0, false
	}
	return int64(value), true
}

func parquetFloat64(value any) (float64, bool) {
	switch typed := value.(type) {
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case float32:
		return float64(typed), true
	case float64:
		return typed, true
	}

	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(reflected.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(reflected.Uint()), true
	case reflect.Float32, reflect.Float64:
		return reflected.Float(), true
	default:
		return 0, false
	}
}

func isNilParquetValue(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func releaseArrowArrays(arrays []arrow.Array) {
	for _, column := range arrays {
		if column != nil {
			column.Release()
		}
	}
}
