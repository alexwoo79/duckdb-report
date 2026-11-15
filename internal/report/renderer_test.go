package report

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexwoo79/duckdb-report/internal/database"
	"github.com/alexwoo79/duckdb-report/internal/model"
	"github.com/alexwoo79/duckdb-report/internal/workspace"
)

func TestHTMLTable(t *testing.T) {
	t.Parallel()
	result := model.QueryResult{
		Columns: []string{"name", "value"},
		Rows: [][]any{
			{"<duck>", nil},
		},
		Truncated: true,
	}
	table := HTMLTable(result)
	if !strings.Contains(table, "&lt;duck&gt;") {
		t.Fatalf("HTMLTable() did not escape cell: %s", table)
	}
	if !strings.Contains(table, "NULL") {
		t.Fatalf("HTMLTable() did not render null: %s", table)
	}
	if !strings.Contains(table, "2,000") {
		t.Fatalf("HTMLTable() did not render truncation note: %s", table)
	}
}

func TestSplitLog(t *testing.T) {
	t.Parallel()
	got := splitLog("one\r\n\r\ntwo\n")
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("splitLog() = %#v", got)
	}
}

func TestMarkdownTable(t *testing.T) {
	t.Parallel()
	result := model.QueryResult{
		Columns: []string{"项目", "说明"},
		Rows: [][]any{
			{"A|B", "第一行\n第二行"},
			{nil, 42},
		},
	}
	table := MarkdownTable(result)
	for _, expected := range []string{
		"| 项目 | 说明 |",
		`A\|B`,
		"第一行<br>第二行",
		"| NULL | 42 |",
	} {
		if !strings.Contains(table, expected) {
			t.Fatalf("MarkdownTable() missing %q:\n%s", expected, table)
		}
	}
}

func TestTypstTable(t *testing.T) {
	t.Parallel()
	result := model.QueryResult{
		Columns: []string{"项目", "金额"},
		Rows: [][]any{
			{"测试", 128000},
			{nil, 42},
		},
	}
	table := TypstTable(result)
	for _, expected := range []string{
		"```{=typst}",
		"columns: (1fr,) * 2",
		"align: (left, right)",
		`[#text("项目", weight: "bold", size: 7.8pt)]`,
		`[#text("测试")]`,
		`[#text("NULL", fill: rgb("#879498"))]`,
	} {
		if !strings.Contains(table, expected) {
			t.Fatalf("TypstTable() missing %q:\n%s", expected, table)
		}
	}
}

func TestDeclaredParams(t *testing.T) {
	t.Parallel()
	content := `---
title: "Report"
params:
  leader: "default"
  database: "out/analysis.duckdb"
  top_n: 10
format:
  typst: default
---
`
	got := DeclaredParams(content)
	want := []string{"database", "leader", "top_n"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("DeclaredParams() = %#v, want %#v", got, want)
	}
}

func TestDefaultReportFont(t *testing.T) {
	t.Parallel()
	for goos, want := range map[string]string{
		"darwin":  "PingFang SC",
		"windows": "Microsoft YaHei",
		"linux":   "Noto Sans CJK SC",
	} {
		if got := defaultReportFont(goos); got != want {
			t.Errorf("defaultReportFont(%q) = %q, want %q", goos, got, want)
		}
	}
}

func TestDeclaredParamDefaults(t *testing.T) {
	t.Parallel()
	content := `---
title: "Report"
params:
  title: "默认标题"
  top_n: 8
format: html
---
`
	got := DeclaredParamDefaults(content)
	if got["title"] != "默认标题" || got["top_n"] != "8" {
		t.Fatalf("DeclaredParamDefaults() = %#v", got)
	}
}

func TestApplyReportParamDefaults(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		content  string
		saveName string
		params   map[string]string
		key      string
		want     string
	}{
		{
			name:    "legacy top n",
			content: "TOP_N={{top_n}}",
			params:  map[string]string{},
			key:     "top_n",
			want:    "8",
		},
		{
			name: "declared top n",
			content: `---
params:
  top_n: 3
---
{{top_n}}`,
			params: map[string]string{},
			key:    "top_n",
			want:   "3",
		},
		{
			name:    "runtime top n wins",
			content: "TOP_N={{top_n}}",
			params:  map[string]string{"top_n": "12"},
			key:     "top_n",
			want:    "12",
		},
		{
			name:     "save name becomes title",
			content:  "# {{title}}",
			saveName: "月度销售分析",
			params:   map[string]string{},
			key:      "title",
			want:     "月度销售分析",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			applyReportParamDefaults(test.content, test.saveName, test.params)
			if got := test.params[test.key]; got != test.want {
				t.Errorf("params[%q] = %q, want %q", test.key, got, test.want)
			}
		})
	}
}

func TestEnvironmentTreatsQuartoAsPDFCapable(t *testing.T) {
	t.Parallel()
	environment := NewRenderer("/missing/quarto", nil, nil).Environments(context.Background())
	if !environment.PDFAvailable {
		t.Fatal("Quarto/Typst should make PDF output available")
	}
}

func TestApplyDeclaredParams(t *testing.T) {
	t.Parallel()
	content := "---\ntitle: \"Report\"\nparams:\n  database: \"old.duckdb\"\n  title: \"Old\"\nformat: html\n---\n"
	got := ApplyDeclaredParams(content, map[string]string{
		"database": "/tmp/new path/report.duckdb",
		"title":    "新报告",
	})
	for _, expected := range []string{
		`database: "/tmp/new path/report.duckdb"`,
		`title: "新报告"`,
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("ApplyDeclaredParams() missing %q:\n%s", expected, got)
		}
	}
}

func TestRenderReplacesTablePlaceholderBeforeVariables(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, err := workspace.New(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(context.Background(), store.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	if _, err := db.Query(context.Background(), "CREATE TABLE report_data(value INTEGER); INSERT INTO report_data VALUES (42)", 10); err != nil {
		t.Fatal(err)
	}
	quarto := filepath.Join(root, "fake-quarto")
	script := "#!/bin/sh\nwhile [ \"$#\" -gt 0 ]; do if [ \"$1\" = \"--output\" ]; then shift; out=\"$1\"; fi; shift; done\nprintf '<html><body>ok</body></html>' > \"$out\"\n"
	if err := os.WriteFile(quarto, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	renderer := NewRenderer(quarto, store, db)
	result, err := renderer.Render(context.Background(), model.RenderRequest{
		Content: "---\ntitle: \"{{title}}\"\nformat: html\n---\n\n{{table}}\n",
		Format:  "html",
		Query:   "SELECT * FROM report_data",
		Params:  map[string]string{"title": "Regression"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(result.PreviewPath, "report.html") {
		t.Fatalf("PreviewPath = %q", result.PreviewPath)
	}
	runDir, err := store.RunDir(result.RunID)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := os.ReadFile(filepath.Join(runDir, "report.qmd"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rendered), "{{table}}") || !strings.Contains(string(rendered), "42") {
		t.Fatalf("rendered qmd was not prepared: %s", rendered)
	}
}

func TestRenderErrorIncludesQuartoLog(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, err := workspace.New(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	quarto := filepath.Join(root, "failing-quarto")
	script := "#!/bin/sh\necho 'missing R package: tinytable' >&2\nexit 1\n"
	if err := os.WriteFile(quarto, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	renderer := NewRenderer(quarto, store, nil)
	_, err = renderer.Render(context.Background(), model.RenderRequest{
		Content: "# Plain report\n",
		Format:  "html",
	})
	if err == nil || !strings.Contains(err.Error(), "missing R package: tinytable") {
		t.Fatalf("Render() error = %v, want Quarto log detail", err)
	}
}
