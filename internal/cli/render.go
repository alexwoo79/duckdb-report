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
	"github.com/alexwoo79/duckdb-report/internal/template"
	"github.com/spf13/cobra"
)

func newRenderCommand(exampleFS fs.FS, options *rootOptions) *cobra.Command {
	var templatePath string
	var format string
	var query string
	var queryFile string
	var saveAs string
	var parameterValues []string
	var namedQueryValues []string
	var namedQueryFileValues []string

	command := &cobra.Command{
		Use:   "render",
		Short: "Render a QMD template to HTML or PDF",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return &usageError{err: err}
			}
			if strings.TrimSpace(templatePath) == "" {
				return usagef("--template is required")
			}
			format = strings.ToLower(strings.TrimSpace(format))
			if format != "html" && format != "pdf" {
				return usagef("--format must be html or pdf")
			}
			if strings.TrimSpace(query) != "" && strings.TrimSpace(queryFile) != "" {
				return usagef("--query and --query-file are mutually exclusive")
			}
			return nil
		},
		RunE: func(command *cobra.Command, _ []string) error {
			params, err := parseKeyValues(parameterValues)
			if err != nil {
				return err
			}
			namedQueries, err := parseKeyValues(namedQueryValues)
			if err != nil {
				return err
			}
			namedQueryFiles, err := parseKeyValues(namedQueryFileValues)
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(command.Context(), 8*time.Minute)
			defer cancel()
			return useRuntime(ctx, exampleFS, options, func(runtime *cliRuntime) error {
				template, err := runtime.store.ResolveWorkspace(templatePath)
				if err != nil {
					return err
				}
				content, err := os.ReadFile(template)
				if err != nil {
					return fmt.Errorf("read qmd template: %w", err)
				}

				resolvedQuery := query
				if strings.TrimSpace(queryFile) != "" {
					resolvedQuery, err = loadQueryFile(runtime.store, queryFile, params)
					if err != nil {
						return err
					}
				}
				if resolvedQuery != "" {
					params["query"] = resolvedQuery
				}
				for name, value := range namedQueries {
					params["query_"+name] = value
				}
				for name, value := range namedQueryFiles {
					loaded, err := loadQueryFile(runtime.store, value, params)
					if err != nil {
						return fmt.Errorf("load named query %s: %w", name, err)
					}
					params["query_"+name] = loaded
				}

				name := strings.TrimSpace(saveAs)
				if name == "" {
					name = strings.TrimSuffix(filepath.Base(template), filepath.Ext(template))
				}
				result, err := runtime.renderer.Render(ctx, model.RenderRequest{
					Content:      string(content),
					Format:       format,
					Query:        resolvedQuery,
					Params:       params,
					SaveAs:       name,
					TemplatePath: runtime.store.Relative(template),
				})
				if err != nil {
					return err
				}
				return writeResult(command.OutOrStdout(), options.json, result, func() error {
					fmt.Fprintf(command.OutOrStdout(), "run: %s\n", result.RunID)
					fmt.Fprintf(command.OutOrStdout(), "output: %s\n", result.OutputPath)
					fmt.Fprintf(command.OutOrStdout(), "preview: %s\n", result.PreviewPath)
					return nil
				})
			})
		},
	}
	command.Flags().StringVar(&templatePath, "template", "", "QMD template path")
	command.Flags().StringVar(&format, "format", "html", "output format: html or pdf")
	command.Flags().StringVar(&query, "query", "", "primary {{table}} query")
	command.Flags().StringVar(&queryFile, "query-file", "", "file containing the primary {{table}} query")
	command.Flags().StringVar(&saveAs, "name", "", "report/run name")
	command.Flags().StringArrayVar(&parameterValues, "param", nil, "template parameter in key=value form (repeatable)")
	command.Flags().StringArrayVar(&namedQueryValues, "named-query", nil, "named query in name=SQL form (repeatable)")
	command.Flags().StringArrayVar(&namedQueryFileValues, "named-query-file", nil, "named query file in name=path form (repeatable)")
	return command
}

func loadQueryFile(store interface {
	ResolveWorkspace(string) (string, error)
}, value string, params map[string]string) (string, error) {
	path, err := store.ResolveWorkspace(value)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read query file: %w", err)
	}
	resolved, err := template.Resolve(string(data), params)
	if err != nil {
		return "", err
	}
	return database.ExpandScript(resolved, filepath.Dir(path))
}
