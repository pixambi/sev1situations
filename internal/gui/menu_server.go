//go:build server

package gui

import "github.com/wailsapp/wails/v3/pkg/application"

// EventMenuSettings is emitted when the instructor chooses Settings in the menu.
const EventMenuSettings = "menu:settings"

// setupMenu does nothing in server mode: there is no native menu.
func setupMenu(*application.App, string) {}
