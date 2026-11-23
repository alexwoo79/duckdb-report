package database

import (
	"runtime"
	"testing"
)

func TestDuckDBThreads(t *testing.T) {
	t.Setenv("DUCKDB_REPORT_THREADS", "3")
	if got := duckDBThreads(); got != 3 {
		t.Fatalf("duckDBThreads() = %d, want 3", got)
	}

	t.Setenv("DUCKDB_REPORT_THREADS", "invalid")
	if got, want := duckDBThreads(), max(1, runtime.NumCPU()/2); got != want {
		t.Fatalf("duckDBThreads() fallback = %d, want %d", got, want)
	}
}

func TestDuckDBMemoryLimit(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{value: "3.5GB", want: "3.5GB"},
		{value: "65%", want: "65%"},
		{value: "invalid", want: defaultDuckDBMemoryLimit},
		{value: "", want: defaultDuckDBMemoryLimit},
	}
	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			t.Setenv("DUCKDB_REPORT_MEMORY_LIMIT", test.value)
			if got := duckDBMemoryLimit(); got != test.want {
				t.Fatalf("duckDBMemoryLimit() = %q, want %q", got, test.want)
			}
		})
	}
}
