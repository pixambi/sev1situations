// Package gui is the Wails shell: the application, its window, and the
// services and events that connect the frontend to the core. It is the only
// package that imports Wails (R-LANG-2).
package gui

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Deps are what the services work over. The program supplies them, so this
// package holds no knowledge of Podman or products.
type Deps struct {
	// Connect joins the core to the container runtime. It is called when the
	// window asks, and again on Retry while the runtime is not ready.
	Connect Connector
	// Catalogue loads the scenarios.
	Catalogue Catalogue
	// Licensed reports whether a license is saved for a product.
	Licensed LicenseCheck
	// Versions lists a product's versions for the Start form.
	Versions VersionLister
	// Instructor returns the saved instructor name.
	Instructor func() string
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
	var conn *link
	if deps != nil {
		conn = newLink(ctx, deps.Connect, emit)
		services = []application.Service{
			application.NewService(NewScenarioService(ScenarioDeps{Catalogue: deps.Catalogue, Licensed: deps.Licensed, Versions: deps.Versions, Instructor: deps.Instructor, Context: ctx})),
			application.NewService(NewRunService(ctx, conn.source, deps.Catalogue, emit)),
			application.NewService(NewSystemService(conn, emit)),
			application.NewService(NewClusterService(ctx, conn)),
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
		// Quitting during a run asks first, then ends the run so nothing is left
		// behind (R-APP-6, R-STATE-3). The window closing leads here as well,
		// because the application ends with its last window.
		ShouldQuit: func() bool { return conn == nil || !confirmQuit(app, conn) },
		OnShutdown: func() { conn.endRun() },
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

// confirmQuit reports whether the application must stay open for now. With no run
// it returns false and the application quits. During a run it asks the instructor,
// and quits itself once the run has ended, if they agree.
func confirmQuit(app *application.App, conn *link) bool {
	if !conn.runActive() {
		return false
	}
	d := app.Dialog.Question().SetTitle("Quit Sev1 Situations?").SetMessage(
		"A scenario is running. Quitting ends it and removes its containers, and the report is saved.")
	quit := d.AddButton("End scenario and quit")
	quit.OnClick(func() {
		go func() {
			conn.endRun()
			app.Quit()
		}()
	})
	keep := d.AddButton("Keep running")
	d.SetDefaultButton(keep)
	d.SetCancelButton(keep)
	d.Show()
	return true
}
