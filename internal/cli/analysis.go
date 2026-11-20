package cli

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alexwoo79/duckdb-report/internal/database"
	"github.com/alexwoo79/duckdb-report/internal/model"
	"github.com/spf13/cobra"
)

type statusResult struct {
	Workspace   string            `json:"workspace"`
	Database    string            `json:"database"`
	Tables      []model.TableRef  `json:"tables"`
	Sources     []model.Source    `json:"sources"`
	Environment model.Environment `json:"environment"`
}

func newStatusCommand(exampleFS fs.FS, options *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show workspace, runtime, table, and source status",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return &usageError{err: err}
			}
			return nil
		},
		RunE: func(command *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(command.Context(), 30*time.Second)
			defer cancel()
			return useRuntime(ctx, exampleFS, options, func(runtime *cliRuntime) error {
				tables, err := runtime.database.ListTables(ctx)
				if err != nil {
					return err
				}
				if tables == nil {
					tables = []model.TableRef{}
				}
				sources, err := runtime.store.LoadSources()
				if err != nil {
					return err
				}
				result := statusResult{
					Workspace:   runtime.store.Root,
					Database:    runtime.store.Database,
					Tables:      tables,
					Sources:     sources,
					Environment: runtime.renderer.Environments(ctx),
				}
				return writeResult(command.OutOrStdout(), options.json, result, func() error {
					writer := command.OutOrStdout()
					fmt.Fprintf(writer, "workspace: %s\n", result.Workspace)
					fmt.Fprintf(writer, "database:  %s\n", result.Database)
					fmt.Fprintf(writer, "duckdb:    %s\n", result.Environment.DuckDBVersion)
					fmt.Fprintf(writer, "quarto:    %s\n", fallback(result.Environment.QuartoVersion, "not found"))
					fmt.Fprintf(writer, "r:         %s\n", fallback(result.Environment.RVersion, "not found"))
					fmt.Fprintf(writer, "python:    %s\n", fallback(result.Environment.PythonVersion, "not found"))
					fmt.Fprintf(writer, "jupyter:   %t\n", result.Environment.JupyterAvailable)
					fmt.Fprintf(writer, "pdf:       %t\n", result.Environment.PDFAvailable)
					fmt.Fprintf(writer, "tables:    %d\n", len(result.Tables))
					fmt.Fprintf(writer, "sources:   %d\n", len(result.Sources))
					return nil
				})
			})
		},
	}
}

func newQueryCommand(exampleFS fs.FS, options *rootOptions) *cobra.Command {
	var file string
	var query string
	var basePath string
	var limit int
	command := &cobra.Command{
		Use:   "query",
		Short: "Run SQL against the workspace database",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return &usageError{err: err}
			}
			if (strings.TrimSpace(file) == "") == (strings.TrimSpace(query) == "") {
				return usagef("exactly one of --file or --sql is required")
			}
			if limit <= 0 {
				return usagef("--limit must be greater than zero")
			}
			return nil
		},
		RunE: func(command *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(command.Context(), 3*time.Minute)
			defer cancel()
			return useRuntime(ctx, exampleFS, options, func(runtime *cliRuntime) error {
				sql := query
				baseDir := runtime.store.Root
				if strings.TrimSpace(file) != "" {
					path, err := resolveReadableFile(runtime.store, file)
					if err != nil {
						return err
					}
					data, err := os.ReadFile(path)
					if err != nil {
						return fmt.Errorf("read sql file: %w", err)
					}
					sql = string(data)
					baseDir = filepath.Dir(path)
				}
				if strings.TrimSpace(basePath) != "" {
					path, err := resolveReadableFile(runtime.store, basePath)
					if err != nil {
						return err
					}
					baseDir = filepath.Dir(path)
				}
				expanded, err := database.ExpandScript(sql, baseDir)
				if err != nil {
					return err
				}
				result, err := runtime.database.Query(ctx, expanded, limit)
				if err != nil {
					return err
				}
				view := queryResultView{
					Columns: result.Columns, Rows: result.Rows,
					RowCount: result.RowCount, Truncated: result.Truncated,
				}
				return writeResult(command.OutOrStdout(), options.json, result, func() error {
					return writeQueryText(command.OutOrStdout(), view)
				})
			})
		},
	}
	command.Flags().StringVar(&file, "file", "", "SQL file path (workspace-relative or absolute)")
	command.Flags().StringVar(&query, "sql", "", "inline SQL to execute")
	command.Flags().StringVar(&basePath, "base", "", "file used to resolve relative .read paths")
	command.Flags().IntVar(&limit, "limit", 500, "maximum rows returned to stdout")
	return command
}

func newImportCommand(exampleFS fs.FS, options *rootOptions) *cobra.Command {
	var file string
	var kind string
	var table string
	var sheet string
	command := &cobra.Command{
		Use:   "import",
		Short: "Import a data file into the workspace database",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return &usageError{err: err}
			}
			if strings.TrimSpace(file) == "" {
				return usagef("--file is required")
			}
			return nil
		},
		RunE: func(command *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(command.Context(), 5*time.Minute)
			defer cancel()
			return useRuntime(ctx, exampleFS, options, func(runtime *cliRuntime) error {
				path, err := resolveReadableFile(runtime.store, file)
				if err != nil {
					return err
				}
				result, err := runtime.database.Import(ctx, model.ImportRequest{
					Path: path, Kind: kind, Table: table, Sheet: sheet,
				})
				if err != nil {
					return err
				}
				if err := runtime.store.SaveSource(result.Source); err != nil {
					return err
				}
				return writeResult(command.OutOrStdout(), options.json, result, func() error {
					fmt.Fprintf(command.OutOrStdout(), "imported %s as %s\n", result.Source.Path, result.Source.Table)
					for _, line := range result.Log {
						fmt.Fprintln(command.ErrOrStderr(), line)
					}
					return nil
				})
			})
		},
	}
	command.Flags().StringVar(&file, "file", "", "data file path")
	command.Flags().StringVar(&kind, "kind", "", "source kind: csv, json, parquet, xlsx, xlsm, duckdb, or db")
	command.Flags().StringVar(&table, "table", "", "table name or attached database alias")
	command.Flags().StringVar(&sheet, "sheet", "", "Excel sheet name")
	return command
}

func fallback(value, empty string) string {
	if strings.TrimSpace(value) == "" {
		return empty
	}
	return value
}
