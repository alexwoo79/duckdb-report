package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/alexwoo79/duckdb-report/internal/model"
)

var variablePattern = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.-]+)\s*\}\}`)

var (
	ErrWorkflowInvalid = errors.New("workflow is invalid")
	ErrStepInvalid     = errors.New("workflow step is invalid")
	ErrVariableMissing = errors.New("workflow variable is missing")
)

func Validate(w model.Workflow) error {
	if strings.TrimSpace(w.Name) == "" {
		return fmt.Errorf("%w: name is required", ErrWorkflowInvalid)
	}
	if len(w.Steps) == 0 {
		return fmt.Errorf("%w: at least one step is required", ErrWorkflowInvalid)
	}

	presets := make(map[string]model.Preset, len(w.Presets))
	for _, preset := range w.Presets {
		if strings.TrimSpace(preset.ID) == "" {
			return fmt.Errorf("%w: preset id is required", ErrWorkflowInvalid)
		}
		if _, exists := presets[preset.ID]; exists {
			return fmt.Errorf("%w: duplicate preset %q", ErrWorkflowInvalid, preset.ID)
		}
		presets[preset.ID] = preset
	}

	stepIDs := make(map[string]struct{}, len(w.Steps))
	for _, step := range w.Steps {
		if strings.TrimSpace(step.ID) == "" {
			return fmt.Errorf("%w: step id is required", ErrWorkflowInvalid)
		}
		if _, exists := stepIDs[step.ID]; exists {
			return fmt.Errorf("%w: duplicate step %q", ErrWorkflowInvalid, step.ID)
		}
		stepIDs[step.ID] = struct{}{}
		switch step.Kind {
		case model.StepImport, model.StepSQL, model.StepRender, model.StepExport:
		default:
			return fmt.Errorf("%w: unsupported kind %q", ErrStepInvalid, step.Kind)
		}
		if _, exists := presets[step.PresetID]; !exists {
			return fmt.Errorf("%w: preset %q does not exist", ErrStepInvalid, step.PresetID)
		}
	}
	return nil
}

func ParamsForStep(w model.Workflow, step model.Step, runtime map[string]string) (map[string]string, error) {
	var preset *model.Preset
	for i := range w.Presets {
		if w.Presets[i].ID == step.PresetID {
			preset = &w.Presets[i]
			break
		}
	}
	if preset == nil {
		return nil, fmt.Errorf("%w: preset %q does not exist", ErrStepInvalid, step.PresetID)
	}

	params := make(map[string]string, len(preset.Params)+len(step.Overrides)+len(runtime))
	for key, value := range preset.Params {
		params[key] = value
	}
	for key, value := range step.Overrides {
		params[key] = value
	}
	for key, value := range runtime {
		params[key] = value
	}
	return params, nil
}

func ResolveTemplate(input string, params map[string]string) (string, error) {
	var missing []string
	result := variablePattern.ReplaceAllStringFunc(input, func(match string) string {
		parts := variablePattern.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		value, ok := params[parts[1]]
		if !ok {
			missing = append(missing, parts[1])
			return match
		}
		return value
	})
	if len(missing) > 0 {
		sort.Strings(missing)
		return "", fmt.Errorf("%w: %s", ErrVariableMissing, strings.Join(missing, ", "))
	}
	return result, nil
}

func ResolveWorkspacePath(root, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%w: path is empty", ErrStepInvalid)
	}
	path := value
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)

	if !filepath.IsAbs(value) {
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return "", fmt.Errorf("resolve workspace path: %w", err)
		}
		if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("%w: path escapes workspace", ErrStepInvalid)
		}
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("open workflow file %q: %w", path, err)
	}
	return path, nil
}

func DefaultWorkflow() model.Workflow {
	return model.Workflow{
		ID:          "demo-monthly",
		Name:        "月度经营分析",
		Description: "导入销售明细，执行 SQL 聚合，并渲染为可预览的经营报告。",
		Runtime: map[string]string{
			"title":      "月度经营分析",
			"start_date": "2026-01-01",
			"end_date":   "2026-12-31",
		},
		Presets: []model.Preset{
			{
				ID:          "source-sales",
				Type:        model.PresetDataSource,
				Name:        "销售明细 CSV",
				Description: "示例数据源，导入时会在工作库中重建 sales 表。",
				Params: map[string]string{
					"kind":  "csv",
					"path":  "data/sales.csv",
					"table": "sales",
				},
			},
			{
				ID:          "sql-monthly",
				Type:        model.PresetSQLSet,
				Name:        "月度经营口径",
				Description: "按月份、区域与产品汇总收入、成本和毛利。",
				Params: map[string]string{
					"mode":       "file",
					"file":       "sql/monthly_analysis.sql",
					"inline_sql": "",
					"query_file": "sql/monthly_result.sql",
					"limit":      "500",
				},
			},
			{
				ID:          "report-monthly",
				Type:        model.PresetReport,
				Name:        "经营月报模板",
				Description: "Quarto QMD 模板，默认输出自包含 HTML。",
				Params: map[string]string{
					"template": "templates/monthly_report.qmd",
					"format":   "html",
					"query":    "SELECT strftime(month, '%Y-%m') AS month, region, product, orders, revenue, cost, gross_profit, gross_margin_pct FROM monthly_summary ORDER BY month, region, product",
				},
			},
			{
				ID:          "output-default",
				Type:        model.PresetOutput,
				Name:        "报告与数据发布包",
				Description: "将报告、数据库快照和 QMD 源文件打包为 ZIP。",
				Params: map[string]string{
					"filename":         "report-package",
					"directory":        "exports",
					"include_report":   "true",
					"include_database": "true",
					"include_source":   "true",
				},
			},
		},
		Steps: []model.Step{
			{ID: "import", Name: "导入销售明细", Kind: model.StepImport, PresetID: "source-sales", Enabled: true},
			{ID: "analyze", Name: "计算月度汇总", Kind: model.StepSQL, PresetID: "sql-monthly", Enabled: true},
			{ID: "render", Name: "渲染经营报告", Kind: model.StepRender, PresetID: "report-monthly", Enabled: true},
			{ID: "export", Name: "打包报告与数据", Kind: model.StepExport, PresetID: "output-default", Enabled: true},
		},
	}
}
