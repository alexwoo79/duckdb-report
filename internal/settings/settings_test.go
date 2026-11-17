package settings

import (
	"encoding/json"
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
	if value.DisplayName != "DuckDB Report Studio" ||
		value.Workspace == "" ||
		value.UIFontSize != 13 {
		t.Fatalf("Load() = %#v", value)
	}
}

func TestStoreSaveAndLoad(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := New(root, filepath.Join(root, "default"))
	workspace := filepath.Join(root, "custom", "workspace")
	want := model.AppSettings{
		DisplayName: "My Reports",
		Workspace:   workspace,
		UIFontSize:  15,
	}
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

func TestStoreLoadLegacySettingsWithoutUIFontSize(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	store := New(root, filepath.Join(root, "default"))
	data, err := json.Marshal(map[string]string{
		"displayName": "Legacy Reports",
		"workspace":   workspace,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.UIFontSize != 13 {
		t.Fatalf("UIFontSize = %d, want 13", got.UIFontSize)
	}
}

func TestStoreNormalizesUIFontSize(t *testing.T) {
	t.Parallel()
	store := New(t.TempDir(), t.TempDir())
	workspace := t.TempDir()
	if err := store.Save(model.AppSettings{
		DisplayName: "My Reports",
		Workspace:   workspace,
		UIFontSize:  30,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.UIFontSize != 17 {
		t.Fatalf("UIFontSize = %d, want 17", got.UIFontSize)
	}
}

func TestStoreRejectsEmptyName(t *testing.T) {
	t.Parallel()
	store := New(t.TempDir(), t.TempDir())
	if err := store.Save(model.AppSettings{Workspace: t.TempDir()}); err == nil {
		t.Fatal("expected empty display name to fail")
	}
}
