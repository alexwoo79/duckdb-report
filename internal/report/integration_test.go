//go:build integration

package report

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/alexwoo79/duckdb-report/internal/model"
	"github.com/alexwoo79/duckdb-report/internal/workspace"
)

func TestQuartoScriptReportsIntegration(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")
	t.Setenv("LANG", "")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_CTYPE", "")
	runtimePathOnce = sync.Once{}

	root := t.TempDir()
	store, err := workspace.New(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	renderer := NewRenderer("", store, nil)
	if renderer.quarto == "" {
		t.Skip("Quarto is not installed")
	}

	tests := []struct {
		name        string
		content     string
		outputToken string
	}{
		{
			name: "python defaults",
			content: `---
title: "{{title}}"
params:
  top_n: 8
format: html
---

` + "```{python}\nprint({{top_n}})\n```\n",
			outputToken: "8",
		},
		{
			name: "legacy r top n fallback",
			content: `---
title: "{{title}}"
format: html
---

` + "```{r}\ncat(\"{{top_n}} 区域 办公家具\")\n```\n",
			outputToken: "区域 办公家具",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := renderer.Render(t.Context(), model.RenderRequest{
				Content: test.content,
				Format:  "html",
				Params:  map[string]string{},
				SaveAs:  test.name,
			})
			if err != nil {
				t.Fatal(err)
			}
			runDir, err := store.RunDir(result.RunID)
			if err != nil {
				t.Fatal(err)
			}
			qmd, err := os.ReadFile(filepath.Join(runDir, "report.qmd"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(qmd), "{{top_n}}") {
				t.Fatalf("top_n was not resolved:\n%s", qmd)
			}
			html, err := os.ReadFile(filepath.Join(runDir, "report.html"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(html), test.outputToken) {
				t.Fatalf("rendered report does not contain %q", test.outputToken)
			}
			if strings.Contains(string(html), "&lt;U+") {
				t.Fatal("R locale converted Chinese text to Unicode escape notation")
			}
		})
	}
}
