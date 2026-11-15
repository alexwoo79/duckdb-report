package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist example
var assets embed.FS

func main() {
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
