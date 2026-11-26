// Package sev1situations is the module root. It only embeds the compiled
// frontend and the built-in scenarios, so that cmd/s1s can hand them to the
// shell and the scenario registry (R-LANG-3).
package sev1situations

import "embed"

// Frontend is the compiled Svelte app (frontend/dist), built by `task build`.
//
//go:embed all:frontend/dist
var Frontend embed.FS

// Scenarios is the built-in scenario tree. Every folder under scenarios/ that
// holds a scenario.yaml is a scenario, found at start-up with no Go change
// (R-SCN-16).
//
//go:embed all:scenarios
var Scenarios embed.FS
