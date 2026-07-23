package main

import (
	"embed"

	"rohy/backend/consts"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app, err := NewApp()
	if err != nil {
		println("Error: failed to start rohy:", err.Error())
		return
	}

	// Bind the thin API structs (not App): the frontend calls Events and Graph.
	err = wails.Run(&options.App{
		Title:     consts.AppDisplayName,
		Width:     consts.WindowDefaultWidth,
		Height:    consts.WindowDefaultHeight,
		MinWidth:  consts.WindowMinWidth,
		MinHeight: consts.WindowMinHeight,
		// Frameless: the app draws its own title bar with minimise/maximise/close
		// controls (see TitleBar.svelte). The window stays resizable from its edges.
		Frameless: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app.Events,
			app.Graph,
			app.Rules,
			app.Build,
			app.Findings,
			app.System,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
