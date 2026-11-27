//go:build !server

package main

import "github.com/pixambi/sev1situations/internal/app"

// useTestDoubles does nothing in a build for people. The fake runtime exists only in the server build that the end-to-end tests
// use, so no setting can switch a release onto it.
func useTestDoubles(*app.Options) error { return nil }
