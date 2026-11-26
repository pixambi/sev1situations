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
	engine    *engine.Engine
	catalogue Catalogue
	emit      Emitter
	// ctx ends when the application does. The engine gives a run its own context,
	// which End cancels, so a call's context only bounds the call (R-APP-6).
	ctx context.Context
}

// NewRunService returns a service over the engine. ctx ends when the application does.
func NewRunService(ctx context.Context, eng *engine.Engine, c Catalogue, emit Emitter) *RunService {
	return &RunService{engine: eng, catalogue: c, emit: emit, ctx: ctx}
}

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
	form.Instructor, form.Trainee = strings.TrimSpace(form.Instructor), strings.TrimSpace(form.Trainee)
	switch {
	case strings.TrimSpace(form.ScenarioID) == "":
		return s.engine.Snapshot(), notice("Choose a scenario to start.")
	case form.Instructor == "":
		return s.engine.Snapshot(), notice("Enter the instructor's name.")
	case form.Trainee == "":
		return s.engine.Snapshot(), notice("Enter the trainee's name.")
	case utf8.RuneCountInString(form.Instructor) > maxNameLength, utf8.RuneCountInString(form.Trainee) > maxNameLength:
		return s.engine.Snapshot(), notice(fmt.Sprintf("Names can be at most %d characters.", maxNameLength))
	}

	reg, err := s.catalogue()
	if reg == nil {
		return s.engine.Snapshot(), fmt.Errorf("load the scenarios: %w", err)
	}
	entry, ok := reg.Get(form.ScenarioID)
	if !ok {
		return s.engine.Snapshot(), notice(fmt.Sprintf("There is no scenario %q.", form.ScenarioID))
	}

	err = s.engine.Start(s.ctx, entry.Scenario, engine.StartInput{Instructor: form.Instructor, Trainee: form.Trainee, Params: form.Params})
	return s.finish(err)
}

// Current returns the run's state, or an idle snapshot when there is none. The
// frontend calls it once on load and then follows run:state.
func (s *RunService) Current() engine.Snapshot { return s.engine.Snapshot() }

// Retry tears a failed deployment down and deploys it again.
func (s *RunService) Retry() (engine.Snapshot, error) { return s.finish(s.engine.Retry(s.ctx)) }

// RunAction applies the active breakage step's change, then checks the result.
func (s *RunService) RunAction(stepID string) (engine.Snapshot, error) {
	if err := s.requireActive(stepID); err != nil {
		return s.engine.Snapshot(), err
	}
	return s.finish(s.engine.Apply(s.ctx))
}

// Verify runs the active step's check. It can be run as often as needed.
func (s *RunService) Verify(stepID string) (engine.Snapshot, error) {
	if err := s.requireActive(stepID); err != nil {
		return s.engine.Snapshot(), err
	}
	return s.finish(s.engine.Verify(s.ctx))
}

// Continue completes the active step, which needs a passing verification, and
// opens the next one.
func (s *RunService) Continue(stepID string) (engine.Snapshot, error) {
	if err := s.requireActive(stepID); err != nil {
		return s.engine.Snapshot(), err
	}
	return s.finish(s.engine.Continue(s.ctx))
}

// SaveNotes saves the instructor's notes for a step.
func (s *RunService) SaveNotes(stepID, text string) (engine.Snapshot, error) {
	if utf8.RuneCountInString(text) > maxNotesLength {
		return s.engine.Snapshot(), notice(fmt.Sprintf("Notes can be at most %d characters.", maxNotesLength))
	}
	return s.finish(s.engine.SetNotes(stepID, text))
}

// SetRating saves the instructor's rating of a step, from 1 to 5, or 0 to clear it.
func (s *RunService) SetRating(stepID string, rating int) (engine.Snapshot, error) {
	if rating < 0 || rating > 5 {
		return s.engine.Snapshot(), notice("Choose a rating from 1 to 5.")
	}
	return s.finish(s.engine.SetRating(stepID, rating))
}

// Rollback returns the run to the start of a step that the scenario allows it to.
func (s *RunService) Rollback(stepID string) (engine.Snapshot, error) {
	if strings.TrimSpace(stepID) == "" {
		return s.engine.Snapshot(), notice("Choose the step to roll back to.")
	}
	return s.finish(s.engine.Rollback(s.ctx, stepID))
}

// End stops the run, removes everything it created and returns the final state.
// It also stops an operation that is still running.
func (s *RunService) End() (engine.Snapshot, error) {
	snap, err := s.engine.End(s.ctx)
	s.emit(EventRunState, s.engine.Snapshot())
	return snap, plain(err)
}

// NodeAction stops, starts, removes or revives one node.
func (s *RunService) NodeAction(node, action string) (engine.Snapshot, error) {
	if strings.TrimSpace(node) == "" {
		return s.engine.Snapshot(), notice("Choose a node.")
	}
	a := engine.NodeAction(action)
	switch a {
	case engine.ActionStop, engine.ActionStart, engine.ActionRemove, engine.ActionRevive:
	default:
		return s.engine.Snapshot(), notice(fmt.Sprintf("%q is not something a node can do.", action))
	}
	return s.finish(s.engine.NodeAction(s.ctx, node, a))
}

// requireActive refuses a step action meant for a step that is no longer the
// active one, as when the page shows an older state than the run.
func (s *RunService) requireActive(stepID string) error {
	snap := s.engine.Snapshot()
	if snap.ActiveStep < 0 || snap.ActiveStep >= len(snap.Steps) || snap.Steps[snap.ActiveStep].ID != stepID {
		return notice("That step is not the one in progress. The page may be out of date, so check the current step.")
	}
	return nil
}

// finish publishes the state after an operation, which the event stream may have
// missed, and returns it with the error in plain language.
func (s *RunService) finish(err error) (engine.Snapshot, error) {
	snap := s.engine.Snapshot()
	s.emit(EventRunState, snap)
	return snap, plain(err)
}
