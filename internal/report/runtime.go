package report

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

var (
	rCodeChunk       = regexp.MustCompile("(?m)^```\\s*\\{\\s*r(?:\\s|[,}])")
	pythonCodeChunk  = regexp.MustCompile("(?m)^```\\s*\\{\\s*python(?:\\s|[,}])")
	templateVariable = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.-]+)\s*\}\}`)
	primaryTable     = regexp.MustCompile(`\{\{\s*table\s*\}\}`)
	namedTable       = regexp.MustCompile(`\{\{\s*table_([a-zA-Z0-9_.-]+)\s*\}\}`)
)

type ScriptRequirements struct {
	NeedsR      bool `json:"needsR"`
	NeedsPython bool `json:"needsPython"`
}

var runtimePathOnce sync.Once

// EnsureRuntimePath adds common desktop tool locations that GUI-launched apps
// do not inherit from the user's interactive shell.
func EnsureRuntimePath() {
	runtimePathOnce.Do(func() {
		ensureUTF8Environment()
		path := mergePathEntries(os.Getenv("PATH"), runtimePathEntries())
		if path != "" {
			_ = os.Setenv("PATH", path)
		}
	})
}

func ensureUTF8Environment() {
	if runtime.GOOS == "windows" {
		if strings.TrimSpace(os.Getenv("PYTHONUTF8")) == "" {
			_ = os.Setenv("PYTHONUTF8", "1")
		}
		return
	}
	for _, key := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
		if strings.Contains(value, "utf-8") || strings.Contains(value, "utf8") {
			return
		}
	}
	_ = os.Setenv("LANG", "C.UTF-8")
	_ = os.Setenv("LC_CTYPE", "C.UTF-8")
}

func runtimePathEntries() []string {
	home, _ := os.UserHomeDir()
	candidates := []string{
		"/opt/homebrew/bin",
		"/usr/local/bin",
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".pyenv", "shims"),
		filepath.Join(home, "miniconda3", "bin"),
		filepath.Join(home, "miniforge3", "bin"),
		filepath.Join(home, "anaconda3", "bin"),
		filepath.Join(home, "mambaforge", "bin"),
	}
	switch runtime.GOOS {
	case "darwin":
		candidates = append(candidates,
			"/Applications/quarto/bin",
			"/Library/Frameworks/R.framework/Resources/bin",
		)
		candidates = append(candidates, globDirectories(
			filepath.Join(home, "Library", "Python", "3.*", "bin"),
			filepath.Join(home, "Library", "Frameworks", "Python.framework", "Versions", "*", "bin"),
			"/Library/Frameworks/Python.framework/Versions/*/bin",
		)...)
	case "linux":
		candidates = append(candidates,
			linuxRuntimePathCandidates(home, os.Getenv("XDG_DATA_HOME"))...,
		)
		candidates = append(candidates, globDirectories(
			"/opt/R/*/bin",
			"/opt/quarto-*/bin",
		)...)
	case "windows":
		localAppData := os.Getenv("LOCALAPPDATA")
		appData := os.Getenv("APPDATA")
		programFiles := os.Getenv("ProgramFiles")
		candidates = append(candidates,
			filepath.Join(localAppData, "Programs", "Quarto", "bin"),
		)
		candidates = append(candidates, globDirectories(
			filepath.Join(localAppData, "Programs", "Python", "Python*"),
			filepath.Join(localAppData, "Programs", "Python", "Python*", "Scripts"),
			filepath.Join(appData, "Python", "Python*", "Scripts"),
		)...)
		candidates = append(candidates, globDirectories(
			filepath.Join(programFiles, "R", "R-*", "bin", "x64"),
		)...)
	}

	existing := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if !filepath.IsAbs(candidate) {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			existing = append(existing, filepath.Clean(candidate))
		}
	}
	return existing
}

func linuxRuntimePathCandidates(home, xdgDataHome string) []string {
	dataHome := strings.TrimSpace(xdgDataHome)
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}
	return []string{
		"/opt/quarto/bin",
		filepath.Join(home, "opt", "quarto", "bin"),
		filepath.Join(home, ".local", "opt", "quarto", "bin"),
		filepath.Join(dataHome, "quarto", "bin"),
		filepath.Join(dataHome, "duckdb-report", "jupyter", "bin"),
		"/usr/lib/R/bin",
		"/usr/lib64/R/bin",
		filepath.Join(home, ".local", "share", "rstudio", "bin", "quarto", "bin"),
	}
}

func globDirectories(patterns ...string) []string {
	var result []string
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		result = append(result, matches...)
	}
	return result
}

func mergePathEntries(current string, preferred []string) string {
	entries := make([]string, 0, len(preferred)+4)
	seen := make(map[string]struct{}, len(preferred)+4)
	for _, entry := range preferred {
		entry = filepath.Clean(strings.TrimSpace(entry))
		if entry == "." || !filepath.IsAbs(entry) {
			continue
		}
		if _, exists := seen[entry]; exists {
			continue
		}
		seen[entry] = struct{}{}
		entries = append(entries, entry)
	}
	for _, entry := range filepath.SplitList(current) {
		entry = filepath.Clean(strings.TrimSpace(entry))
		if entry == "" || entry == "." {
			continue
		}
		if _, exists := seen[entry]; exists {
			continue
		}
		seen[entry] = struct{}{}
		entries = append(entries, entry)
	}
	return strings.Join(entries, string(os.PathListSeparator))
}

func DetectRequirements(content string) ScriptRequirements {
	return ScriptRequirements{
		NeedsR:      rCodeChunk.MatchString(content),
		NeedsPython: pythonCodeChunk.MatchString(content),
	}
}

func validateScriptRuntime(content string) error {
	requirements := DetectRequirements(content)
	if requirements.NeedsR {
		if _, err := exec.LookPath("Rscript"); err != nil {
			return errors.New("qmd contains R code but Rscript is unavailable; install R or use the {{table}} data bridge")
		}
	}
	if requirements.NeedsPython {
		if _, err := exec.LookPath("jupyter"); err != nil {
			return errors.New("qmd contains Python code but Jupyter is unavailable; configure Quarto/Jupyter or use the {{table}} data bridge")
		}
	}
	return nil
}
