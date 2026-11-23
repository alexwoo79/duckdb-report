package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"strings"

	"github.com/alexwoo79/duckdb-report/internal/cli"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist example
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

func main() {
	if len(os.Args) > 1 {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		exitCode := cli.Execute(ctx, os.Args[1:], assets, os.Stdout, os.Stderr)
		stop()
		if exitCode != 0 {
			os.Exit(exitCode)
		}
		return
	}
	runDesktop()
}

func runDesktop() {
	frontend, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		fmt.Fprintln(os.Stderr, "load frontend assets:", err)
		os.Exit(1)
	}
	app := NewApp(assets)
	appName := app.displayName()
	assetOptions := &assetserver.Options{Assets: frontend}
	if handler := app.previewHandler(); handler != nil {
		assetOptions.Handler = handler
	}
	err = wails.Run(&options.App{
		Title:            appName,
		Width:            1480,
		Height:           940,
		MinWidth:         1120,
		MinHeight:        720,
		BackgroundColour: options.NewRGB(17, 20, 24),
		AssetServer:      assetOptions,
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []any{app},
		Linux: &linux.Options{
			Icon:             appIcon,
			ProgramName:      "duckdb_report",
			WebviewGpuPolicy: linuxWebviewGpuPolicy(),
		},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
			About: &mac.AboutInfo{
				Title:   appName,
				Message: "Local DuckDB + SQL + Quarto report workbench",
			},
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "run application:", err)
		os.Exit(1)
	}
}

func linuxWebviewGpuPolicy() linux.WebviewGpuPolicy {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DUCKDB_REPORT_WEBVIEW_GPU"))) {
	case "never", "off", "false":
		return linux.WebviewGpuPolicyNever
	case "always":
		return linux.WebviewGpuPolicyAlways
	default:
		return linux.WebviewGpuPolicyOnDemand
	}
}
