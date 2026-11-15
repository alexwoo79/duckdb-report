package workflowbundle

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alexwoo79/duckdb-report/internal/model"
	"github.com/alexwoo79/duckdb-report/internal/workflow"
	"github.com/alexwoo79/duckdb-report/internal/workspace"
)

const (
	manifestName       = "workflow.json"
	maxImportFileCount = 10000
	maxImportBytes     = 512 << 20
)

type assetRoot struct {
	prefix string
}

func Export(output io.Writer, store *workspace.Store, value model.Workflow) error {
	packaged, err := cloneWorkflow(value)
	if err != nil {
		return err
	}
	archive := zip.NewWriter(output)
	roots := make(map[string]assetRoot)
	files := make(map[string]string)
	nextRoot := 0
	nextFile := 0

	for presetIndex := range packaged.Presets {
		preset := &packaged.Presets[presetIndex]
		for key, value := range preset.Params {
			if value == "" || (key != "path" && key != "file" && key != "query_file" && key != "template") {
				continue
			}
			source, resolveErr := store.ResolveExisting(value)
			if resolveErr != nil {
				continue
			}
			if key == "path" {
				cacheKey := "file\x00" + source
				packagePath, exists := files[cacheKey]
				if !exists {
					nextFile++
					packagePath = fmt.Sprintf("assets/files/%03d-%s", nextFile, filepath.Base(source))
					if err := addFile(archive, source, packagePath); err != nil {
						archive.Close()
						return err
					}
					files[cacheKey] = packagePath
				}
				preset.Params[key] = packagePath
				continue
			}

			root := filepath.Dir(source)
			asset, exists := roots[root]
			if !exists {
				nextRoot++
				asset = assetRoot{prefix: fmt.Sprintf("assets/roots/%03d", nextRoot)}
				roots[root] = asset
				if err := addDirectory(archive, root, asset.prefix); err != nil {
					archive.Close()
					return err
				}
			}
			relative, err := filepath.Rel(root, source)
			if err != nil {
				archive.Close()
				return fmt.Errorf("resolve workflow asset path: %w", err)
			}
			preset.Params[key] = asset.prefix + "/" + filepath.ToSlash(relative)
		}
	}

	manifest, err := archive.Create(manifestName)
	if err != nil {
		archive.Close()
		return fmt.Errorf("create workflow manifest: %w", err)
	}
	encoded, err := json.MarshalIndent(packaged, "", "  ")
	if err != nil {
		archive.Close()
		return fmt.Errorf("encode workflow manifest: %w", err)
	}
	if _, err := manifest.Write(append(encoded, '\n')); err != nil {
		archive.Close()
		return fmt.Errorf("write workflow manifest: %w", err)
	}
	if err := archive.Close(); err != nil {
		return fmt.Errorf("close workflow package: %w", err)
	}
	return nil
}

func Import(path string, store *workspace.Store) (model.Workflow, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return model.Workflow{}, fmt.Errorf("open workflow package: %w", err)
	}
	defer reader.Close()
	if len(reader.File) > maxImportFileCount {
		return model.Workflow{}, errors.New("workflow package contains too many files")
	}

	var manifest []byte
	for _, file := range reader.File {
		if file.Name == manifestName {
			manifest, err = readZipFile(file, maxImportBytes)
			if err != nil {
				return model.Workflow{}, err
			}
			break
		}
	}
	if manifest == nil {
		return model.Workflow{}, errors.New("workflow package does not contain workflow.json")
	}
	var imported model.Workflow
	if err := json.Unmarshal(manifest, &imported); err != nil {
		return model.Workflow{}, fmt.Errorf("decode workflow manifest: %w", err)
	}

	importID := time.Now().Format("20060102-150405") + "-" + slug(imported.Name)
	importDir := filepath.Join(store.Workflows, "imports", importID)
	if err := os.MkdirAll(importDir, 0o755); err != nil {
		return model.Workflow{}, fmt.Errorf("create workflow import directory: %w", err)
	}
	var total int64
	for _, file := range reader.File {
		if file.Name == manifestName || file.FileInfo().IsDir() || strings.HasPrefix(file.Name, "__MACOSX/") {
			continue
		}
		clean := filepath.Clean(filepath.FromSlash(file.Name))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return model.Workflow{}, fmt.Errorf("unsafe workflow package path %q", file.Name)
		}
		total += int64(file.UncompressedSize64)
		if total > maxImportBytes {
			return model.Workflow{}, errors.New("workflow package expands beyond 512 MB")
		}
		target := filepath.Join(importDir, clean)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return model.Workflow{}, fmt.Errorf("create workflow asset directory: %w", err)
		}
		if err := extractZipFile(file, target); err != nil {
			return model.Workflow{}, err
		}
	}

	importPrefix := store.Relative(importDir)
	for presetIndex := range imported.Presets {
		for key, value := range imported.Presets[presetIndex].Params {
			if strings.HasPrefix(value, "assets/") {
				imported.Presets[presetIndex].Params[key] = importPrefix + "/" + value
			}
		}
	}
	if err := workflow.Validate(imported); err != nil {
		return model.Workflow{}, err
	}
	encoded, err := json.MarshalIndent(imported, "", "  ")
	if err != nil {
		return model.Workflow{}, fmt.Errorf("encode imported workflow: %w", err)
	}
	if err := os.WriteFile(filepath.Join(store.Workflows, importID+".json"), append(encoded, '\n'), 0o644); err != nil {
		return model.Workflow{}, fmt.Errorf("save imported workflow: %w", err)
	}
	if err := store.SaveWorkflow(imported); err != nil {
		return model.Workflow{}, err
	}
	return imported, nil
}

func cloneWorkflow(value model.Workflow) (model.Workflow, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return model.Workflow{}, fmt.Errorf("clone workflow: %w", err)
	}
	var result model.Workflow
	if err := json.Unmarshal(data, &result); err != nil {
		return model.Workflow{}, fmt.Errorf("clone workflow: %w", err)
	}
	return result, nil
}

func addDirectory(archive *zip.Writer, root, prefix string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk workflow asset directory: %w", walkErr)
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() || entry.Name() == ".DS_Store" {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("resolve workflow asset: %w", err)
		}
		return addFile(archive, path, prefix+"/"+filepath.ToSlash(relative))
	})
}

func addFile(archive *zip.Writer, source, name string) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open workflow asset %q: %w", source, err)
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return fmt.Errorf("inspect workflow asset: %w", err)
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return fmt.Errorf("create workflow asset entry: %w", err)
	}
	header.Name = filepath.ToSlash(name)
	header.Method = zip.Deflate
	output, err := archive.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("create workflow asset: %w", err)
	}
	if _, err := io.Copy(output, input); err != nil {
		return fmt.Errorf("write workflow asset: %w", err)
	}
	return nil
}

func readZipFile(file *zip.File, limit int64) ([]byte, error) {
	if int64(file.UncompressedSize64) > limit {
		return nil, fmt.Errorf("workflow manifest is too large")
	}
	entry, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open workflow manifest: %w", err)
	}
	defer entry.Close()
	data, err := io.ReadAll(io.LimitReader(entry, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read workflow manifest: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, errors.New("workflow manifest is too large")
	}
	return data, nil
}

func extractZipFile(file *zip.File, destination string) error {
	entry, err := file.Open()
	if err != nil {
		return fmt.Errorf("open workflow asset: %w", err)
	}
	defer entry.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create workflow asset: %w", err)
	}
	if _, err := io.Copy(output, io.LimitReader(entry, maxImportBytes+1)); err != nil {
		output.Close()
		return fmt.Errorf("extract workflow asset: %w", err)
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("close workflow asset: %w", err)
	}
	return nil
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			builder.WriteRune(character)
		} else if builder.Len() > 0 && !strings.HasSuffix(builder.String(), "-") {
			builder.WriteByte('-')
		}
	}
	result := strings.Trim(builder.String(), "-")
	if result == "" {
		return "workflow"
	}
	if len(result) > 40 {
		result = result[:40]
	}
	return result
}
