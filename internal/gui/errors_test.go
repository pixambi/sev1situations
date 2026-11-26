package gui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/pixambi/sev1situations/internal/engine"
)

func TestPlainGivesTheInstructorASentenceNotAnInternalError(t *testing.T) {
	for _, tt := range []struct {
		err  error
		want string
	}{
		{engine.ErrBusy, "still running"},
		{engine.ErrNoRun, "No scenario is running"},
		{engine.ErrWrongState, "cannot be done at this point"},
		{fmt.Errorf("continue: %w: the check has not passed", engine.ErrWrongState), "cannot be done at this point"},
	} {
		if got := plain(tt.err); got == nil || !strings.Contains(got.Error(), tt.want) {
			t.Errorf("plain(%v) = %v, want text with %q", tt.err, got, tt.want)
		}
	}

	other := errors.New("deploy: the image could not be pulled")
	if !errors.Is(plain(other), other) {
		t.Error("an error that is already written for people was changed")
	}
	if plain(nil) != nil {
		t.Error("plain(nil) is not nil")
	}
}
