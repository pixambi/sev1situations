package gui

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/pixambi/sev1situations/internal/engine"
)

// Limits on what the instructor can type, so a paste cannot grow the run's state
// without bound.
const (
	maxNameLength  = 100
	maxNotesLength = 20000
)

// RunService drives the one run (3.4). Its methods validate their input and call
// the engine, which does the work and decides what is allowed (R-UI-5). Each one
// returns when its operation is finished, so the frontend shows a loading state
// until then, and progress arrives as events in the meantime.
type RunService struct {
	engines   EngineSource
	catalogue Catalogue
	emit      Emitter
	// ctx ends when the application does. The engine gives a run its own context,
	// which End cancels, so a call's context only bounds the call (R-APP-6).
	ctx context.Context
}

// NewRunService returns a service over the engine. ctx ends when the application does.
func NewRunService(ctx context.Context, engines EngineSource, c Catalogue, emit Emitter) *RunService {
	return &RunService{engines: engines, catalogue: c, emit: emit, ctx: ctx}
}

// idle is the state shown when there is no engine to ask.
func idle() engine.Snapshot { return engine.Snapshot{Status: engine.StatusIdle, ActiveStep: -1} }

// StartForm is what the Start step collects (R-SCN-11).
type StartForm struct {
	ScenarioID string
	Instructor string
	Trainee    string
	// Params are the scenario's parameters by ID. Missing ones take their default.
	Params map[string]any
}

// Start deploys a scenario and returns when it is healthy or has failed. After a
// failure the run stays so it can be retried or ended.
func (s *RunService) Start(form StartForm) (engine.Snapshot, error) {
	eng, err := s.engines()
	if err != nil {
		return idle(), err
	}
	form.Instructor, form.Trainee = strings.TrimSpace(form.Instructor), strings.TrimSpace(form.Trainee)
	switch {
	case strings.TrimSpace(form.ScenarioID) == "":
		return eng.Snapshot(), notice("Choose a scenario to start.")
	case form.Instructor == "":
		return eng.Snapshot(), notice("Enter the instructor's name.")
	case form.Trainee == "":
		return eng.Snapshot(), notice("Enter the trainee's name.")
	case utf8.RuneCountInString(form.Instructor) > maxNameLength, utf8.RuneCountInString(form.Trainee) > maxNameLength:
		return eng.Snapshot(), notice(fmt.Sprintf("Names can be at most %d characters.", maxNameLength))
	}

	reg, err := s.catalogue()
	if reg == nil {
		return eng.Snapshot(), fmt.Errorf("load the scenarios: %w", err)
	}
	entry, ok := reg.Get(form.ScenarioID)
	if !ok {
		return eng.Snapshot(), notice(fmt.Sprintf("There is no scenario %q.", form.ScenarioID))
	}

	err = eng.Start(s.ctx, entry.Scenario, engine.StartInput{Instructor: form.Instructor, Trainee: form.Trainee, Params: form.Params})
	return s.finish(eng, err)
}

// Current returns the run's state, or an idle snapshot when there is none. The
// frontend calls it once on load and then follows run:state.
func (s *RunService) Current() engine.Snapshot {
	if eng, err := s.engines(); err == nil {
		return eng.Snapshot()
	}
	return idle()
}

// Retry tears a failed deployment down and deploys it again.
func (s *RunService) Retry() (engine.Snapshot, error) {
	return s.step("", false, func(eng *engine.Engine) error { return eng.Retry(s.ctx) })
}

// RunAction applies the active breakage step's change, then checks the result.
func (s *RunService) RunAction(stepID string) (engine.Snapshot, error) {
	return s.step(stepID, true, func(eng *engine.Engine) error { return eng.Apply(s.ctx) })
}

// Verify runs the active step's check. It can be run as often as needed.
func (s *RunService) Verify(stepID string) (engine.Snapshot, error) {
	return s.step(stepID, true, func(eng *engine.Engine) error { return eng.Verify(s.ctx) })
}

// Continue completes the active step, which needs a passing verification, and
// opens the next one.
func (s *RunService) Continue(stepID string) (engine.Snapshot, error) {
	return s.step(stepID, true, func(eng *engine.Engine) error { return eng.Continue(s.ctx) })
}

// SaveNotes saves the instructor's notes for a step.
func (s *RunService) SaveNotes(stepID, text string) (engine.Snapshot, error) {
	if utf8.RuneCountInString(text) > maxNotesLength {
		return s.Current(), notice(fmt.Sprintf("Notes can be at most %d characters.", maxNotesLength))
	}
	return s.step("", false, func(eng *engine.Engine) error { return eng.SetNotes(stepID, text) })
}

// SetRating saves the instructor's rating of a step, from 1 to 5, or 0 to clear it.
func (s *RunService) SetRating(stepID string, rating int) (engine.Snapshot, error) {
	if rating < 0 || rating > 5 {
		return s.Current(), notice("Choose a rating from 1 to 5.")
	}
	return s.step("", false, func(eng *engine.Engine) error { return eng.SetRating(stepID, rating) })
}

// Rollback returns the run to the start of a step that the scenario allows it to.
func (s *RunService) Rollback(stepID string) (engine.Snapshot, error) {
	if strings.TrimSpace(stepID) == "" {
		return s.Current(), notice("Choose the step to roll back to.")
	}
	return s.step("", false, func(eng *engine.Engine) error { return eng.Rollback(s.ctx, stepID) })
}

// End stops the run, removes everything it created and returns the final state.
// It also stops an operation that is still running.
func (s *RunService) End() (engine.Snapshot, error) {
	eng, err := s.engines()
	if err != nil {
		return idle(), err
	}
	snap, err := eng.End(s.ctx)
	s.emit(EventRunState, eng.Snapshot())
	return snap, plain(err)
}

// NodeAction stops, starts, removes or revives one node.
func (s *RunService) NodeAction(node, action string) (engine.Snapshot, error) {
	if strings.TrimSpace(node) == "" {
		return s.Current(), notice("Choose a node.")
	}
	a := engine.NodeAction(action)
	switch a {
	case engine.ActionStop, engine.ActionStart, engine.ActionRemove, engine.ActionRevive:
	default:
		return s.Current(), notice(fmt.Sprintf("%q is not something a node can do.", action))
	}
	return s.step("", false, func(eng *engine.Engine) error { return eng.NodeAction(s.ctx, node, a) })
}

// step runs one operation on the engine. With active set it first refuses a step
// that is no longer the active one, as when the page shows an older state than the
// run. It publishes the state afterwards, which the event stream may have missed,
// and returns the error in plain language.
func (s *RunService) step(stepID string, active bool, op func(*engine.Engine) error) (engine.Snapshot, error) {
	eng, err := s.engines()
	if err != nil {
		return idle(), err
	}
	if active {
		snap := eng.Snapshot()
		if snap.ActiveStep < 0 || snap.ActiveStep >= len(snap.Steps) || snap.Steps[snap.ActiveStep].ID != stepID {
			return snap, notice("That step is not the one in progress. The page may be out of date, so check the current step.")
		}
	}
	return s.finish(eng, op(eng))
}

// finish publishes the state after an operation and returns it with the error.
func (s *RunService) finish(eng *engine.Engine, err error) (engine.Snapshot, error) {
	snap := eng.Snapshot()
	s.emit(EventRunState, snap)
	return snap, plain(err)
}
