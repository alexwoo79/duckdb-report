package report

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/alexwoo79/duckdb-report/internal/database"
	"github.com/alexwoo79/duckdb-report/internal/model"
	"github.com/alexwoo79/duckdb-report/internal/workflow"
	"github.com/alexwoo79/duckdb-report/internal/workspace"
)

type Renderer struct {
	quarto   string
	store    *workspace.Store
	database *database.Service
}

func NewRenderer(quarto string, store *workspace.Store, database *database.Service) *Renderer {
	EnsureRuntimePath()
	if strings.TrimSpace(quarto) == "" {
		quarto, _ = exec.LookPath("quarto")
	}
	return &Renderer{quarto: strings.TrimSpace(quarto), store: store, database: database}
}

func (r *Renderer) Environments(ctx context.Context) model.Environment {
	result := model.Environment{}
	result.RPath, _ = exec.LookPath("Rscript")
	if result.RPath != "" {
		result.RVersion = commandVersion(ctx, result.RPath)
	}
	result.PythonPath, _ = exec.LookPath("python3")
	if result.PythonPath != "" {
		result.PythonVersion = commandVersion(ctx, result.PythonPath)
	}
	_, err := exec.LookPath("jupyter")
	result.JupyterAvailable = err == nil
	if r.quarto != "" {
		result.QuartoPath = r.quarto
		result.QuartoVersion = commandVersion(ctx, r.quarto)
	}
	result.PDFAvailable = result.QuartoPath != "" || hasExecutable("xelatex") || hasExecutable("pdflatex")
	if r.database != nil {
		if version, err := r.database.Version(ctx); err == nil {
			result.DuckDBVersion = version
		}
	}
	return result
}

func (r *Renderer) Render(ctx context.Context, request model.RenderRequest) (model.RenderResult, error) {
	if strings.TrimSpace(request.Content) == "" {
		return model.RenderResult{}, errors.New("qmd content is empty")
	}
	format := strings.ToLower(strings.TrimSpace(request.Format))
	if format == "" {
		format = "html"
	}
	if format != "html" && format != "pdf" {
		return model.RenderResult{}, fmt.Errorf("unsupported output format %q", format)
	}
	if r.quarto == "" {
		return model.RenderResult{}, errors.New("quarto executable was not found")
	}
	if err := validateScriptRuntime(request.Content); err != nil {
		return model.RenderResult{}, err
	}

	params := make(map[string]string, len(request.Params)+1)
	for key, value := range request.Params {
		params[key] = value
	}
	params["database"] = r.store.Database
	if strings.TrimSpace(params["report_font"]) == "" {
		params["report_font"] = defaultReportFont(runtime.GOOS)
	}
	title := strings.TrimSpace(params["title"])
	if title == "" {
		title = "DuckDB 报告"
		params["title"] = title
	}
	saveName := strings.TrimSpace(request.SaveAs)
	if saveName == "" {
		saveName = title
	}
	runID, runDir, err := r.store.NewRun(saveName)
	if err != nil {
		return model.RenderResult{}, err
	}
	if r.database != nil {
		snapshotPath := filepath.Join(runDir, "analysis.duckdb")
		if err := r.database.Snapshot(ctx, snapshotPath); err != nil {
			return model.RenderResult{}, fmt.Errorf("create report database snapshot: %w", err)
		}
		params["database"] = filepath.ToSlash(snapshotPath)
	}
	if request.TemplatePath != "" {
		if err := copyTemplateAssets(r.store.Root, request.TemplatePath, runDir); err != nil {
			return model.RenderResult{}, err
		}
	}

	contentWithTable := request.Content
	if strings.Contains(contentWithTable, "{{table}}") {
		tableContent := "<p><em>没有配置报告数据查询。</em></p>"
		if strings.TrimSpace(request.Query) != "" {
			resolvedQuery, resolveErr := workflow.ResolveTemplate(request.Query, params)
			if resolveErr != nil {
				return model.RenderResult{}, resolveErr
			}
			queryResult, queryErr := r.database.Query(ctx, resolvedQuery, 2000)
			if queryErr != nil {
				return model.RenderResult{}, fmt.Errorf("build report table: %w", queryErr)
			}
			if format == "pdf" {
				tableContent = TypstTable(queryResult)
			} else {
				tableContent = HTMLTable(queryResult)
			}
		}
		contentWithTable = strings.ReplaceAll(contentWithTable, "{{table}}", tableContent)
	}
	resolvedContent, err := workflow.ResolveTemplate(contentWithTable, params)
	if err != nil {
		return model.RenderResult{}, err
	}
	resolvedContent = ApplyDeclaredParams(resolvedContent, params)

	qmdPath := filepath.Join(runDir, "report.qmd")
	if err := os.WriteFile(qmdPath, []byte(resolvedContent), 0o644); err != nil {
		return model.RenderResult{}, fmt.Errorf("write temporary qmd: %w", err)
	}
	logPath := filepath.Join(runDir, "quarto.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return model.RenderResult{}, fmt.Errorf("create quarto log: %w", err)
	}
	defer logFile.Close()

	var log bytes.Buffer
	output := io.MultiWriter(logFile, &log)
	outputName := "report." + format
	quartoFormat := format
	if format == "pdf" {
		quartoFormat = "typst"
	}
	renderCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	command := exec.CommandContext(
		renderCtx,
		r.quarto,
	)
	command.Args = append(command.Args,
		"render",
		"report.qmd",
		"--to",
		quartoFormat,
		"--output",
		outputName,
	)
	if format == "html" {
		command.Args = append(command.Args, "-M", "embed-resources:true")
	}
	command.Args = append(command.Args, "--no-cache")
	command.Dir = runDir
	command.Stdout = output
	command.Stderr = output
	runErr := command.Run()
	logLines := splitLog(log.String())
	if runErr != nil {
		detail := strings.TrimSpace(log.String())
		if len(detail) > 6000 {
			detail = detail[len(detail)-6000:]
		}
		if detail != "" {
			return model.RenderResult{
				RunID:  runID,
				Format: format,
				Log:    logLines,
			}, fmt.Errorf("render qmd: %w\n%s", runErr, detail)
		}
		return model.RenderResult{
			RunID:  runID,
			Format: format,
			Log:    logLines,
		}, fmt.Errorf("render qmd: %w", runErr)
	}

	outputPath := filepath.Join(runDir, outputName)
	if _, err := os.Stat(outputPath); err != nil {
		return model.RenderResult{
			RunID:  runID,
			Format: format,
			Log:    logLines,
		}, fmt.Errorf("quarto output is missing: %w", err)
	}
	return model.RenderResult{
		RunID:       runID,
		PreviewURL:  "/preview/" + runID + "/" + outputName,
		PreviewPath: r.store.Relative(outputPath),
		OutputPath:  outputPath,
		Format:      format,
		Log:         logLines,
	}, nil
}

func defaultReportFont(goos string) string {
	switch goos {
	case "darwin":
		return "PingFang SC"
	case "windows":
		return "Microsoft YaHei"
	default:
		return "Noto Sans CJK SC"
	}
}

func HTMLTable(result model.QueryResult) string {
	var builder strings.Builder
	builder.WriteString(`<div class="duckdb-report-table"><table>`)
	builder.WriteString("<thead><tr>")
	for _, column := range result.Columns {
		builder.WriteString("<th>")
		builder.WriteString(html.EscapeString(column))
		builder.WriteString("</th>")
	}
	builder.WriteString("</tr></thead><tbody>")
	if len(result.Rows) == 0 {
		builder.WriteString(`<tr><td colspan="`)
		builder.WriteString(fmt.Sprintf("%d", max(1, len(result.Columns))))
		builder.WriteString(`" class="empty">查询没有返回记录。</td></tr>`)
	}
	for _, row := range result.Rows {
		builder.WriteString("<tr>")
		for _, value := range row {
			builder.WriteString("<td>")
			if value == nil {
				builder.WriteString(`<span class="null">NULL</span>`)
			} else {
				builder.WriteString(html.EscapeString(fmt.Sprint(value)))
			}
			builder.WriteString("</td>")
		}
		builder.WriteString("</tr>")
	}
	builder.WriteString("</tbody></table>")
	if result.Truncated {
		builder.WriteString(`<p class="table-note">报告表格仅显示前 2,000 行。</p>`)
	}
	builder.WriteString("</div>")
	return builder.String()
}

func MarkdownTable(result model.QueryResult) string {
	if len(result.Columns) == 0 {
		return "_查询没有返回记录。_"
	}
	var builder strings.Builder
	builder.WriteString("| ")
	for index, column := range result.Columns {
		if index > 0 {
			builder.WriteString(" | ")
		}
		builder.WriteString(escapeMarkdownCell(column))
	}
	builder.WriteString(" |\n| ")
	for index := range result.Columns {
		if index > 0 {
			builder.WriteString(" | ")
		}
		builder.WriteString("---")
	}
	builder.WriteString(" |\n")
	for _, row := range result.Rows {
		builder.WriteString("| ")
		for index := range result.Columns {
			if index > 0 {
				builder.WriteString(" | ")
			}
			if index >= len(row) || row[index] == nil {
				builder.WriteString("NULL")
			} else {
				builder.WriteString(escapeMarkdownCell(fmt.Sprint(row[index])))
			}
		}
		builder.WriteString(" |\n")
	}
	if result.Truncated {
		builder.WriteString("\n> 报告表格仅显示前 2,000 行。\n")
	}
	return builder.String()
}

func TypstTable(result model.QueryResult) string {
	if len(result.Columns) == 0 {
		return "_查询没有返回记录。_"
	}
	var builder strings.Builder
	builder.WriteString("```{=typst}\n")
	builder.WriteString("#table(\n")
	builder.WriteString("  columns: (1fr,) * ")
	builder.WriteString(fmt.Sprintf("%d", len(result.Columns)))
	builder.WriteString(",\n")
	builder.WriteString("  inset: (x: 4.5pt, y: 4pt),\n")
	builder.WriteString("  align: (")
	for index := range result.Columns {
		if index > 0 {
			builder.WriteString(", ")
		}
		if columnIsNumeric(result, index) {
			builder.WriteString("right")
		} else {
			builder.WriteString("left")
		}
	}
	builder.WriteString("),\n")
	builder.WriteString("  fill: (x, y) => if y == 0 { rgb(\"#eef2f0\") } else if calc.even(y) { rgb(\"#fafbf9\") } else { none },\n")
	builder.WriteString("  stroke: (x, y) => if y == 0 { (bottom: 1pt + rgb(\"#162a33\")) } else if y == ")
	builder.WriteString(fmt.Sprintf("%d", len(result.Rows)))
	builder.WriteString(" { (bottom: 0.8pt + rgb(\"#d5dcda\")) } else { (bottom: 0.35pt + rgb(\"#e7ebea\")) },\n")
	builder.WriteString("  table.header(repeat: true,\n")
	for _, column := range result.Columns {
		builder.WriteString("    [#text(")
		builder.WriteString(quoteTypstString(column))
		builder.WriteString(", weight: \"bold\", size: 7.8pt)],\n")
	}
	builder.WriteString("  ),\n")
	for _, row := range result.Rows {
		for index := range result.Columns {
			builder.WriteString("  ")
			if index >= len(row) || row[index] == nil {
				builder.WriteString("[#text(\"NULL\", fill: rgb(\"#879498\"))],\n")
				continue
			}
			builder.WriteString("[#text(")
			builder.WriteString(quoteTypstString(fmt.Sprint(row[index])))
			builder.WriteString(")],\n")
		}
	}
	builder.WriteString(")\n")
	if result.Truncated {
		builder.WriteString("\n_报告表格仅显示前 2,000 行。_\n")
	}
	builder.WriteString("```\n")
	return builder.String()
}

func escapeMarkdownCell(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `|`, `\|`)
	value = strings.ReplaceAll(value, "\r\n", "<br>")
	value = strings.ReplaceAll(value, "\n", "<br>")
	return value
}

func columnIsNumeric(result model.QueryResult, column int) bool {
	found := false
	for _, row := range result.Rows {
		if column >= len(row) || row[column] == nil {
			continue
		}
		found = true
		switch row[column].(type) {
		case int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64,
			float32, float64:
		default:
			return false
		}
	}
	return found
}

func quoteTypstString(value string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\r", `\r`,
		"\n", `\n`,
		"\t", `\t`,
	)
	return `"` + replacer.Replace(value) + `"`
}

func DeclaredParams(content string) []string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return nil
	}
	frontMatterEnd := -1
	for index := 1; index < len(lines); index++ {
		if strings.TrimSpace(lines[index]) == "---" {
			frontMatterEnd = index
			break
		}
	}
	if frontMatterEnd < 0 {
		return nil
	}

	paramsIndent := -1
	seen := make(map[string]struct{})
	var result []string
	for index := 1; index < frontMatterEnd; index++ {
		line := lines[index]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if paramsIndent < 0 {
			if trimmed == "params:" {
				paramsIndent = indent
			}
			continue
		}
		if indent <= paramsIndent {
			break
		}
		if indent != paramsIndent+2 {
			continue
		}
		key, _, ok := strings.Cut(trimmed, ":")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func ApplyDeclaredParams(content string, params map[string]string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return content
	}
	frontMatterEnd := -1
	for index := 1; index < len(lines); index++ {
		if strings.TrimSpace(lines[index]) == "---" {
			frontMatterEnd = index
			break
		}
	}
	if frontMatterEnd < 0 {
		return content
	}
	paramsIndent := -1
	for index := 1; index < frontMatterEnd; index++ {
		line := lines[index]
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if paramsIndent < 0 {
			if trimmed == "params:" {
				paramsIndent = indent
			}
			continue
		}
		if indent <= paramsIndent {
			break
		}
		if indent != paramsIndent+2 {
			continue
		}
		key, _, ok := strings.Cut(trimmed, ":")
		key = strings.TrimSpace(key)
		if !ok {
			continue
		}
		value, exists := params[key]
		if !exists {
			continue
		}
		lines[index] = strings.Repeat(" ", indent) + key + ": " + quoteYAMLString(value)
	}
	return strings.Join(lines, "\n")
}

func quoteYAMLString(value string) string {
	return `"` + strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\n", `\n`,
		"\r", `\r`,
	).Replace(value) + `"`
}

func copyTemplateAssets(root, templatePath, runDir string) error {
	resolved, err := workflow.ResolveWorkspacePath(root, templatePath)
	if err != nil {
		return err
	}
	assetDir := filepath.Join(filepath.Dir(resolved), "assets")
	info, err := os.Stat(assetDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect template assets: %w", err)
	}
	if !info.IsDir() {
		return nil
	}
	if err := os.CopyFS(filepath.Join(runDir, "assets"), os.DirFS(assetDir)); err != nil {
		return fmt.Errorf("copy template assets: %w", err)
	}
	return nil
}

func splitLog(value string) []string {
	lines := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			result = append(result, line)
		}
	}
	return result
}

func commandVersion(ctx context.Context, binary string) string {
	commandCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(commandCtx, binary, "--version").CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func hasExecutable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
