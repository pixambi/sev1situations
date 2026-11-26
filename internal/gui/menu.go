//go:build !server

package gui

import "github.com/wailsapp/wails/v3/pkg/application"

// EventMenuSettings is emitted when the instructor chooses Settings in the menu,
// and the page shows the Settings page.
const EventMenuSettings = "menu:settings"

// setupMenu installs the application menu (6.7): About, Settings and Quit in the
// application menu, the Edit menu, which copy and paste in the page depend on,
// full screen, and the Window menu.
//
// It is built by hand and not from Wails's default, because the default menu has a
// Help item that navigates the window to a website and Reload and developer-tool
// items that an instructor in a session has no use for.
func setupMenu(app *application.App, name string) {
	menu := app.Menu.New()

	main := menu.AddSubmenu(name)
	main.AddRole(application.About)
	main.AddSeparator()
	main.Add("Settings…").SetAccelerator("CmdOrCtrl+,").OnClick(func(*application.Context) {
		app.Event.Emit(EventMenuSettings)
	})
	main.AddSeparator()
	main.AddRole(application.Hide)
	main.AddRole(application.HideOthers)
	main.AddRole(application.UnHide)
	main.AddSeparator()
	main.AddRole(application.Quit)

	menu.AddRole(application.EditMenu)

	view := menu.AddSubmenu("View")
	view.AddRole(application.ToggleFullscreen)

	menu.AddRole(application.WindowMenu)

	app.Menu.SetApplicationMenu(menu)
}
