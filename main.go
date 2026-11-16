package main

import (
	"embed"
	"log/slog"
	"os"

	"rohy/backend/api"
	"rohy/backend/consts"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app, err := NewApp()
	if err != nil {
		println("Error: failed to start rohy:", err.Error())
		os.Exit(1)
	}
	desk := &desktop{}
	app.desktop = desk

	wapp := application.New(application.Options{
		Name:     consts.AppDisplayName,
		Services: services(app),
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		// Withholds every call's arguments and result from the log: a passphrase crosses a call as
		// an argument (item 41).
		Logger: slog.New(api.NewSafeLogHandler(slog.NewTextHandler(os.Stderr, nil))),
		// Sends a failed call's code with its message; the frontend's call() reads it.
		MarshalError: api.MarshalError,
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})
	desk.app = wapp
	desk.window = wapp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     consts.AppDisplayName,
		Width:     consts.WindowDefaultWidth,
		Height:    consts.WindowDefaultHeight,
		MinWidth:  consts.WindowMinWidth,
		MinHeight: consts.WindowMinHeight,
		// Frameless: the app draws its own title bar with minimise/maximise/close
		// controls (see TitleBar.svelte). The window stays resizable from its edges.
		Frameless:        true,
		BackgroundColour: application.NewRGB(27, 38, 54),
		URL:              "/",
	})

	if err := wapp.Run(); err != nil {
		println("Error:", err.Error())
		os.Exit(1)
	}
}
