// Package sev1situations is the module root. It only embeds the compiled
// frontend so that cmd/s1s can hand it to the Wails shell (R-LANG-3).
package sev1situations

import "embed"

// Frontend is the compiled Svelte app (frontend/dist), built by `task build`.
//
//go:embed all:frontend/dist
var Frontend embed.FS
