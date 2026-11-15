package preview

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexwoo79/duckdb-report/internal/workspace"
)

func TestHandlerServesRunFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runs := filepath.Join(root, "runs")
	runDir := filepath.Join(runs, "run-1")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "report.html"), []byte("<h1>report</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(&workspace.Store{Root: root, Runs: runs})
	request := httptest.NewRequest(http.MethodGet, "/preview/run-1/report.html", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.String() != "<h1>report</h1>" {
		t.Fatalf("body = %q", recorder.Body.String())
	}
}

func TestHandlerRejectsTraversal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runs := filepath.Join(root, "runs")
	if err := os.MkdirAll(filepath.Join(runs, "run-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(&workspace.Store{Root: root, Runs: runs})
	request := httptest.NewRequest(http.MethodGet, "/preview/run-1/%2e%2e/secret.txt", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}
