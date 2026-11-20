package cli

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/alexwoo79/duckdb-report/internal/database"
	"github.com/alexwoo79/duckdb-report/internal/model"
	"github.com/alexwoo79/duckdb-report/internal/report"
	"github.com/spf13/cobra"
)

var (
	doctorSelectStarPattern = regexp.MustCompile(`(?i)\bselect\s+\*`)
	doctorWritePattern      = regexp.MustCompile(`(?i)\b(drop|delete|update|insert|alter|truncate)\b`)
	doctorAssetListPattern  = regexp.MustCompile(`(?m)^\s*-\s+(assets/[a-zA-Z0-9._/-]+)\s*$`)
	doctorAssetLinkPattern  = regexp.MustCompile(`!?\[[^\]]*\]\((assets/[a-zA-Z0-9._/-]+)\)`)
)

type doctorIssue struct {
	Code       string `json:"code"`
	Severity   string `json:"severity"`
	File       string `json:"file,omitempty"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

type doctorSummary struct {
	Status      string `json:"status"`
	Errors      int    `json:"errors"`
	Warnings    int    `json:"warnings"`
	Suggestions int    `json:"suggestions"`
}

type doctorResult struct {
	Workspace string        `json:"workspace"`
	Summary   doctorSummary `json:"summary"`
	Issues    []doctorIssue `json:"issues"`
}

type issueCollector struct {
	issues []doctorIssue
	seen   map[string]struct{}
}

func (c *issueCollector) add(severity, code, file, message, suggestion string) {
	file = filepath.ToSlash(strings.TrimSpace(file))
	key := severity + "\x00" + code + "\x00" + file + "\x00" + message
	if _, exists := c.seen[key]; exists {
		return
	}
	c.seen[key] = struct{}{}
	c.issues = append(c.issues, doctorIssue{
		Code:       code,
		Severity:   severity,
		File:       file,
		Message:    message,
		Suggestion: suggestion,
	})
}

func (c *issueCollector) finish() doctorResult {
	if c.issues == nil {
		c.issues = []doctorIssue{}
	}
	sort.SliceStable(c.issues, func(i, j int) bool {
		left, right := severityRank(c.issues[i].Severity), severityRank(c.issues[j].Severity)
		if left != right {
			return left < right
		}
		if c.issues[i].File != c.issues[j].File {
			return c.issues[i].File < c.issues[j].File
		}
		return c.issues[i].Code < c.issues[j].Code
	})
	result := doctorResult{Issues: c.issues}
	for _, issue := range result.Issues {
		switch issue.Severity {
		case "error":
			result.Summary.Errors++
		case "warning":
			result.Summary.Warnings++
		default:
			result.Summary.Suggestions++
		}
	}
	result.Summary.Status = "ok"
	if result.Summary.Warnings > 0 {
		result.Summary.Status = "warning"
	}
	if result.Summary.Errors > 0 {
		result.Summary.Status = "error"
	}
	return result
}

func severityRank(severity string) int {
	switch severity {
	case "error":
		return 0
	case "warning":
		return 1
	default:
		return 2
	}
}

func newDoctorCommand(exampleFS fs.FS, options *rootOptions) *cobra.Command {
	var explain bool
	var strict bool
	command := &cobra.Command{
		Use:   "doctor",
		Short: "Audit workspace SQL, QMD, report assets, and runtimes",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return &usageError{err: err}
			}
			return nil
		},
		RunE: func(command *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(command.Context(), 60*time.Second)
			defer cancel()

			var findingsError error
			runErr := useRuntime(ctx, exampleFS, options, func(runtime *cliRuntime) error {
				collector := &issueCollector{seen: make(map[string]struct{})}
				environment := runtime.renderer.Environments(ctx)
				if strings.TrimSpace(environment.DuckDBVersion) == "" {
					collector.add("error", "env.duckdb_missing", "", "DuckDB version was not detected", "check the embedded DuckDB driver")
				}

				sqlFiles, err := runtime.store.ListSQL()
				if err != nil {
					return err
				}
				for _, file := range sqlFiles {
					checkDoctorSQL(ctx, runtime, file.Path, explain, collector)
				}

				templates, err := runtime.store.ListTemplates()
				if err != nil {
					return err
				}
				for _, file := range templates {
					checkDoctorQMD(runtime, file.Path, environment, collector)
				}

				checked := collector.finish()
				result := doctorResult{
					Workspace: runtime.store.Root,
					Summary:   checked.Summary,
					Issues:    checked.Issues,
				}
				if err := writeResult(command.OutOrStdout(), options.json, result, func() error {
					return writeDoctorText(command, result)
				}); err != nil {
					return err
				}
				if result.Summary.Errors > 0 || (strict && result.Summary.Warnings > 0) {
					findingsError = fmt.Errorf(
						"doctor found %d error(s) and %d warning(s)",
						result.Summary.Errors,
						result.Summary.Warnings,
					)
				}
				return nil
			})
			if runErr != nil {
				return runErr
			}
			return findingsError
		},
	}
	command.Flags().BoolVar(&explain, "explain", false, "run EXPLAIN against workspace SQL files")
	command.Flags().BoolVar(&strict, "strict", false, "return a failing exit code when warnings are found")
	return command
}

func writeDoctorText(command *cobra.Command, result doctorResult) error {
	writer := command.OutOrStdout()
	fmt.Fprintf(
		writer,
		"%s: %d error(s), %d warning(s), %d suggestion(s)\n",
		strings.ToUpper(result.Summary.Status),
		result.Summary.Errors,
		result.Summary.Warnings,
		result.Summary.Suggestions,
	)
	for _, issue := range result.Issues {
		location := issue.File
		if location == "" {
			location = "workspace"
		}
		fmt.Fprintf(writer, "[%s] %s %s: %s\n", issue.Severity, issue.Code, location, issue.Message)
		if issue.Suggestion != "" {
			fmt.Fprintf(writer, "  suggestion: %s\n", issue.Suggestion)
		}
	}
	return nil
}

func checkDoctorSQL(
	ctx context.Context,
	runtime *cliRuntime,
	relativePath string,
	explain bool,
	collector *issueCollector,
) {
	path, err := runtime.store.ResolveWorkspace(relativePath)
	if err != nil {
		collector.add("error", "sql.file_unresolved", relativePath, err.Error(), "use an existing SQL file inside the workspace")
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		collector.add("error", "sql.file_read", relativePath, err.Error(), "check file permissions")
		return
	}
	expanded, err := database.ExpandScript(string(data), filepath.Dir(path))
	if err != nil {
		collector.add("error", "sql.read_script", relativePath, err.Error(), "fix .read paths and cycles")
		return
	}
	if err := runtime.database.ValidateSyntax(ctx, expanded); err != nil {
		collector.add("error", "sql.syntax", relativePath, err.Error(), "fix the SQL syntax")
	}
	checkSQLHeuristics(expanded, relativePath, collector)
	if explain {
		if err := runtime.database.Explain(ctx, expanded); err != nil {
			collector.add("warning", "sql.explain_failed", relativePath, err.Error(), "run doctor after creating the referenced tables")
		}
	}
}

func checkDoctorQMD(
	runtime *cliRuntime,
	relativePath string,
	environment model.Environment,
	collector *issueCollector,
) {
	path, err := runtime.store.ResolveWorkspace(relativePath)
	if err != nil {
		collector.add("error", "report.template_unresolved", relativePath, err.Error(), "use an existing QMD file inside the workspace")
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		collector.add("error", "report.template_read", relativePath, err.Error(), "check file permissions")
		return
	}
	content := string(data)
	if environment.QuartoPath == "" {
		collector.add("error", "env.quarto_missing", relativePath, "Quarto is not available", "install Quarto and make it discoverable on PATH")
	}

	defaults := report.DeclaredParamDefaults(content)
	for _, name := range report.DeclaredParams(content) {
		if _, exists := defaults[name]; !exists {
			collector.add(
				"suggestion",
				"report.param_default",
				relativePath,
				fmt.Sprintf("QMD declares parameter %q without a default", name),
				"declare a default in QMD params or pass it when rendering",
			)
		}
	}
	checkReportAssets(path, content, collector)

	requirements := report.DetectRequirements(content)
	if requirements.NeedsR && environment.RPath == "" {
		collector.add("error", "report.r_missing", relativePath, "QMD contains R code but Rscript is unavailable", "install R and the documented R packages")
	}
	if requirements.NeedsPython && !environment.JupyterAvailable {
		collector.add("error", "report.jupyter_missing", relativePath, "QMD contains Python code but Jupyter is unavailable", "install Jupyter and the documented Python packages")
	}
}

func checkSQLHeuristics(content, source string, collector *issueCollector) {
	if doctorSelectStarPattern.MatchString(content) {
		collector.add("suggestion", "sql.select_star", source, "query selects every column with SELECT *", "select only columns required by the report or analysis")
	}
	if doctorWritePattern.MatchString(content) {
		collector.add("warning", "sql.write_statement", source, "SQL contains a write or destructive statement", "confirm this mutation is intentional and runs in the expected workspace")
	}
}

func checkReportAssets(templatePath, content string, collector *issueCollector) {
	assets := make(map[string]struct{})
	for _, pattern := range []*regexp.Regexp{doctorAssetListPattern, doctorAssetLinkPattern} {
		for _, match := range pattern.FindAllStringSubmatch(content, -1) {
			if len(match) == 2 {
				assets[match[1]] = struct{}{}
			}
		}
	}
	for asset := range assets {
		path := filepath.Join(filepath.Dir(templatePath), filepath.FromSlash(asset))
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			collector.add("error", "report.asset_missing", filepath.ToSlash(asset), "referenced report asset was not found", "create the asset next to the QMD template")
		}
	}
}
