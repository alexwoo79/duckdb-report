package report

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestDetectRequirements(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		content     string
		needsR      bool
		needsPython bool
	}{
		{
			name:    "table bridge only",
			content: "# Report\n\n{{table}}\n",
		},
		{
			name:    "R chunk",
			content: "```{r setup}\nsummary(cars)\n```\n",
			needsR:  true,
		},
		{
			name:        "Python chunk",
			content:     "```{python}\nprint('ok')\n```\n",
			needsPython: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := DetectRequirements(test.content)
			if got.NeedsR != test.needsR || got.NeedsPython != test.needsPython {
				t.Fatalf("DetectRequirements() = %#v", got)
			}
		})
	}
}

func TestMergePathEntries(t *testing.T) {
	t.Parallel()
	current := strings.Join([]string{"/usr/bin", "/opt/homebrew/bin", "/custom/bin"}, string(os.PathListSeparator))
	preferred := []string{"/opt/homebrew/bin", "/custom/bin", "/tools/bin", "/tools/bin"}
	got := mergePathEntries(current, preferred)
	want := strings.Join([]string{"/opt/homebrew/bin", "/custom/bin", "/tools/bin", "/usr/bin"}, string(os.PathListSeparator))
	if got != want {
		t.Fatalf("mergePathEntries() = %q, want %q", got, want)
	}
}

func TestLinuxRuntimePathCandidates(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux path candidates are only evaluated on Linux")
	}
	t.Parallel()
	tests := []struct {
		name        string
		home        string
		xdgDataHome string
		want        []string
	}{
		{
			name: "default XDG data home",
			home: "/home/tester",
			want: []string{
				"/opt/quarto/bin",
				"/home/tester/opt/quarto/bin",
				"/home/tester/.local/opt/quarto/bin",
				"/home/tester/.local/share/quarto/bin",
				"/home/tester/.local/share/duckdb-report/jupyter/bin",
				"/usr/lib/R/bin",
				"/usr/lib64/R/bin",
				"/home/tester/.local/share/rstudio/bin/quarto/bin",
			},
		},
		{
			name:        "explicit XDG data home",
			home:        "/home/tester",
			xdgDataHome: "/data/tester",
			want: []string{
				"/opt/quarto/bin",
				"/home/tester/opt/quarto/bin",
				"/home/tester/.local/opt/quarto/bin",
				"/data/tester/quarto/bin",
				"/data/tester/duckdb-report/jupyter/bin",
				"/usr/lib/R/bin",
				"/usr/lib64/R/bin",
				"/home/tester/.local/share/rstudio/bin/quarto/bin",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := linuxRuntimePathCandidates(test.home, test.xdgDataHome)
			if strings.Join(got, string(os.PathListSeparator)) != strings.Join(test.want, string(os.PathListSeparator)) {
				t.Fatalf("linuxRuntimePathCandidates() = %#v, want %#v", got, test.want)
			}
		})
	}
}
