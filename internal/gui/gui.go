package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Wails uses Go's `embed` package to embed the frontend files into the binary.
// Any files in the frontend/dist folder will be embedded into the binary and
// made available to the frontend.
// See https://pkg.go.dev/embed for more information.

//go:embed all:frontend/dist
var assets embed.FS

// main is the application's entry point. It creates the application and its window and runs it.
func main() {
	// Records a runtime trace for the whole run when S1S_TRACE is set (see trace.go).
	stopTrace := startTrace()

	app := application.New(application.Options{
		Name:        "Sev1 Situations",
		Description: "Role-played severity-1 incident calls for HashiCorp products",
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		// Only used when built with -tags server, which the Playwright e2e tests do.
		Server: application.ServerOptions{
			Host: "127.0.0.1",
			Port: 8080,
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	// Adds in-app updates when this is a release build (see updater.go).
	setupUpdater(app)

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Sev1 Situations",
		Width:            1000,
		Height:           618,
		BackgroundColour: application.NewRGB(6, 7, 15),
		URL:              "/",
	})

	// Run the application. This blocks until the application has been exited.
	err := app.Run()
	stopTrace()

	// If an error occurred while running the application, log it and exit.
	if err != nil {
		log.Fatal(err)
	}
}
