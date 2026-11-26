//go:build !server

package main

import (
	"context"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"

	"github.com/pixambi/sev1situations/internal/version"
)

// updateRepository is the GitHub repository whose releases are the update source.
const updateRepository = "pixambi/sev1situations"

// setupUpdater wires in-app updates from GitHub releases and adds
// "Check for Updates..." to the Help menu.
//
// Builds without a semantic version (task dev, go run) skip it entirely, so a
// developer build never offers to replace itself with a release.
// Updates are verified against the SHA256SUMS asset of the release (decision D7).
func setupUpdater(app *application.App) {
	if !version.IsRelease() {
		app.Logger.Info("updater disabled: not a release build", "version", version.Version)
		return
	}

	gh, err := github.New(github.Config{
		Repository:    updateRepository,
		ChecksumAsset: "SHA256SUMS",
		// The repository is access-controlled, so unauthenticated requests get a 404.
		Token: os.Getenv("S1S_GITHUB_TOKEN"),
	})
	if err != nil {
		app.Logger.Error("updater disabled", "error", err)
		return
	}

	if err := app.Updater.Init(updater.Config{
		CurrentVersion: version.Version,
		Providers:      []updater.Provider{gh},
	}); err != nil {
		app.Logger.Error("updater disabled", "error", err)
		return
	}

	menu := application.DefaultApplicationMenu()
	if help := menu.FindByRole(application.HelpMenu); help != nil && help.GetSubmenu() != nil {
		help.GetSubmenu().Add("Check for Updates…").OnClick(func(*application.Context) {
			// CheckAndInstall blocks until the update is staged, so keep it off the UI thread.
			go func() {
				if err := app.Updater.CheckAndInstall(context.Background()); err != nil {
					app.Logger.Error("update", "error", err)
				}
			}()
		})
	}
	app.Menu.SetApplicationMenu(menu)
}
