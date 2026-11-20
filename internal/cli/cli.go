package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/alexwoo79/duckdb-report/internal/database"
	"github.com/alexwoo79/duckdb-report/internal/report"
	"github.com/alexwoo79/duckdb-report/internal/runner"
	"github.com/alexwoo79/duckdb-report/internal/settings"
	appversion "github.com/alexwoo79/duckdb-report/internal/version"
	"github.com/alexwoo79/duckdb-report/internal/workspace"
	"github.com/spf13/cobra"
)

type rootOptions struct {
	workspace string
	json      bool
	quiet     bool
}

type usageError struct {
	err error
}

func (e *usageError) Error() string {
	return e.err.Error()
}

func (e *usageError) Unwrap() error {
	return e.err
}

func usagef(format string, args ...any) error {
	return &usageError{err: fmt.Errorf(format, args...)}
}

func execute(
	ctx context.Context,
	args []string,
	exampleFS fs.FS,
	stdout io.Writer,
	stderr io.Writer,
) int {
	command := newRootCommand(exampleFS)
	command.SetArgs(args)
	command.SetOut(stdout)
	command.SetErr(stderr)
	err := command.ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	fmt.Fprintf(stderr, "error: %v\n", err)
	var typed *usageError
	if errors.As(err, &typed) ||
		strings.Contains(err.Error(), "unknown command") ||
		strings.Contains(err.Error(), "unknown flag") {
		return 2
	}
	return 1
}

// Execute runs the headless command tree and returns a process exit code.
func Execute(
	ctx context.Context,
	args []string,
	exampleFS fs.FS,
	stdout io.Writer,
	stderr io.Writer,
) int {
	return execute(ctx, args, exampleFS, stdout, stderr)
}

func newRootCommand(exampleFS fs.FS) *cobra.Command {
	options := &rootOptions{}
	command := &cobra.Command{
		Use:           "duckdb-report",
		Short:         "Automate DuckDB Report Studio workspaces, analysis, and reports",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       appversion.Current,
	}
	command.SetVersionTemplate("duckdb-report {{.Version}}\n")
	command.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &usageError{err: err}
	})
	command.PersistentFlags().StringVar(
		&options.workspace,
		"workspace",
		"",
		"workspace directory (defaults to DUCKDB_REPORT_HOME or the saved workspace)",
	)
	command.PersistentFlags().BoolVar(&options.json, "json", false, "write machine-readable JSON to stdout")
	command.PersistentFlags().BoolVar(&options.quiet, "quiet", false, "suppress workflow progress messages on stderr")

	command.AddCommand(
		newStatusCommand(exampleFS, options),
		newQueryCommand(exampleFS, options),
		newImportCommand(exampleFS, options),
		newRenderCommand(exampleFS, options),
		newDoctorCommand(exampleFS, options),
		newWorkflowCommand(exampleFS, options),
		newVersionCommand(options),
	)
	return command
}

type cliRuntime struct {
	store    *workspace.Store
	database *database.Service
	renderer *report.Renderer
	runner   *runner.Runner
}

func openRuntime(ctx context.Context, exampleFS fs.FS, root string) (*cliRuntime, error) {
	var store *workspace.Store
	var err error
	if isDefaultWorkspace(root) {
		store, err = workspace.New(root, exampleFS)
	} else {
		store, err = workspace.Open(root, exampleFS)
	}
	if err != nil {
		return nil, err
	}
	db, err := database.OpenWithBaseDir(ctx, store.Database, store.Root)
	if err != nil {
		return nil, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = db.Close()
		}
	}()

	sources, err := store.LoadSources()
	if err != nil {
		return nil, err
	}
	if err := db.Reattach(ctx, sources); err != nil {
		return nil, err
	}
	quarto, _ := exec.LookPath("quarto")
	renderer := report.NewRenderer(quarto, store, db)
	cleanup = false
	return &cliRuntime{
		store:    store,
		database: db,
		renderer: renderer,
		runner:   runner.New(store, db, renderer),
	}, nil
}

func isDefaultWorkspace(root string) bool {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return false
	}
	defaultRoot := filepath.Join(configDir, "duckdb_report")
	rootInfo, rootErr := os.Stat(root)
	defaultInfo, defaultErr := os.Stat(defaultRoot)
	if rootErr == nil && defaultErr == nil {
		return os.SameFile(rootInfo, defaultInfo)
	}
	return filepath.Clean(root) == filepath.Clean(defaultRoot)
}

func (r *cliRuntime) Close() error {
	if r == nil || r.database == nil {
		return nil
	}
	return r.database.Close()
}

func resolveWorkspace(explicit string) (string, error) {
	candidate := strings.TrimSpace(explicit)
	if candidate == "" {
		candidate = strings.TrimSpace(os.Getenv("DUCKDB_REPORT_HOME"))
	}
	if candidate == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("find application data directory: %w", err)
		}
		root := filepath.Join(configDir, "duckdb_report")
		saved, err := settings.New(root, root).Load()
		if err != nil {
			return "", err
		}
		candidate = saved.Workspace
		if strings.TrimSpace(candidate) == "" {
			candidate = root
		}
	}
	absolute, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}
	return absolute, nil
}

func workspaceFor(options *rootOptions) (string, error) {
	return resolveWorkspace(options.workspace)
}

func useRuntime(
	ctx context.Context,
	exampleFS fs.FS,
	options *rootOptions,
	run func(*cliRuntime) error,
) error {
	root, err := workspaceFor(options)
	if err != nil {
		return err
	}
	runtime, err := openRuntime(ctx, exampleFS, root)
	if err != nil {
		return err
	}
	defer func() {
		_ = runtime.Close()
	}()
	return run(runtime)
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("write json output: %w", err)
	}
	return nil
}

func writeResult(writer io.Writer, asJSON bool, value any, human func() error) error {
	if asJSON {
		return writeJSON(writer, value)
	}
	return human()
}

func writeQueryText(writer io.Writer, result queryResultView) error {
	table := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	for index, column := range result.Columns {
		if index > 0 {
			fmt.Fprint(table, "\t")
		}
		fmt.Fprint(table, column)
	}
	fmt.Fprintln(table)
	for _, row := range result.Rows {
		for index, value := range row {
			if index > 0 {
				fmt.Fprint(table, "\t")
			}
			fmt.Fprint(table, printableCell(value))
		}
		fmt.Fprintln(table)
	}
	if err := table.Flush(); err != nil {
		return fmt.Errorf("write query output: %w", err)
	}
	if result.Truncated {
		fmt.Fprintf(writer, "\n(truncated at %d rows)\n", result.RowCount)
	}
	return nil
}

type queryResultView struct {
	Columns   []string
	Rows      [][]any
	RowCount  int
	Truncated bool
}

func printableCell(value any) string {
	if value == nil {
		return "NULL"
	}
	text := fmt.Sprint(value)
	text = strings.ReplaceAll(text, "\r\n", "\\n")
	text = strings.ReplaceAll(text, "\n", "\\n")
	text = strings.ReplaceAll(text, "\t", "\\t")
	return text
}

func parseKeyValues(values []string) (map[string]string, error) {
	result := make(map[string]string, len(values))
	for _, value := range values {
		key, item, ok := strings.Cut(value, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, usagef("expected key=value, got %q", value)
		}
		result[key] = item
	}
	return result, nil
}

func resolveReadableFile(store *workspace.Store, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", usagef("file path is empty")
	}
	if info, err := os.Stat(value); err == nil && !info.IsDir() {
		absolute, absErr := filepath.Abs(value)
		if absErr != nil {
			return "", fmt.Errorf("resolve file: %w", absErr)
		}
		return absolute, nil
	}
	return store.ResolveExisting(value)
}

func newVersionCommand(options *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the application version",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return &usageError{err: err}
			}
			return nil
		},
		RunE: func(command *cobra.Command, _ []string) error {
			payload := struct {
				Version string `json:"version"`
			}{Version: appversion.Current}
			return writeResult(command.OutOrStdout(), options.json, payload, func() error {
				_, err := fmt.Fprintln(command.OutOrStdout(), appversion.Current)
				return err
			})
		},
	}
}
