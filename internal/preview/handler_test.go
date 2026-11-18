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
	handler := NewHandler(func() *workspace.Store {
		return &workspace.Store{Root: root, Runs: runs}
	})
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

func TestHandlerUsesCurrentStore(t *testing.T) {
	t.Parallel()
	createStore := func(content string) (*workspace.Store, string) {
		t.Helper()
		root := t.TempDir()
		runs := filepath.Join(root, "runs")
		runDir := filepath.Join(runs, "run-1")
		if err := os.MkdirAll(runDir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(runDir, "report.html")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return &workspace.Store{Root: root, Runs: runs}, content
	}
	first, firstContent := createStore("first")
	second, secondContent := createStore("second")
	current := first
	handler := NewHandler(func() *workspace.Store {
		return current
	})

	serve := func() string {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "/preview/run-1/report.html", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
		}
		return recorder.Body.String()
	}

	if body := serve(); body != firstContent {
		t.Fatalf("first body = %q", body)
	}
	current = second
	if body := serve(); body != secondContent {
		t.Fatalf("second body = %q", body)
	}
}

func TestHandlerRejectsTraversal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runs := filepath.Join(root, "runs")
	if err := os.MkdirAll(filepath.Join(runs, "run-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(func() *workspace.Store {
		return &workspace.Store{Root: root, Runs: runs}
	})
	request := httptest.NewRequest(http.MethodGet, "/preview/run-1/%2e%2e/secret.txt", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}
