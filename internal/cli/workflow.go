package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alexwoo79/duckdb-report/internal/model"
	"github.com/alexwoo79/duckdb-report/internal/workflow"
	"github.com/alexwoo79/duckdb-report/internal/workflowbundle"
	"github.com/alexwoo79/duckdb-report/internal/workspace"
	"github.com/spf13/cobra"
)

func newWorkflowCommand(exampleFS fs.FS, options *rootOptions) *cobra.Command {
	command := &cobra.Command{
		Use:   "workflow",
		Short: "Validate, run, import, or export workflows",
	}
	command.AddCommand(
		newWorkflowValidateCommand(exampleFS, options),
		newWorkflowRunCommand(exampleFS, options),
		newWorkflowImportCommand(exampleFS, options),
		newWorkflowExportCommand(exampleFS, options),
	)
	return command
}

func newWorkflowValidateCommand(exampleFS fs.FS, options *rootOptions) *cobra.Command {
	var file string
	command := &cobra.Command{
		Use:   "validate",
		Short: "Validate the current or specified workflow",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return &usageError{err: err}
			}
			return nil
		},
		RunE: func(command *cobra.Command, _ []string) error {
			root, err := workspaceFor(options)
			if err != nil {
				return err
			}
			store, err := workspace.New(root, exampleFS)
			if err != nil {
				return err
			}
			value, path, err := loadWorkflow(store, file)
			if err != nil {
				return err
			}
			result := struct {
				Valid  bool   `json:"valid"`
				Name   string `json:"name"`
				Steps  int    `json:"steps"`
				Source string `json:"source"`
			}{Valid: true, Name: value.Name, Steps: len(value.Steps), Source: path}
			return writeResult(command.OutOrStdout(), options.json, result, func() error {
				_, err := fmt.Fprintf(
					command.OutOrStdout(),
					"valid workflow %q with %d steps: %s\n",
					value.Name,
					len(value.Steps),
					path,
				)
				return err
			})
		},
	}
	command.Flags().StringVar(&file, "file", "", "workflow JSON path; defaults to workflows/current.json")
	return command
}

func newWorkflowRunCommand(exampleFS fs.FS, options *rootOptions) *cobra.Command {
	var file string
	var parameterValues []string
	command := &cobra.Command{
		Use:   "run",
		Short: "Run the current or specified workflow",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return &usageError{err: err}
			}
			return nil
		},
		RunE: func(command *cobra.Command, _ []string) error {
			overrides, err := parseKeyValues(parameterValues)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(command.Context(), 20*time.Minute)
			defer cancel()
			return useRuntime(ctx, exampleFS, options, func(runtime *cliRuntime) error {
				value, _, err := loadWorkflow(runtime.store, file)
				if err != nil {
					return err
				}
				params := make(map[string]string, len(value.Runtime)+len(overrides))
				for key, item := range value.Runtime {
					params[key] = item
				}
				for key, item := range overrides {
					params[key] = item
				}
				var sink func(model.StepEvent)
				if !options.quiet {
					sink = func(event model.StepEvent) {
						fmt.Fprintf(
							command.ErrOrStderr(),
							"[%s] %s: %s\n",
							event.Status,
							event.StepName,
							event.Message,
						)
					}
				}
				result, err := runtime.runner.Run(ctx, model.WorkflowRequest{
					Workflow: value,
					Params:   params,
				}, sink)
				if err != nil {
					return err
				}
				return writeResult(command.OutOrStdout(), options.json, result, func() error {
					fmt.Fprintf(command.OutOrStdout(), "run: %s\n", result.RunID)
					if result.PreviewPath != "" {
						fmt.Fprintf(command.OutOrStdout(), "preview: %s\n", result.PreviewPath)
					}
					if result.PackagePath != "" {
						fmt.Fprintf(command.OutOrStdout(), "package: %s\n", result.PackagePath)
					}
					return nil
				})
			})
		},
	}
	command.Flags().StringVar(&file, "file", "", "workflow JSON path; defaults to workflows/current.json")
	command.Flags().StringArrayVar(&parameterValues, "param", nil, "runtime parameter override in key=value form (repeatable)")
	return command
}

func newWorkflowImportCommand(exampleFS fs.FS, options *rootOptions) *cobra.Command {
	var file string
	command := &cobra.Command{
		Use:   "import",
		Short: "Import a workflow JSON or workflow ZIP as current",
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
			root, err := workspaceFor(options)
			if err != nil {
				return err
			}
			store, err := workspace.New(root, exampleFS)
			if err != nil {
				return err
			}
			path, err := resolveReadableFile(store, file)
			if err != nil {
				return err
			}
			var imported model.Workflow
			if strings.EqualFold(filepath.Ext(path), ".json") {
				imported, err = readWorkflowJSON(path)
				if err == nil {
					err = store.SaveWorkflow(imported)
				}
			} else {
				imported, err = workflowbundle.Import(path, store)
			}
			if err != nil {
				return err
			}
			return writeResult(command.OutOrStdout(), options.json, imported, func() error {
				_, err := fmt.Fprintf(command.OutOrStdout(), "imported workflow %q (%s)\n", imported.Name, imported.ID)
				return err
			})
		},
	}
	command.Flags().StringVar(&file, "file", "", "workflow .json or .zip path")
	return command
}

func newWorkflowExportCommand(exampleFS fs.FS, options *rootOptions) *cobra.Command {
	var file string
	var output string
	command := &cobra.Command{
		Use:   "export",
		Short: "Export the current or specified workflow as a portable ZIP",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return &usageError{err: err}
			}
			if strings.TrimSpace(output) == "" {
				return usagef("--output is required")
			}
			return nil
		},
		RunE: func(command *cobra.Command, _ []string) error {
			root, err := workspaceFor(options)
			if err != nil {
				return err
			}
			store, err := workspace.New(root, exampleFS)
			if err != nil {
				return err
			}
			value, _, err := loadWorkflow(store, file)
			if err != nil {
				return err
			}
			destination, err := filepath.Abs(output)
			if err != nil {
				return fmt.Errorf("resolve output path: %w", err)
			}
			if _, err := os.Stat(destination); err == nil {
				return fmt.Errorf("output already exists: %s", destination)
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("inspect output path: %w", err)
			}
			if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
				return fmt.Errorf("create output directory: %w", err)
			}
			fileHandle, err := os.Create(destination)
			if err != nil {
				return fmt.Errorf("create workflow package: %w", err)
			}
			exportErr := workflowbundle.Export(fileHandle, store, value)
			closeErr := fileHandle.Close()
			if exportErr != nil {
				_ = os.Remove(destination)
				return exportErr
			}
			if closeErr != nil {
				_ = os.Remove(destination)
				return fmt.Errorf("close workflow package: %w", closeErr)
			}
			payload := struct {
				Path string `json:"path"`
			}{Path: destination}
			return writeResult(command.OutOrStdout(), options.json, payload, func() error {
				_, err := fmt.Fprintln(command.OutOrStdout(), destination)
				return err
			})
		},
	}
	command.Flags().StringVar(&file, "file", "", "workflow JSON path; defaults to workflows/current.json")
	command.Flags().StringVar(&output, "output", "", "destination workflow ZIP path")
	return command
}

func loadWorkflow(store *workspace.Store, file string) (model.Workflow, string, error) {
	if strings.TrimSpace(file) == "" {
		value, err := store.LoadWorkflow()
		return value, store.WorkflowFile, err
	}
	path, err := resolveReadableFile(store, file)
	if err != nil {
		return model.Workflow{}, "", err
	}
	value, err := readWorkflowJSON(path)
	return value, path, err
}

func readWorkflowJSON(path string) (model.Workflow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Workflow{}, fmt.Errorf("read workflow json: %w", err)
	}
	var value model.Workflow
	if err := json.Unmarshal(data, &value); err != nil {
		return model.Workflow{}, fmt.Errorf("decode workflow json: %w", err)
	}
	if err := workflow.Validate(value); err != nil {
		return model.Workflow{}, err
	}
	return value, nil
}
