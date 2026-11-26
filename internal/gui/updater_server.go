//go:build server

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// setupUpdater does nothing in server mode: there is no native menu or install
// location to update, and the server build only exists for the e2e tests.
func setupUpdater(*application.App) {}
