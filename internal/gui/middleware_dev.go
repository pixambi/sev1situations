//go:build !production

package gui

import "github.com/wailsapp/wails/v3/pkg/application"

// assetMiddleware adds nothing in a development build. The Vite dev server needs a
// websocket for hot reload, which the production policy forbids.
func assetMiddleware() application.Middleware { return nil }
