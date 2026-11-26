// Package gui is the Wails shell: the application, its window, and the
// services and events that connect the frontend to the core. It is the only
// package that imports Wails (R-LANG-2).
package gui

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/pixambi/sev1situations/internal/engine"
)

// Deps are the core pieces the services work over. The program builds them at
// start-up (T-062), so this package holds no knowledge of Podman or products.
type Deps struct {
	// Engine runs the one run.
	Engine *engine.Engine
	// Catalogue loads the scenarios.
	Catalogue Catalogue
	// Prereq checks the container runtime.
	Prereq PrereqCheck
}

// Run opens the main window on the compiled frontend and blocks until the
// application exits. With deps it also registers the services and forwards the
// engine's events to the frontend. Without them it shows the window alone.
func Run(assets fs.FS, deps *Deps) error {
	// Records a runtime trace for the whole run when S1S_TRACE is set (see trace.go).
	stopTrace, err := startTrace()
	if err != nil {
		return err
	}
	defer stopTrace()

	// ctx ends when the application does, which stops the services' work and the event pump.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var window *application.WebviewWindow
	var app *application.App
	emit := func(name string, data any) {
		if app != nil {
			app.Event.Emit(name, data)
		}
	}

	var services []application.Service
	if deps != nil {
		services = []application.Service{
			application.NewService(NewScenarioService(deps.Catalogue)),
			application.NewService(NewRunService(ctx, deps.Engine, deps.Catalogue, emit)),
			application.NewService(NewSystemService(ctx, deps.Prereq, emit)),
		}
	}

	app = application.New(application.Options{
		Services:    services,
		Name:        "Sev1 Situations",
		Description: "Role-played severity-1 incident calls for HashiCorp products",
		Assets: application.AssetOptions{
			Handler:    application.AssetFileServerFS(assets),
			Middleware: assetMiddleware(),
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

	if deps != nil {
		go pumpEvents(ctx, deps.Engine, emit)
	}

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
