package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alexwoo79/duckdb-report/internal/model"
)

func TestStoreLoadDefaults(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := New(root, filepath.Join(root, "workspace"))
	value, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if value.DisplayName != "DuckDB Report Studio" || value.Workspace == "" {
		t.Fatalf("Load() = %#v", value)
	}
}

func TestStoreSaveAndLoad(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := New(root, filepath.Join(root, "default"))
	workspace := filepath.Join(root, "custom", "workspace")
	want := model.AppSettings{DisplayName: "My Reports", Workspace: workspace}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Load() = %#v, want %#v", got, want)
	}
	if _, err := os.Stat(filepath.Join(workspace, "..")); err != nil {
		t.Fatal(err)
	}
}

func TestStoreRejectsEmptyName(t *testing.T) {
	t.Parallel()
	store := New(t.TempDir(), t.TempDir())
	if err := store.Save(model.AppSettings{Workspace: t.TempDir()}); err == nil {
		t.Fatal("expected empty display name to fail")
	}
}
