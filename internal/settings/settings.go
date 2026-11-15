package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alexwoo79/duckdb-report/internal/model"
)

const fileName = "settings.json"

type Store struct {
	Path             string
	DefaultWorkspace string
}

func New(configRoot, defaultWorkspace string) *Store {
	return &Store{
		Path:             filepath.Join(configRoot, fileName),
		DefaultWorkspace: defaultWorkspace,
	}
}

func (s *Store) Load() (model.AppSettings, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return model.AppSettings{
			DisplayName: "DuckDB Report Studio",
			Workspace:   s.DefaultWorkspace,
		}, nil
	}
	if err != nil {
		return model.AppSettings{}, fmt.Errorf("read settings: %w", err)
	}
	var value model.AppSettings
	if err := json.Unmarshal(data, &value); err != nil {
		return model.AppSettings{}, fmt.Errorf("decode settings: %w", err)
	}
	if strings.TrimSpace(value.DisplayName) == "" {
		value.DisplayName = "DuckDB Report Studio"
	}
	if strings.TrimSpace(value.Workspace) == "" {
		value.Workspace = s.DefaultWorkspace
	}
	return value, nil
}

func (s *Store) Save(value model.AppSettings) error {
	value.DisplayName = strings.TrimSpace(value.DisplayName)
	value.Workspace = strings.TrimSpace(value.Workspace)
	if value.DisplayName == "" {
		return errors.New("application display name is empty")
	}
	if len([]rune(value.DisplayName)) > 80 {
		return errors.New("application display name is longer than 80 characters")
	}
	if value.Workspace == "" {
		return errors.New("workspace path is empty")
	}
	absolute, err := filepath.Abs(value.Workspace)
	if err != nil {
		return fmt.Errorf("resolve workspace path: %w", err)
	}
	value.Workspace = absolute
	if err := os.MkdirAll(absolute, 0o755); err != nil {
		return fmt.Errorf("create workspace directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return fmt.Errorf("create settings directory: %w", err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	temporary := s.Path + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write temporary settings: %w", err)
	}
	if err := os.Rename(temporary, s.Path); err != nil {
		return fmt.Errorf("replace settings: %w", err)
	}
	return nil
}
