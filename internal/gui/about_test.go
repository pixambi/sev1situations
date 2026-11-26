package gui

import (
	"strings"
	"testing"

	"github.com/pixambi/sev1situations/internal/version"
)

func TestTheAboutTextSaysWhichVersionThisIs(t *testing.T) {
	old := version.Version
	t.Cleanup(func() { version.Version = old })

	version.Version = "1.4.2"
	if got := aboutText(); !strings.Contains(got, "Version 1.4.2") || strings.Contains(got, "development") {
		t.Errorf("a release build says %q", got)
	}
	version.Version = "dev"
	if got := aboutText(); !strings.Contains(got, "development build") {
		t.Errorf("a development build says %q", got)
	}
}
