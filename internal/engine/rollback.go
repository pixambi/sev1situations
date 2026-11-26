package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/pixambi/sev1situations/internal/scenario"
)

// Rollback returns the run to a reset point (R-SCN-6, 4.7). Only a step the
// scenario flags as a reset point, and that is already complete, can be rolled
// back to.
//
// By default the environment is destroyed and deployed again, then the actions
// of the steps up to and including the reset point are replayed. The fixes the
// trainee made are not replayed, so the step after the last replayed action
// becomes the active one. A step can instead declare its own reset calls, which
// are run in place of the redeploy and replay. Step times and notes are kept and
// the rollback is recorded as an event.
//
// If the redeploy or the replay fails, the run is left in StatusDeployFailed and
// Retry carries on from where it stopped.
func (e *Engine) Rollback(ctx context.Context, stepID string) error {
	r, opCtx, done, err := e.begin(ctx)
	if err != nil {
		return err
	}
	defer done()

	e.mu.Lock()
	idx := -1
	for i, st := range r.steps {
		if st.def.ID == stepID {
			idx = i
		}
	}
	switch {
	case idx < 0:
		e.mu.Unlock()
		return fmt.Errorf("roll back: there is no step %q", stepID)
	case r.status != StatusActive:
		e.mu.Unlock()
		return fmt.Errorf("roll back: %w: the run is not in progress", ErrWrongState)
	case !r.steps[idx].def.ResetPoint:
		e.mu.Unlock()
		return fmt.Errorf("roll back: %w: step %q is not a reset point", ErrWrongState, stepID)
	case r.steps[idx].state != StepComplete:
		e.mu.Unlock()
		return fmt.Errorf("roll back: %w: step %q has not been completed yet", ErrWrongState, stepID)
	}
	r.resumeFrom = idx
	step := r.steps[idx]
	e.mu.Unlock()

	e.emit(r, EventRollback, stepID, "rolling back to step "+stepID)

	if len(step.def.Reset) > 0 {
		out := e.runCalls(opCtx, r, step.def.Reset, "reset")
		if opCtx.Err() != nil {
			return opCtx.Err()
		}
		if out.Status != outcomePass {
			e.mu.Lock()
			r.resumeFrom = -1
			e.mu.Unlock()
			e.emit(r, EventRollbackFailed, stepID, out.Status+": "+out.Message)
			return fmt.Errorf("roll back to %q: the reset failed: %s", stepID, out.Message)
		}
		return e.finishRollback(opCtx, r)
	}

	e.mu.Lock()
	r.status = StatusDeploying
	e.mu.Unlock()
	return e.redeploy(opCtx, r)
}

// finishRollback replays the actions up to the reset point (unless the step
// reset the environment itself) and then puts the steps in their rolled-back
// states.
func (e *Engine) finishRollback(ctx context.Context, r *run) error {
	e.mu.Lock()
	idx := r.resumeFrom
	custom := len(r.steps[idx].def.Reset) > 0
	last := -1 // the last step up to the reset point that has an action
	for i := 0; i <= idx; i++ {
		if len(r.steps[i].def.Action) > 0 {
			last = i
		}
	}
	e.mu.Unlock()

	if !custom {
		for i := 0; i <= last; i++ {
			if err := e.replayStep(ctx, r, i); err != nil {
				return err
			}
		}
	}

	e.mu.Lock()
	now := e.deps.Clock()
	for i, st := range r.steps {
		if i <= last {
			continue // the actions were replayed, so these stay complete
		}
		if !st.started.IsZero() {
			end := st.ended
			if end.IsZero() {
				end = now
			}
			st.prior += end.Sub(st.started)
		}
		st.state, st.verified, st.last = StepLocked, false, nil
		st.started, st.ended = time.Time{}, time.Time{}
	}
	r.status, r.resumeFrom = StatusActive, -1
	next := last + 1
	e.activateLocked(r, next)
	id, nextID := r.steps[idx].def.ID, r.steps[next].def.ID
	e.mu.Unlock()

	e.refreshNodes(ctx, r)
	e.emit(r, EventRollback, id, fmt.Sprintf("rolled back to step %s, step %s is active", id, nextID))
	e.emit(r, EventStepActive, nextID, "step "+nextID+" is active")
	return nil
}

// replayStep applies a step's action again and confirms the environment is
// broken as before.
func (e *Engine) replayStep(ctx context.Context, r *run, i int) error {
	e.mu.Lock()
	st := r.steps[i]
	e.mu.Unlock()
	if len(st.def.Action) == 0 {
		return nil
	}

	phases := []struct {
		name  string
		calls []scenario.Call
	}{{"action", st.def.Action}, {"check", st.def.Verify}}
	for _, phase := range phases {
		out := e.runCalls(ctx, r, phase.calls, phase.name)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if out.Status != outcomePass {
			msg := fmt.Sprintf("replaying step %s, %s: %s", st.def.ID, phase.name, out.Message)
			e.failDeploy(r, errors.New(msg))
			e.emit(r, EventRollbackFailed, st.def.ID, msg)
			return fmt.Errorf("roll back: %s", msg)
		}
	}
	return nil
}

// End closes the run: it stops anything in flight, destroys everything the run
// created, and returns the final snapshot, from which the report is written
// (R-SCN-10, R-REPORT-6). The run counts as completed if its final step was
// completed or it is a sandbox, and as ended early otherwise.
//
// Teardown goes on past a failed removal. The failures are returned with the
// snapshot and recorded as an event, and the run is closed either way, so the
// instructor is never left guessing (section 7).
func (e *Engine) End(ctx context.Context) (Snapshot, error) {
	e.mu.Lock()
	r := e.run
	if r == nil {
		e.mu.Unlock()
		return Snapshot{}, ErrNoRun
	}
	if r.ending {
		e.mu.Unlock()
		return Snapshot{}, fmt.Errorf("end: %w: the run is already ending", ErrBusy)
	}
	r.ending = true
	r.cancel() // stop whatever is in flight
	for e.busy {
		e.cond.Wait()
	}
	e.busy = true // nothing else may start now
	if r.result == "" {
		r.result = "ended early"
		if len(r.steps) == 0 {
			r.result = resultCompleted
		}
	}
	e.mu.Unlock()

	err := e.removeObjects(r)
	if err != nil {
		e.emit(r, EventTeardown, "", "teardown did not finish: "+err.Error())
	} else {
		e.emit(r, EventTeardown, "", "everything the run created was removed")
	}
	e.emit(r, EventRunEnded, "", "the run ended: "+r.result)

	e.mu.Lock()
	if r.finishedAt.IsZero() {
		r.finishedAt = e.deps.Clock()
	}
	snap := e.snapshotLocked(r)
	e.run = nil
	e.busy = false
	e.cond.Broadcast()
	e.mu.Unlock()

	// The report is written whether or not teardown went cleanly: a run that left
	// containers behind is the one an instructor most needs a record of.
	saveErr := e.saveReport(snap, r.redactor)
	if err != nil {
		err = fmt.Errorf("end: teardown: %w", err)
	}
	return snap, errors.Join(err, saveErr)
}

// removeObjects removes everything the run created. It uses its own time limit,
// because the run's context has usually been cancelled by now.
func (e *Engine) removeObjects(r *run) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.ctx), e.deps.TeardownTimeout)
	defer cancel()
	return e.deps.Runtime.RemoveRun(ctx, r.id)
}
