//go:build production

package gui

import "github.com/wailsapp/wails/v3/pkg/application"

// assetMiddleware applies the security headers in a production build.
func assetMiddleware() application.Middleware { return withSecurityHeaders }
