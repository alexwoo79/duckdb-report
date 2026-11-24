package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alexwoo79/duckdb-report/internal/model"
	"github.com/alexwoo79/duckdb-report/internal/update"
	appversion "github.com/alexwoo79/duckdb-report/internal/version"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	updateRepository      = "alexwoo79/duckdb-report"
	updateCheckTimeout    = 20 * time.Second
	updateDownloadTimeout = 15 * time.Minute
)

type updateState struct {
	mu             sync.Mutex
	downloadMu     sync.Mutex
	latest         *update.Release
	downloadedPath string
}

func newUpdateChecker() *update.Checker {
	return update.NewChecker(update.Config{
		Repository:     updateRepository,
		CurrentVersion: appversion.Current,
		UserAgent:      "duckdb-report/" + appversion.Current,
	})
}

func (a *App) CheckForUpdates() (model.UpdateInfo, error) {
	if a.updater == nil {
		return model.UpdateInfo{}, errors.New("update checker is not available")
	}
	ctx, cancel := context.WithTimeout(a.operationContext(), updateCheckTimeout)
	defer cancel()
	release, err := a.updater.Check(ctx)
	if err != nil {
		return model.UpdateInfo{}, err
	}
	a.updates.mu.Lock()
	a.updates.latest = release
	a.updates.mu.Unlock()
	return release.Info(), nil
}

func (a *App) DownloadUpdate() (model.UpdateDownload, error) {
	a.updates.downloadMu.Lock()
	defer a.updates.downloadMu.Unlock()

	a.updates.mu.Lock()
	release := a.updates.latest
	a.updates.mu.Unlock()
	if release == nil {
		return model.UpdateDownload{}, errors.New("check for updates before downloading")
	}
	downloadDir, err := updateDownloadDirectory()
	if err != nil {
		return model.UpdateDownload{}, err
	}
	ctx, cancel := context.WithTimeout(a.operationContext(), updateDownloadTimeout)
	defer cancel()
	download, err := a.updater.Download(ctx, release, downloadDir)
	if err != nil {
		return model.UpdateDownload{}, err
	}
	a.updates.mu.Lock()
	a.updates.downloadedPath = download.Path
	a.updates.mu.Unlock()
	return download, nil
}

func (a *App) OpenUpdateRelease() error {
	a.updates.mu.Lock()
	release := a.updates.latest
	a.updates.mu.Unlock()
	info := release.Info()
	if strings.TrimSpace(info.ReleaseURL) == "" {
		return errors.New("release page is not available")
	}
	if a.ctx == nil {
		return errors.New("desktop runtime is not available")
	}
	runtime.BrowserOpenURL(a.ctx, info.ReleaseURL)
	return nil
}

func (a *App) OpenDownloadedUpdate() error {
	a.updates.mu.Lock()
	path := a.updates.downloadedPath
	a.updates.mu.Unlock()
	if path == "" {
		return errors.New("no update package has been downloaded")
	}
	return openWithSystem(path)
}

func updateDownloadDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, "Downloads", "duckdb-report-updates"), nil
	}
	cacheDir, cacheErr := os.UserCacheDir()
	if cacheErr != nil {
		return "", fmt.Errorf("locate update download directory: %w", cacheErr)
	}
	return filepath.Join(cacheDir, "duckdb_report", "updates"), nil
}
