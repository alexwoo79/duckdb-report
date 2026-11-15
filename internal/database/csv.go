package database

import (
	"encoding/csv"
	"fmt"
	"io"

	"github.com/alexwoo79/duckdb-report/internal/model"
)

func WriteQueryCSV(writer io.Writer, result model.QueryResult) error {
	if len(result.Columns) == 0 {
		return fmt.Errorf("query result has no columns")
	}
	if _, err := io.WriteString(writer, "\xEF\xBB\xBF"); err != nil {
		return fmt.Errorf("write csv bom: %w", err)
	}
	csvWriter := csv.NewWriter(writer)
	if err := csvWriter.Write(result.Columns); err != nil {
		return fmt.Errorf("write csv header: %w", err)
	}
	for rowIndex, row := range result.Rows {
		record := make([]string, len(result.Columns))
		for columnIndex := range result.Columns {
			if columnIndex >= len(row) || row[columnIndex] == nil {
				continue
			}
			record[columnIndex] = fmt.Sprint(row[columnIndex])
		}
		if err := csvWriter.Write(record); err != nil {
			return fmt.Errorf("write csv row %d: %w", rowIndex+1, err)
		}
	}
	csvWriter.Flush()
	if err := csvWriter.Error(); err != nil {
		return fmt.Errorf("flush csv: %w", err)
	}
	return nil
}
