package workspace

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/alexwoo79/duckdb-report/internal/model"
	"github.com/alexwoo79/duckdb-report/internal/workflow"
)

//go:embed all:starters
var starterFS embed.FS

var safeIDPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

const legacyDemoSalesHash = "2fac6de44239defc1cef625c2f2ea9375f59e1a2705d377c0405e8d4f6ca672a"

type Store struct {
	Root         string
	Database     string
	Runs         string
	Data         string
	SQL          string
	Templates    string
	Workflows    string
	SourcesFile  string
	WorkflowFile string
}

func New(root string, exampleFS fs.FS) (*Store, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}
	store := &Store{
		Root:         absolute,
		Database:     filepath.Join(absolute, "workspace.duckdb"),
		Runs:         filepath.Join(absolute, "runs"),
		Data:         filepath.Join(absolute, "data"),
		SQL:          filepath.Join(absolute, "sql"),
		Templates:    filepath.Join(absolute, "templates"),
		Workflows:    filepath.Join(absolute, "workflows"),
		SourcesFile:  filepath.Join(absolute, "sources.json"),
		WorkflowFile: filepath.Join(absolute, "workflows", "current.json"),
	}
	for _, directory := range []string{store.Data, store.SQL, store.Templates, store.Workflows, store.Runs} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return nil, fmt.Errorf("create workspace directory %q: %w", directory, err)
		}
	}

	seed, err := fs.Sub(starterFS, "starters")
	if err != nil {
		return nil, fmt.Errorf("open starter assets: %w", err)
	}
	if err := copyMissing(seed, absolute); err != nil {
		return nil, err
	}
	if exampleFS != nil {
		if err := copyMissingSub(exampleFS, "example", filepath.Join(absolute, "examples", "monthly_sales")); err != nil &&
			!errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	if _, err := os.Stat(store.WorkflowFile); errors.Is(err, os.ErrNotExist) {
		if err := store.SaveWorkflow(workflow.DefaultWorkflow()); err != nil {
			return nil, err
		}
	}
	if err := upgradeLegacyDefaults(store, seed); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) LoadWorkflow() (model.Workflow, error) {
	data, err := os.ReadFile(s.WorkflowFile)
	if err != nil {
		return model.Workflow{}, fmt.Errorf("read workflow: %w", err)
	}
	var result model.Workflow
	if err := json.Unmarshal(data, &result); err != nil {
		return model.Workflow{}, fmt.Errorf("decode workflow: %w", err)
	}
	if err := workflow.Validate(result); err != nil {
		return model.Workflow{}, err
	}
	return result, nil
}

func (s *Store) SaveWorkflow(value model.Workflow) error {
	if err := workflow.Validate(value); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode workflow: %w", err)
	}
	if err := os.WriteFile(s.WorkflowFile, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write workflow: %w", err)
	}
	return nil
}

func (s *Store) LoadSources() ([]model.Source, error) {
	data, err := os.ReadFile(s.SourcesFile)
	if errors.Is(err, os.ErrNotExist) {
		return []model.Source{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read data sources: %w", err)
	}
	var result []model.Source
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode data sources: %w", err)
	}
	if result == nil {
		result = []model.Source{}
	}
	return result, nil
}

func (s *Store) SaveSource(source model.Source) error {
	sources, err := s.LoadSources()
	if err != nil {
		return err
	}
	replaced := false
	for i := range sources {
		if sources[i].Table == source.Table && sources[i].Kind == source.Kind {
			sources[i] = source
			replaced = true
			break
		}
	}
	if !replaced {
		sources = append(sources, source)
	}
	return s.SaveSources(sources)
}

func (s *Store) SaveSources(sources []model.Source) error {
	if sources == nil {
		sources = []model.Source{}
	}
	sort.SliceStable(sources, func(i, j int) bool {
		return sources[i].Table < sources[j].Table
	})
	data, err := json.MarshalIndent(sources, "", "  ")
	if err != nil {
		return fmt.Errorf("encode data sources: %w", err)
	}
	if err := os.WriteFile(s.SourcesFile, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write data sources: %w", err)
	}
	return nil
}

func (s *Store) RemoveSource(source model.Source) error {
	sources, err := s.LoadSources()
	if err != nil {
		return err
	}
	filtered := sources[:0]
	for _, candidate := range sources {
		if candidate.Table == source.Table && candidate.Kind == source.Kind {
			continue
		}
		filtered = append(filtered, candidate)
	}
	return s.SaveSources(filtered)
}

func (s *Store) ClearSources() error {
	return s.SaveSources(nil)
}

func (s *Store) ResolveExisting(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%w: path is empty", workflow.ErrStepInvalid)
	}
	path := value
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.Root, path)
	}
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("open file %q: %w", path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("open file %q: path is a directory", path)
	}
	return path, nil
}

func (s *Store) ResolveWorkspace(value string) (string, error) {
	return workflow.ResolveWorkspacePath(s.Root, value)
}

func (s *Store) Relative(path string) string {
	relative, err := filepath.Rel(s.Root, path)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return path
	}
	return filepath.ToSlash(relative)
}

func (s *Store) ReadText(path string) (model.FileContent, error) {
	resolved, err := s.ResolveExisting(path)
	if err != nil {
		return model.FileContent{}, err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return model.FileContent{}, fmt.Errorf("read text file: %w", err)
	}
	return model.FileContent{Path: s.Relative(resolved), Content: string(data)}, nil
}

func (s *Store) WriteText(path, content string) (model.FileContent, error) {
	resolved, err := s.resolveWritable(path)
	if err != nil {
		return model.FileContent{}, err
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return model.FileContent{}, fmt.Errorf("create file directory: %w", err)
	}
	if err := os.WriteFile(resolved, []byte(content), 0o644); err != nil {
		return model.FileContent{}, fmt.Errorf("write text file: %w", err)
	}
	return model.FileContent{Path: s.Relative(resolved), Content: content}, nil
}

func (s *Store) ListSQL() ([]model.FileRef, error) {
	return s.listFiles([]string{".sql"}, []string{s.SQL, filepath.Join(s.Root, "examples")})
}

func (s *Store) ListTemplates() ([]model.FileRef, error) {
	return s.listFiles([]string{".qmd"}, []string{s.Templates, filepath.Join(s.Root, "examples")})
}

func (s *Store) ListWorkflows() ([]model.FileRef, error) {
	return s.listFiles([]string{".json"}, []string{s.Workflows})
}

func (s *Store) ListRuns() ([]model.RunInfo, error) {
	entries, err := os.ReadDir(s.Runs)
	if err != nil {
		return nil, fmt.Errorf("read report runs: %w", err)
	}
	result := make([]model.RunInfo, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !safeIDPattern.MatchString(entry.Name()) {
			continue
		}
		dir := filepath.Join(s.Runs, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		run := model.RunInfo{ID: entry.Name(), Name: entry.Name(), ModifiedAt: info.ModTime().Unix()}
		for _, extension := range []string{".html", ".pdf"} {
			matches, _ := filepath.Glob(filepath.Join(dir, "report*"+extension))
			sort.Strings(matches)
			if len(matches) == 0 {
				continue
			}
			path := s.Relative(matches[0])
			if extension == ".html" {
				run.HTMLPath = path
			} else {
				run.PDFPath = path
			}
		}
		result = append(result, run)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ModifiedAt > result[j].ModifiedAt
	})
	if len(result) > 30 {
		result = result[:30]
	}
	return result, nil
}

func (s *Store) NewRun(name string) (string, string, error) {
	slug := sanitizeSlug(name)
	if slug == "" {
		slug = "report"
	}
	id := time.Now().Format("20060102-150405") + "-" + slug
	if !safeIDPattern.MatchString(id) {
		return "", "", fmt.Errorf("invalid run id")
	}
	dir := filepath.Join(s.Runs, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("create report run: %w", err)
	}
	return id, dir, nil
}

func (s *Store) RunDir(id string) (string, error) {
	if !safeIDPattern.MatchString(id) {
		return "", fmt.Errorf("invalid run id")
	}
	dir := filepath.Join(s.Runs, id)
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("open report run: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("report run is not a directory")
	}
	return dir, nil
}

func (s *Store) resolveWritable(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("file path is empty")
	}
	if filepath.IsAbs(value) {
		return "", fmt.Errorf("absolute paths are not allowed for workspace files")
	}
	resolved := filepath.Clean(filepath.Join(s.Root, value))
	relative, err := filepath.Rel(s.Root, resolved)
	if err != nil {
		return "", fmt.Errorf("resolve file path: %w", err)
	}
	if relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("file path escapes workspace")
	}
	return resolved, nil
}

func (s *Store) listFiles(extensions, roots []string) ([]model.FileRef, error) {
	allowed := make(map[string]struct{}, len(extensions))
	for _, extension := range extensions {
		allowed[strings.ToLower(extension)] = struct{}{}
	}
	seen := make(map[string]struct{})
	var result []model.FileRef
	for _, root := range roots {
		if _, err := os.Stat(root); err != nil {
			continue
		}
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == "assets" || entry.Name() == "fonts" || entry.Name() == ".Rlib" {
					return filepath.SkipDir
				}
				return nil
			}
			if _, ok := allowed[strings.ToLower(filepath.Ext(entry.Name()))]; !ok {
				return nil
			}
			relative := s.Relative(path)
			if _, ok := seen[relative]; ok {
				return nil
			}
			seen[relative] = struct{}{}
			result = append(result, model.FileRef{Name: entry.Name(), Path: relative})
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("list files in %q: %w", root, err)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Path < result[j].Path
	})
	return result, nil
}

func copyMissing(source fs.FS, destination string) error {
	return copyMissingSub(source, ".", destination)
}

func copyMissingSub(source fs.FS, sourceRoot, destination string) error {
	return fs.WalkDir(source, sourceRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk starter assets: %w", walkErr)
		}
		if path == sourceRoot {
			return nil
		}
		relative, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return fmt.Errorf("resolve starter path: %w", err)
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create starter directory: %w", err)
			}
			return nil
		}
		if _, err := os.Stat(target); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect starter file: %w", err)
		}
		data, err := fs.ReadFile(source, path)
		if err != nil {
			return fmt.Errorf("read starter file %q: %w", path, err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create starter parent directory: %w", err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return fmt.Errorf("write starter file: %w", err)
		}
		return nil
	})
}

func sanitizeSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	previousDash := false
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z',
			character >= '0' && character <= '9':
			builder.WriteRune(character)
			previousDash = false
		case character == '-' || character == '_' || character == ' ':
			if !previousDash && builder.Len() > 0 {
				builder.WriteByte('-')
				previousDash = true
			}
		}
	}
	return strings.Trim(builder.String(), "-")
}

func upgradeLegacyDefaults(store *Store, seed fs.FS) error {
	templatePath := filepath.Join(store.Templates, "monthly_report.qmd")
	currentTemplate, err := os.ReadFile(templatePath)
	if err == nil &&
		bytes.Contains(currentTemplate, []byte(`subtitle: "DuckDB 本地分析工作流"`)) &&
		bytes.Contains(currentTemplate, []byte("本报告由 **DuckDB + SQL + Quarto** 自动生成")) &&
		!bytes.Contains(currentTemplate, []byte("mainfont: \"PingFang SC\"")) &&
		!bytes.Contains(currentTemplate, []byte("assets/report.css")) {
		replacement, readErr := fs.ReadFile(seed, "templates/monthly_report.qmd")
		if readErr != nil {
			return fmt.Errorf("read upgraded report template: %w", readErr)
		}
		if writeErr := os.WriteFile(templatePath, replacement, 0o644); writeErr != nil {
			return fmt.Errorf("upgrade report template: %w", writeErr)
		}
		currentTemplate = replacement
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect report template: %w", err)
	}
	if err == nil && bytes.Contains(currentTemplate, []byte(`mainfont: "PingFang SC"`)) {
		currentTemplate = bytes.ReplaceAll(
			currentTemplate,
			[]byte(`mainfont: "PingFang SC"`),
			[]byte(`mainfont: "{{report_font}}"`),
		)
		if writeErr := os.WriteFile(templatePath, currentTemplate, 0o644); writeErr != nil {
			return fmt.Errorf("upgrade report font: %w", writeErr)
		}
	}
	if err := upgradeLegacyDemoOwners(store, seed); err != nil {
		return err
	}

	resultSQLPath := filepath.Join(store.SQL, "monthly_result.sql")
	currentSQL, err := os.ReadFile(resultSQLPath)
	if err == nil && bytes.Contains(currentSQL, []byte("SELECT *\nFROM monthly_summary\nORDER BY month")) {
		replacement, readErr := fs.ReadFile(seed, "sql/monthly_result.sql")
		if readErr != nil {
			return fmt.Errorf("read upgraded result sql: %w", readErr)
		}
		if writeErr := os.WriteFile(resultSQLPath, replacement, 0o644); writeErr != nil {
			return fmt.Errorf("upgrade result sql: %w", writeErr)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect result sql: %w", err)
	}

	analysisSQLPath := filepath.Join(store.SQL, "monthly_analysis.sql")
	currentAnalysisSQL, err := os.ReadFile(analysisSQLPath)
	if err == nil && bytes.Contains(currentAnalysisSQL, []byte("CREATE OR REPLACE TABLE kpi_summary")) {
		currentAnalysisSQL = bytes.ReplaceAll(
			currentAnalysisSQL,
			[]byte("CREATE OR REPLACE TABLE kpi_summary"),
			[]byte("CREATE OR REPLACE TABLE monthly_kpi_summary"),
		)
		if writeErr := os.WriteFile(analysisSQLPath, currentAnalysisSQL, 0o644); writeErr != nil {
			return fmt.Errorf("upgrade analysis SQL table name: %w", writeErr)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect analysis sql: %w", err)
	}

	currentWorkflow, err := os.ReadFile(store.WorkflowFile)
	if err != nil {
		return fmt.Errorf("inspect workflow for upgrades: %w", err)
	}
	legacyQuery := "SELECT * FROM monthly_summary ORDER BY month, region, product"
	if bytes.Contains(currentWorkflow, []byte(legacyQuery)) {
		upgraded := bytes.ReplaceAll(
			currentWorkflow,
			[]byte(legacyQuery),
			[]byte("SELECT strftime(month, '%Y-%m') AS month, region, product, orders, revenue, cost, gross_profit, gross_margin_pct FROM monthly_summary ORDER BY month, region, product"),
		)
		if err := os.WriteFile(store.WorkflowFile, upgraded, 0o644); err != nil {
			return fmt.Errorf("upgrade workflow report query: %w", err)
		}
	}
	return nil
}

func upgradeLegacyDemoOwners(store *Store, seed fs.FS) error {
	return migrateLegacyDemoSalesFile(
		filepath.Join(store.Data, "sales.csv"),
		seed,
		legacyDemoSalesHash,
		filepath.Join(store.Root, ".demo-sales-owners-v1"),
	)
}

func migrateLegacyDemoSalesFile(path string, seed fs.FS, legacyHash, marker string) error {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect demo sales data: %w", err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(content)) != legacyHash {
		return nil
	}
	replacement, err := fs.ReadFile(seed, "data/sales.csv")
	if err != nil {
		return fmt.Errorf("read sanitized demo sales data: %w", err)
	}
	if err := os.WriteFile(path, replacement, 0o644); err != nil {
		return fmt.Errorf("upgrade demo sales owner names: %w", err)
	}
	if err := os.WriteFile(marker, []byte("reimport-pending\n"), 0o644); err != nil {
		return fmt.Errorf("mark demo sales owner migration: %w", err)
	}
	return nil
}
