package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alexwoo79/duckdb-report/internal/database"
	"github.com/alexwoo79/duckdb-report/internal/model"
	"github.com/alexwoo79/duckdb-report/internal/report"
	"github.com/alexwoo79/duckdb-report/internal/workflow"
	"github.com/alexwoo79/duckdb-report/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	doctorVariablePattern    = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.-]+)\s*\}\}`)
	doctorNamedTablePattern  = regexp.MustCompile(`\{\{\s*table_([a-zA-Z0-9_.-]+)\s*\}\}`)
	doctorPrimaryTable       = regexp.MustCompile(`\{\{\s*table\s*\}\}`)
	doctorSelectStarPattern  = regexp.MustCompile(`(?i)\bselect\s+\*`)
	doctorWritePattern       = regexp.MustCompile(`(?i)\b(drop|delete|update|insert|alter|truncate)\b`)
	doctorAssetListPattern   = regexp.MustCompile(`(?m)^\s*-\s+(assets/[a-zA-Z0-9._/-]+)\s*$`)
	doctorAssetLinkPattern   = regexp.MustCompile(`!?\[[^\]]*\]\((assets/[a-zA-Z0-9._/-]+)\)`)
	doctorSupportedSourceSet = map[string]struct{}{
		"csv": {}, "json": {}, "parquet": {}, "xlsx": {}, "xlsm": {}, "duckdb": {}, "db": {},
	}
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
	Workflow  string        `json:"workflow,omitempty"`
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
		Short: "Audit workspace files, workflow wiring, SQL, QMD, and runtimes",
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

				value, workflowPath, decodeErr, validationErr := loadDoctorWorkflow(runtime.store)
				result := doctorResult{Workspace: runtime.store.Root}
				if decodeErr != nil {
					collector.add("error", "workflow.decode", workflowPath, decodeErr.Error(), "fix workflows/current.json before running the workflow")
				} else {
					result.Workflow = value.Name
					if validationErr != nil {
						collector.add("error", "workflow.validation", workflowPath, validationErr.Error(), "fix the workflow schema before running it")
					}
					checkDoctorWorkflow(ctx, runtime, value, workflowPath, environment, explain, collector)
				}

				result = mergeDoctorSummary(result, collector.finish())
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
	command.Flags().BoolVar(&explain, "explain", false, "run EXPLAIN against SQL and report queries using current workspace tables")
	command.Flags().BoolVar(&strict, "strict", false, "return a failing exit code when warnings are found")
	return command
}

func mergeDoctorSummary(result, checked doctorResult) doctorResult {
	result.Summary = checked.Summary
	result.Issues = checked.Issues
	return result
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

func loadDoctorWorkflow(store *workspace.Store) (model.Workflow, string, error, error) {
	path := store.WorkflowFile
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Workflow{}, path, fmt.Errorf("read workflow: %w", err), nil
	}
	var value model.Workflow
	if err := json.Unmarshal(data, &value); err != nil {
		return model.Workflow{}, path, fmt.Errorf("decode workflow: %w", err), nil
	}
	return value, path, nil, workflow.Validate(value)
}

func checkDoctorWorkflow(
	ctx context.Context,
	runtime *cliRuntime,
	value model.Workflow,
	workflowPath string,
	environment model.Environment,
	explain bool,
	collector *issueCollector,
) {
	presets := make(map[string]model.Preset, len(value.Presets))
	for _, preset := range value.Presets {
		presets[preset.ID] = preset
		switch preset.Type {
		case model.PresetDataSource, model.PresetSQLSet, model.PresetReport, model.PresetOutput:
		default:
			collector.add("error", "workflow.preset_type", workflowPath, fmt.Sprintf("preset %q has unsupported type %q", preset.ID, preset.Type), "use datasource, sqlset, report, or output")
		}
	}

	expectedPresetKind := map[string]string{
		model.StepImport: model.PresetDataSource,
		model.StepSQL:    model.PresetSQLSet,
		model.StepRender: model.PresetReport,
		model.StepExport: model.PresetOutput,
	}
	stepIDs := make(map[string]struct{}, len(value.Steps))
	usedPresets := make(map[string]struct{}, len(value.Steps))
	enabledSteps := 0
	renderSeen := false
	for _, step := range value.Steps {
		if _, exists := stepIDs[step.ID]; exists {
			collector.add("error", "workflow.step_id_duplicate", workflowPath, fmt.Sprintf("step id %q is duplicated", step.ID), "give every step a unique id")
		}
		stepIDs[step.ID] = struct{}{}
		usedPresets[step.PresetID] = struct{}{}
		if !step.Enabled {
			continue
		}
		enabledSteps++
		if step.Kind == model.StepRender {
			renderSeen = true
		}
		if step.Kind == model.StepExport && !renderSeen {
			collector.add("error", "workflow.export_before_render", workflowPath, fmt.Sprintf("step %q exports before any render step", step.Name), "place a successful render step before export")
		}

		preset, exists := presets[step.PresetID]
		if !exists {
			collector.add("error", "workflow.preset_missing", workflowPath, fmt.Sprintf("step %q references missing preset %q", step.Name, step.PresetID), "add the preset or correct presetId")
			continue
		}
		if expected := expectedPresetKind[step.Kind]; expected != "" && preset.Type != expected {
			collector.add("error", "workflow.preset_kind", workflowPath, fmt.Sprintf("step %q kind %q uses preset type %q", step.Name, step.Kind, preset.Type), "match each step kind to the expected preset type")
			continue
		}
		params, err := workflow.ParamsForStep(value, step, value.Runtime)
		if err != nil {
			collector.add("error", "workflow.params", workflowPath, err.Error(), "fix the step preset or overrides")
			continue
		}
		params, err = resolveDoctorParams(params)
		if err != nil {
			collector.add("error", "workflow.variable_missing", workflowPath, err.Error(), "provide the missing runtime, step override, or preset value")
			continue
		}

		switch step.Kind {
		case model.StepImport:
			checkDoctorImport(runtime.store, params, workflowPath, collector)
		case model.StepSQL:
			checkDoctorSQL(ctx, runtime, params, workflowPath, explain, collector)
		case model.StepRender:
			checkDoctorReport(ctx, runtime, params, workflowPath, environment, explain, collector)
		case model.StepExport:
			checkDoctorOutput(params, workflowPath, collector)
		}
	}
	if enabledSteps == 0 {
		collector.add("error", "workflow.no_enabled_steps", workflowPath, "workflow has no enabled steps", "enable at least one step")
	}
	for _, preset := range value.Presets {
		if _, used := usedPresets[preset.ID]; !used {
			collector.add("suggestion", "workflow.preset_unused", workflowPath, fmt.Sprintf("preset %q is not referenced by any step", preset.ID), "remove draft presets or connect them to a step")
		}
	}
}

func resolveDoctorParams(params map[string]string) (map[string]string, error) {
	result := make(map[string]string, len(params))
	for key, value := range params {
		result[key] = value
	}
	for pass := 0; pass < 4; pass++ {
		changed := false
		for key, value := range result {
			resolved, err := workflow.ResolveTemplate(value, result)
			if err != nil {
				return nil, err
			}
			if resolved != value {
				result[key] = resolved
				changed = true
			}
		}
		if !changed {
			return result, nil
		}
	}
	return result, nil
}

func checkDoctorImport(store *workspace.Store, params map[string]string, workflowPath string, collector *issueCollector) {
	path := strings.TrimSpace(params["path"])
	if path == "" {
		collector.add("error", "import.path_missing", workflowPath, "import step has no source path", "set path in the datasource preset")
	} else if _, err := store.ResolveExisting(path); err != nil {
		collector.add("error", "import.path_unresolved", workflowPath, err.Error(), "use an existing workspace-relative or absolute data file")
	}
	kind := strings.ToLower(strings.TrimSpace(params["kind"]))
	if kind == "" && path != "" {
		kind = strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	}
	if _, supported := doctorSupportedSourceSet[kind]; !supported {
		collector.add("error", "import.kind", workflowPath, fmt.Sprintf("unsupported import kind %q", kind), "use csv, json, parquet, xlsx, xlsm, duckdb, or db")
	}
	if strings.TrimSpace(params["table"]) == "" {
		collector.add("error", "import.table_missing", workflowPath, "import step has no target table", "set table in the datasource preset")
	}
	if sheet := strings.TrimSpace(params["sheet"]); sheet != "" && kind != "xlsx" && kind != "xlsm" {
		collector.add("warning", "import.sheet_ignored", workflowPath, fmt.Sprintf("sheet %q is only used for Excel sources", sheet), "remove sheet or use an Excel source")
	}
}

func checkDoctorSQL(
	ctx context.Context,
	runtime *cliRuntime,
	params map[string]string,
	workflowPath string,
	explain bool,
	collector *issueCollector,
) {
	mode := strings.ToLower(strings.TrimSpace(params["mode"]))
	if mode == "" {
		mode = "file"
	}
	var content string
	var baseDir string
	var source string
	switch mode {
	case "file":
		file := strings.TrimSpace(params["file"])
		if file == "" {
			collector.add("error", "sql.file_missing", workflowPath, "SQL preset has no file", "set file or use mode=inline")
			return
		}
		path, err := runtime.store.ResolveWorkspace(file)
		if err != nil {
			collector.add("error", "sql.file_unresolved", workflowPath, err.Error(), "use an existing SQL file inside the workspace")
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			collector.add("error", "sql.file_read", runtime.store.Relative(path), err.Error(), "check file permissions")
			return
		}
		content = string(data)
		baseDir = filepath.Dir(path)
		source = runtime.store.Relative(path)
	case "inline":
		content = params["inline_sql"]
		baseDir = runtime.store.Root
		source = workflowPath
	default:
		collector.add("error", "sql.mode", workflowPath, fmt.Sprintf("unsupported SQL mode %q", mode), "use mode=file or mode=inline")
		return
	}

	resolved, err := workflow.ResolveTemplate(content, params)
	if err != nil {
		collector.add("error", "sql.variable_missing", source, err.Error(), "provide every SQL template variable")
		return
	}
	expanded, err := database.ExpandScript(resolved, baseDir)
	if err != nil {
		collector.add("error", "sql.read_script", source, err.Error(), "fix .read paths and cycles")
		return
	}
	if err := runtime.database.ValidateSyntax(ctx, expanded); err != nil {
		collector.add("error", "sql.syntax", source, err.Error(), "fix the SQL syntax before running the workflow")
	}
	checkSQLHeuristics(expanded, source, collector)

	queryFile := strings.TrimSpace(params["query_file"])
	if queryFile == "" {
		return
	}
	queryPath, err := runtime.store.ResolveWorkspace(queryFile)
	if err != nil {
		collector.add("error", "sql.query_file_unresolved", workflowPath, err.Error(), "use an existing query file inside the workspace")
		return
	}
	queryData, err := os.ReadFile(queryPath)
	if err != nil {
		collector.add("error", "sql.query_file_read", runtime.store.Relative(queryPath), err.Error(), "check file permissions")
		return
	}
	querySource := runtime.store.Relative(queryPath)
	resolvedQuery, err := workflow.ResolveTemplate(string(queryData), params)
	if err != nil {
		collector.add("error", "sql.query_variable_missing", querySource, err.Error(), "provide every result-query template variable")
		return
	}
	expandedQuery, err := database.ExpandScript(resolvedQuery, filepath.Dir(queryPath))
	if err != nil {
		collector.add("error", "sql.query_read_script", querySource, err.Error(), "fix .read paths and cycles")
		return
	}
	if err := runtime.database.ValidateSyntax(ctx, expandedQuery); err != nil {
		collector.add("error", "sql.query_syntax", querySource, err.Error(), "fix the result-query syntax")
	}
	checkSQLHeuristics(expandedQuery, querySource, collector)
	if explain {
		if err := runtime.database.Explain(ctx, expandedQuery); err != nil {
			collector.add("warning", "sql.explain_failed", querySource, err.Error(), "run doctor after importing or creating the referenced tables")
		}
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

func checkDoctorReport(
	ctx context.Context,
	runtime *cliRuntime,
	params map[string]string,
	workflowPath string,
	environment model.Environment,
	explain bool,
	collector *issueCollector,
) {
	format := strings.ToLower(strings.TrimSpace(params["format"]))
	if format == "" {
		format = "html"
	}
	if format != "html" && format != "pdf" {
		collector.add("error", "report.format", workflowPath, fmt.Sprintf("unsupported report format %q", format), "use format=html or format=pdf")
	}
	if environment.QuartoPath == "" {
		collector.add("error", "env.quarto_missing", workflowPath, "Quarto is not available", "install Quarto and make it discoverable on PATH")
	}
	if format == "pdf" && !environment.PDFAvailable {
		collector.add("error", "env.pdf_unavailable", workflowPath, "PDF rendering is unavailable", "install the bundled Typst setup through Quarto or a LaTeX runtime")
	}

	template := strings.TrimSpace(params["template"])
	if template == "" {
		collector.add("error", "report.template_missing", workflowPath, "report preset has no template", "set template to a QMD file")
		return
	}
	path, err := runtime.store.ResolveWorkspace(template)
	if err != nil {
		collector.add("error", "report.template_unresolved", workflowPath, err.Error(), "use an existing QMD template inside the workspace")
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		collector.add("error", "report.template_read", runtime.store.Relative(path), err.Error(), "check file permissions")
		return
	}
	content := string(data)
	source := runtime.store.Relative(path)

	for key, value := range report.DeclaredParamDefaults(content) {
		if _, exists := params[key]; !exists {
			params[key] = value
		}
	}
	for _, name := range report.DeclaredParams(content) {
		if _, exists := params[name]; !exists {
			collector.add("suggestion", "report.param_declared", source, fmt.Sprintf("QMD declares parameter %q without a runtime or preset default", name), "set a default in QMD params or provide it in workflow.runtime")
			params[name] = ""
		}
	}

	checkTemplateVariables(content, params, source, collector)
	checkReportTables(ctx, runtime, content, params, source, explain, collector)
	checkReportAssets(path, content, collector)

	requirements := report.DetectRequirements(content)
	if requirements.NeedsR && environment.RPath == "" {
		collector.add("error", "report.r_missing", source, "QMD contains R code but Rscript is unavailable", "install R and the documented R packages")
	}
	if requirements.NeedsPython && !environment.JupyterAvailable {
		collector.add("error", "report.jupyter_missing", source, "QMD contains Python code but Jupyter is unavailable", "install Jupyter and the documented Python packages")
	}
}

func checkTemplateVariables(content string, params map[string]string, source string, collector *issueCollector) {
	for _, match := range doctorVariablePattern.FindAllStringSubmatch(content, -1) {
		if len(match) != 2 {
			continue
		}
		name := match[1]
		if name == "table" || name == "database" || name == "report_font" || strings.HasPrefix(name, "table_") {
			continue
		}
		if _, exists := params[name]; !exists {
			collector.add("error", "report.variable_missing", source, fmt.Sprintf("template variable %q has no value", name), "provide the runtime, step override, or QMD parameter default")
		}
	}
}

func checkReportTables(
	ctx context.Context,
	runtime *cliRuntime,
	content string,
	params map[string]string,
	source string,
	explain bool,
	collector *issueCollector,
) {
	if doctorPrimaryTable.MatchString(content) {
		query := strings.TrimSpace(params["query"])
		if query == "" {
			collector.add("warning", "report.query_missing", source, "template contains {{table}} but no primary query is configured", "set query in the report preset or choose a template without a table")
		} else {
			checkReportQuery(ctx, runtime, query, params, source, explain, collector)
		}
	}
	seen := make(map[string]struct{})
	for _, match := range doctorNamedTablePattern.FindAllStringSubmatch(content, -1) {
		if len(match) != 2 {
			continue
		}
		name := match[1]
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		key := "query_" + name
		query := strings.TrimSpace(params[key])
		if query == "" {
			collector.add("error", "report.named_query_missing", source, fmt.Sprintf("{{table_%s}} has no %s parameter", name, key), "add the named query to the report preset")
			continue
		}
		checkReportQuery(ctx, runtime, query, params, source, explain, collector)
	}
}

func checkReportQuery(
	ctx context.Context,
	runtime *cliRuntime,
	query string,
	params map[string]string,
	source string,
	explain bool,
	collector *issueCollector,
) {
	resolved, err := workflow.ResolveTemplate(query, params)
	if err != nil {
		collector.add("error", "report.query_variable_missing", source, err.Error(), "provide every report query template variable")
		return
	}
	if err := runtime.database.ValidateSyntax(ctx, resolved); err != nil {
		collector.add("error", "report.query_syntax", source, err.Error(), "fix the report query syntax")
		return
	}
	checkSQLHeuristics(resolved, source, collector)
	if explain {
		if err := runtime.database.Explain(ctx, resolved); err != nil {
			collector.add("warning", "report.explain_failed", source, err.Error(), "run doctor after the SQL step has created the referenced tables")
		}
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

func checkDoctorOutput(params map[string]string, workflowPath string, collector *issueCollector) {
	directory := strings.TrimSpace(params["directory"])
	if filepath.IsAbs(directory) {
		collector.add("error", "output.directory_absolute", workflowPath, "export directory must stay inside the workspace", "use a workspace-relative directory")
	}
	count := 0
	for _, key := range []string{"include_report", "include_database", "include_source"} {
		value := strings.TrimSpace(params[key])
		if value == "" {
			count++
			continue
		}
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			collector.add("warning", "output.bool_invalid", workflowPath, fmt.Sprintf("%s=%q is invalid and falls back to the default", key, value), "use true or false")
			count++
			continue
		}
		if enabled {
			count++
		}
	}
	if count == 0 {
		collector.add("error", "output.empty", workflowPath, "export step excludes report, database, and source", "enable at least one package component")
	}
}
