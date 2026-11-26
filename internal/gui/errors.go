package gui

import (
	"errors"
	"fmt"

	"github.com/pixambi/sev1situations/internal/engine"
)

// notice is an error whose text is a sentence written for the instructor, with a
// capital letter and a full stop. It exists so the linter's rule for error
// strings, which is right for errors that programs wrap, does not apply to them.
type notice string

func (n notice) Error() string { return string(n) }

// errNoRun is the engine's error for there being no run, for services that check it themselves.
var errNoRun = engine.ErrNoRun

// plain turns an error from the core into the sentence the instructor sees
// (R-QUAL-1). Wails passes the text of the error to the frontend, which shows it
// in an inline notification. Errors with no better wording keep their own text,
// which the core already writes for people.
func plain(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, engine.ErrBusy):
		return notice("Another action is still running. Wait for it to finish, then try again.")
	case errors.Is(err, engine.ErrNoRun):
		return notice("No scenario is running.")
	case errors.Is(err, engine.ErrWrongState):
		return notice("That cannot be done at this point in the scenario. The page may be out of date, so check the current step.")
	}
	return err
}

// nodeError explains why a node has no container to read or attach to. what is
// the thing the instructor asked for, such as "log" or "terminal".
func nodeError(node, what string, err error) error {
	if errors.Is(err, engine.ErrWrongState) {
		return notice(fmt.Sprintf("%s has been removed, so it has no %s. Revive it first.", node, what))
	}
	return plain(err)
}
