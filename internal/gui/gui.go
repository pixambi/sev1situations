// Package gui is the Wails shell: the application, its window, and the
// services and events that connect the frontend to the core. It is the only
// package that imports Wails (R-LANG-2).
package gui

import (
	"fmt"
	"io/fs"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Run opens the main window on the compiled frontend and blocks until the
// application exits.
func Run(assets fs.FS) error {
	// Records a runtime trace for the whole run when S1S_TRACE is set (see trace.go).
	stopTrace, err := startTrace()
	if err != nil {
		return err
	}
	defer stopTrace()

	var window *application.WebviewWindow

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
		// Only one instance may run (R-APP-8): start-up cleanup removes every
		// labelled run, so two instances would destroy each other's work. A
		// second launch brings the first window forward and exits.
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.pixelizedambitions.sev1situations",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if window != nil {
					window.UnMinimise()
					window.Show()
					window.Focus()
				}
			},
		},
	})

	// Adds in-app updates when this is a release build (see updater.go).
	setupUpdater(app)

	window = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Sev1 Situations",
		Width:            1000,
		Height:           618,
		BackgroundColour: application.NewRGB(6, 7, 15),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		return fmt.Errorf("run application: %w", err)
	}
	return nil
}
