package report

import (
	"os"
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
