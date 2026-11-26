//go:build !server

package gui

import "github.com/wailsapp/wails/v3/pkg/application"

// nativeSave opens the macOS save panel from Go (6.7). The frontend never sees a path
// until the instructor has chosen it.
func nativeSave(app func() *application.App) SaveDialog {
	return func(suggestedName string) (string, error) {
		a := app()
		if a == nil {
			return "", nil
		}
		return a.Dialog.SaveFile().
			SetFilename(suggestedName).
			SetMessage("Choose where to save the report").
			CanCreateDirectories(true).
			PromptForSingleSelection()
	}
}
