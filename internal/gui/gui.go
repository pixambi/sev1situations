// Package gui is the Wails shell: the application, its window, and the
// services and events that connect the frontend to the core. It is the only
// package that imports Wails (R-LANG-2).
package gui

import (
	"context"
	"fmt"
	"io/fs"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/pixambi/sev1situations/internal/settings"
	"github.com/pixambi/sev1situations/internal/version"
)

// aboutText is what the About box says: what the application is, and which version of it this is.
func aboutText() string {
	v := version.Version
	if !version.IsRelease() {
		v += " (a development build)"
	}
	return "Role-played severity-1 incident calls for HashiCorp products.\n\nVersion " + v
}

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
	// Settings is the saved settings store.
	Settings *settings.Store
}

// Run opens the main window on the compiled frontend and blocks until the
// application exits. signals ends when the process is asked to stop, by SIGINT or
// SIGTERM: the run is then ended and the application quits without asking, because
// nobody is there to answer (R-APP-6). With deps it also registers the services and forwards the
// engine's events to the frontend. Without them it shows the window alone.
func Run(signals context.Context, assets fs.FS, deps *Deps) error {
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
	var stopping atomic.Bool
	emit := func(name string, data any) {
		if app != nil {
			app.Event.Emit(name, data)
		}
	}

	var services []application.Service
	var conn *link
	if deps != nil {
		conn = newLink(ctx, deps.Connect, emit)
		logs := NewLogService(ctx, conn, emit)
		conn.onRunEnded(logs.CloseAll)
		services = []application.Service{
			application.NewService(NewScenarioService(ScenarioDeps{Catalogue: deps.Catalogue, Licensed: deps.Licensed, Versions: deps.Versions, Instructor: deps.Instructor, Context: ctx})),
			application.NewService(NewRunService(ctx, conn.source, deps.Catalogue, emit)),
			application.NewService(NewSystemService(conn, emit)),
			application.NewService(NewSettingsService(deps.Settings)),
			application.NewService(NewClusterService(ctx, conn)),
			application.NewService(logs),
			application.NewService(NewTerminalService(ctx, conn, emit)),
		}
	}

	app = application.New(application.Options{
		Services:    services,
		Name:        "Sev1 Situations",
		Description: aboutText(),
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
		// because the application ends with its last window. A signal does not
		// ask: it sets stopping, and the quit goes straight through.
		ShouldQuit: func() bool { return stopping.Load() || conn == nil || !confirmQuit(app, conn) },
		// Signals are handled below, so that the run is ended before the quit.
		DisableDefaultSignalHandler: true,
		OnShutdown:                  func() { conn.endRun() },
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

	go func() {
		select {
		case <-ctx.Done():
			return // the application is already ending
		case <-signals.Done():
		}
		stopping.Store(true)
		conn.endRun()
		app.Quit()
	}()

	setupMenu(app, "Sev1 Situations")

	// Adds in-app updates when this is a release build (see updater.go).
	setupUpdater(app)

	window = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Sev1 Situations",
		Width:            1000,
		Height:           700,
		MinWidth:         800,
		MinHeight:        560,
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
		"A scenario is running. Quitting ends it, removes its containers and saves its report.")
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
