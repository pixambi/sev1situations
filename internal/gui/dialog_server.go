//go:build server

package gui

import (
	"errors"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// nativeSave cannot open a panel in server mode, so exporting says so.
func nativeSave(func() *application.App) SaveDialog {
	return func(string) (string, error) {
		return "", errors.New("saving a file needs the application window, which this build does not have")
	}
}
