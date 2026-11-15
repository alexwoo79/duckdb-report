package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alexwoo79/duckdb-report/internal/database"
	"github.com/alexwoo79/duckdb-report/internal/exportbundle"
	"github.com/alexwoo79/duckdb-report/internal/model"
	"github.com/alexwoo79/duckdb-report/internal/report"
	"github.com/alexwoo79/duckdb-report/internal/workflow"
	"github.com/alexwoo79/duckdb-report/internal/workspace"
)

type EventSink func(model.StepEvent)

type Runner struct {
	store    *workspace.Store
	database *database.Service
	renderer *report.Renderer
}

func New(store *workspace.Store, database *database.Service, renderer *report.Renderer) *Runner {
	return &Runner{store: store, database: database, renderer: renderer}
}

func (r *Runner) Run(ctx context.Context, request model.WorkflowRequest, emit EventSink) (model.WorkflowResult, error) {
	if err := workflow.Validate(request.Workflow); err != nil {
		return model.WorkflowResult{}, err
	}
	runID, runDir, err := r.store.NewRun(request.Workflow.Name)
	if err != nil {
		return model.WorkflowResult{}, err
	}
	logPath := runDir + "/workflow.log"
	logFile, err := os.Create(logPath)
	if err != nil {
		return model.WorkflowResult{}, fmt.Errorf("create workflow log: %w", err)
	}
	defer logFile.Close()

	logger := io.MultiWriter(logFile)
	result := model.WorkflowResult{
		RunID:     runID,
		Log:       make([]string, 0),
		StartedAt: time.Now().Format(time.RFC3339),
	}
	lastReportRunID := ""
	appendLog := func(message string) {
		result.Log = append(result.Log, message)
		_, _ = fmt.Fprintln(logger, message)
	}

	for _, step := range request.Workflow.Steps {
		if !step.Enabled {
			continue
		}
		started := time.Now()
		emitEvent(emit, model.StepEvent{
			RunID: runID, StepID: step.ID, StepName: step.Name, Status: "running",
		})
		appendLog("▶ " + step.Name)

		params, paramErr := workflow.ParamsForStep(request.Workflow, step, request.Params)
		if paramErr != nil {
			return r.finishStepError(emit, result, step, started, appendLog, paramErr)
		}
		resolved, resolveErr := resolveParams(params)
		if resolveErr != nil {
			return r.finishStepError(emit, result, step, started, appendLog, resolveErr)
		}

		stepCtx, cancel := context.WithTimeout(ctx, 7*time.Minute)
		var stepErr error
		switch step.Kind {
		case model.StepImport:
			stepErr = r.runImport(stepCtx, resolved, appendLog)
		case model.StepSQL:
			result.Query, stepErr = r.runSQL(stepCtx, resolved)
		case model.StepRender:
			var rendered model.RenderResult
			rendered, stepErr = r.runRender(stepCtx, request.Workflow.Name, resolved)
			if stepErr == nil {
				result.PreviewURL = rendered.PreviewURL
				result.PreviewPath = rendered.PreviewPath
				lastReportRunID = rendered.RunID
				result.Log = append(result.Log, rendered.Log...)
			}
		case model.StepExport:
			var packagePath string
			packagePath, stepErr = r.runExport(stepCtx, resolved, lastReportRunID, runID, appendLog)
			if stepErr == nil {
				result.PackagePath = packagePath
			}
		default:
			stepErr = fmt.Errorf("unsupported step kind %q", step.Kind)
		}
		cancel()
		if stepErr != nil {
			return r.finishStepError(emit, result, step, started, appendLog, stepErr)
		}
		duration := time.Since(started).Milliseconds()
		emitEvent(emit, model.StepEvent{
			RunID: runID, StepID: step.ID, StepName: step.Name, Status: "success",
			Message: "完成", DurationMS: duration,
		})
		appendLog(fmt.Sprintf("✓ %s (%d ms)", step.Name, duration))
	}

	result.FinishedAt = time.Now().Format(time.RFC3339)
	if result.PreviewURL == "" {
		result.PreviewURL = previewFromDatabasePath(r.store, result.Query)
	}
	return result, nil
}

func (r *Runner) runExport(ctx context.Context, params map[string]string, reportRunID, workflowRunID string, appendLog func(string)) (string, error) {
	if reportRunID == "" {
		return "", errors.New("export step requires a preceding render step")
	}
	reportDir, err := r.store.RunDir(reportRunID)
	if err != nil {
		return "", err
	}
	directory := strings.TrimSpace(params["directory"])
	if directory == "" {
		directory = "exports"
	}
	if filepath.IsAbs(directory) {
		return "", errors.New("export directory must be relative to the workspace")
	}
	exportDir := filepath.Clean(filepath.Join(r.store.Root, directory))
	relative, err := filepath.Rel(r.store.Root, exportDir)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("export directory escapes the workspace")
	}
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		return "", fmt.Errorf("create export directory: %w", err)
	}
	name := strings.TrimSpace(params["filename"])
	if name == "" {
		name = "report-package"
	}
	name = sanitizeFilename(name)
	destination := filepath.Join(exportDir, name+"-"+workflowRunID+".zip")
	if err := exportbundle.ExportReport(reportDir, destination, params); err != nil {
		return "", err
	}
	appendLog("  导出包：" + r.store.Relative(destination))
	return r.store.Relative(destination), nil
}

func sanitizeFilename(value string) string {
	var builder strings.Builder
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z',
			character >= 'A' && character <= 'Z',
			character >= '0' && character <= '9',
			character == '-', character == '_':
			builder.WriteRune(character)
		default:
			if builder.Len() > 0 && !strings.HasSuffix(builder.String(), "-") {
				builder.WriteByte('-')
			}
		}
	}
	result := strings.Trim(builder.String(), "-")
	if result == "" {
		return "report-package"
	}
	if len(result) > 80 {
		result = result[:80]
	}
	return result
}

func (r *Runner) finishStepError(emit EventSink, result model.WorkflowResult, step model.Step, started time.Time, appendLog func(string), err error) (model.WorkflowResult, error) {
	duration := time.Since(started).Milliseconds()
	message := err.Error()
	appendLog("✗ " + message)
	emitEvent(emit, model.StepEvent{
		RunID: result.RunID, StepID: step.ID, StepName: step.Name, Status: "error",
		Message: message, DurationMS: duration,
	})
	result.FinishedAt = time.Now().Format(time.RFC3339)
	return result, fmt.Errorf("workflow step %q: %w", step.Name, err)
}

func (r *Runner) runImport(ctx context.Context, params map[string]string, appendLog func(string)) error {
	path, err := r.store.ResolveExisting(params["path"])
	if err != nil {
		return err
	}
	kind := strings.ToLower(strings.TrimSpace(params["kind"]))
	if kind == "" {
		kind = strings.TrimPrefix(strings.ToLower(filepathExt(path)), ".")
	}
	table := strings.TrimSpace(params["table"])
	if table == "" {
		return errors.New("import table name is empty")
	}
	imported, err := r.database.Import(ctx, model.ImportRequest{
		Path: path, Kind: kind, Table: table, Sheet: params["sheet"],
	})
	if err != nil {
		return err
	}
	if err := r.store.SaveSource(imported.Source); err != nil {
		return err
	}
	appendLog(fmt.Sprintf("  数据源：%s → %s", imported.Source.Kind, imported.Source.Table))
	for _, message := range imported.Log {
		appendLog("  " + message)
	}
	return nil
}

func (r *Runner) runSQL(ctx context.Context, params map[string]string) (model.QueryResult, error) {
	mode := strings.ToLower(strings.TrimSpace(params["mode"]))
	if mode == "" {
		mode = "file"
	}
	sourceLabel := strings.TrimSpace(params["file"])
	if mode == "inline" {
		sourceLabel = "inline sql"
	}
	var content string
	baseDir := r.store.Root
	switch mode {
	case "file":
		sqlPath, err := r.store.ResolveWorkspace(params["file"])
		if err != nil {
			return model.QueryResult{}, err
		}
		raw, err := os.ReadFile(sqlPath)
		if err != nil {
			return model.QueryResult{}, fmt.Errorf("read sql file: %w", err)
		}
		content = string(raw)
		baseDir = filepath.Dir(sqlPath)
	case "inline":
		content = params["inline_sql"]
		if strings.TrimSpace(content) == "" {
			return model.QueryResult{}, errors.New("inline sql is empty")
		}
	default:
		return model.QueryResult{}, fmt.Errorf("unsupported sql preset mode %q", mode)
	}
	resolvedContent, err := workflow.ResolveTemplate(content, params)
	if err != nil {
		return model.QueryResult{}, err
	}
	resolvedContent, err = database.ExpandScript(resolvedContent, baseDir)
	if err != nil {
		return model.QueryResult{}, err
	}
	limit, err := strconv.Atoi(params["limit"])
	if err != nil || limit <= 0 {
		limit = 500
	}
	executed, err := r.database.Query(ctx, resolvedContent, limit)
	if err != nil {
		return model.QueryResult{}, fmt.Errorf("execute %s: %w", sourceLabel, err)
	}
	if queryPath := strings.TrimSpace(params["query_file"]); queryPath != "" {
		resolvedQueryPath, pathErr := r.store.ResolveWorkspace(queryPath)
		if pathErr != nil {
			return model.QueryResult{}, pathErr
		}
		queryBytes, readErr := os.ReadFile(resolvedQueryPath)
		if readErr != nil {
			return model.QueryResult{}, fmt.Errorf("read result sql: %w", readErr)
		}
		resolvedQuery, resolveErr := workflow.ResolveTemplate(string(queryBytes), params)
		if resolveErr != nil {
			return model.QueryResult{}, resolveErr
		}
		resolvedQuery, resolveErr = database.ExpandScript(resolvedQuery, filepath.Dir(resolvedQueryPath))
		if resolveErr != nil {
			return model.QueryResult{}, resolveErr
		}
		return r.database.Query(ctx, resolvedQuery, limit)
	}
	return executed, nil
}

func (r *Runner) runRender(ctx context.Context, workflowName string, params map[string]string) (model.RenderResult, error) {
	templatePath, err := r.store.ResolveWorkspace(params["template"])
	if err != nil {
		return model.RenderResult{}, err
	}
	content, err := os.ReadFile(templatePath)
	if err != nil {
		return model.RenderResult{}, fmt.Errorf("read qmd template: %w", err)
	}
	return r.renderer.Render(ctx, model.RenderRequest{
		Content:      string(content),
		Format:       params["format"],
		Query:        params["query"],
		Params:       params,
		SaveAs:       workflowName,
		TemplatePath: r.store.Relative(templatePath),
	})
}

func resolveParams(params map[string]string) (map[string]string, error) {
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
			break
		}
	}
	return result, nil
}

func emitEvent(emit EventSink, event model.StepEvent) {
	if emit != nil {
		emit(event)
	}
}

func previewFromDatabasePath(store *workspace.Store, _ model.QueryResult) string {
	runs, err := store.ListRuns()
	if err != nil || len(runs) == 0 {
		return ""
	}
	for _, run := range runs {
		if run.HTMLPath != "" {
			id := run.ID
			return "/preview/" + id + "/report.html"
		}
	}
	return ""
}

func filepathExt(path string) string {
	for i := len(path) - 1; i >= 0 && path[i] != '/' && path[i] != '\\'; i-- {
		if path[i] == '.' {
			return path[i:]
		}
	}
	return ""
}
