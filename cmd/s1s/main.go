// Command s1s is the Sev1 Situations executable. With no arguments it opens the
// application window. With arguments it runs a headless subcommand (R-APP-5).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	sev1situations "github.com/pixambi/sev1situations"
	"github.com/pixambi/sev1situations/internal/app"
	"github.com/pixambi/sev1situations/internal/gui"
	"github.com/pixambi/sev1situations/internal/scenario"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	products, err := app.DefaultProducts()
	if err != nil {
		fmt.Fprintln(os.Stderr, "s1s:", err)
		os.Exit(1)
	}
	opts := app.Options{Builtin: sev1situations.Scenarios, Products: products, Stdin: os.Stdin}

	// With arguments it is the command-line tool (R-APP-5).
	if len(os.Args) > 1 {
		code := app.Run(ctx, os.Args[1:], os.Stdout, os.Stderr, opts)
		stop()
		os.Exit(code)
	}

	// With none it opens the window. The window opens even when Podman is not
	// ready, and connects when the page asks (R-APP-2).
	store, err := app.OpenSettings(opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "s1s:", err)
		os.Exit(1)
	}
	opts.Settings = store
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	deps := &gui.Deps{
		Licensed:   app.Licensed(store),
		Versions:   app.Versions(products),
		Instructor: store.Instructor,
		Settings:   store,
		Catalogue:  func() (*scenario.Registry, error) { return app.LoadCatalogue(opts) },
		Connect: func(ctx context.Context) (*gui.Connection, error) {
			conn, err := app.Connect(ctx, opts, log, nil)
			if err != nil {
				return nil, err
			}
			return &gui.Connection{Engine: conn.Engine, View: conn.Deployer.View, Runtime: conn.Runtime, LogStreams: conn.Deployer.LogStreams, Notices: conn.Startup.Notices()}, nil
		},
	}
	if err := gui.Run(sev1situations.Frontend, deps); err != nil {
		fmt.Fprintln(os.Stderr, "s1s:", err)
		os.Exit(1)
	}
}
