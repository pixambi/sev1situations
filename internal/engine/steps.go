package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/runtime"
	"github.com/pixambi/sev1situations/internal/scenario"
)

// Apply runs the active breakage step's action, then verifies that the
// environment is in the expected broken state (4.2). It returns an error only
// when the operation could not be attempted. How the action itself went is in
// the step's Last outcome: if it failed or errored the step stays active and
// can be retried (4.6).
func (e *Engine) Apply(ctx context.Context) error {
	r, opCtx, done, err := e.begin(ctx)
	if err != nil {
		return err
	}
	defer done()

	st, err := e.claim(r, func(st *stepState) error {
		if st.def.Kind != scenario.KindBreakage || st.state != StepActive {
			return fmt.Errorf("apply: %w: step %q has nothing to apply right now", ErrWrongState, st.def.ID)
		}
		return nil
	})
	if err != nil {
		return err
	}

	e.mu.Lock()
	retrying := st.last != nil && st.last.Status != outcomePass
	st.state = StepApplying
	e.mu.Unlock()
	if retrying {
		e.emit(r, EventRetry, st.def.ID, "retrying step "+st.def.ID)
	}
	e.emit(r, EventStepApplying, st.def.ID, "applying step "+st.def.ID)

	act := e.runCalls(opCtx, r, st.def.Action, "applied")
	if opCtx.Err() != nil {
		return opCtx.Err()
	}
	e.refreshNodes(opCtx, r) // an action may have stopped or removed nodes
	if act.Status != outcomePass {
		e.mu.Lock()
		st.state, st.last = StepActive, &act
		e.mu.Unlock()
		e.emit(r, EventActionFailed, st.def.ID, act.Status+": "+act.Message)
		return nil
	}

	e.mu.Lock()
	st.state = StepApplied
	e.mu.Unlock()
	e.emit(r, EventStepApplied, st.def.ID, "the breakage is applied")
	return e.verifyStep(opCtx, r, st)
}

// Verify runs the active step's verification: for a verification step, the
// check that the fix worked, and for a breakage step that has been applied, the
// check that the environment is broken as expected. It can be run as often as
// needed. Only a pass lets Continue through (R-SCN-9).
func (e *Engine) Verify(ctx context.Context) error {
	r, opCtx, done, err := e.begin(ctx)
	if err != nil {
		return err
	}
	defer done()

	st, err := e.claim(r, func(st *stepState) error {
		ok := st.def.Kind == scenario.KindVerification && st.state == StepActive ||
			st.def.Kind == scenario.KindBreakage && st.state == StepApplied
		if !ok {
			return fmt.Errorf("verify: %w: step %q has nothing to verify right now", ErrWrongState, st.def.ID)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return e.verifyStep(opCtx, r, st)
}

func (e *Engine) verifyStep(ctx context.Context, r *run, st *stepState) error {
	out := e.runCalls(ctx, r, st.def.Verify, "all checks passed")
	if ctx.Err() != nil {
		return ctx.Err()
	}
	e.refreshNodes(ctx, r)
	e.mu.Lock()
	st.verified = out.Status == outcomePass
	st.last = &out
	e.mu.Unlock()
	e.emit(r, EventVerification, st.def.ID, out.Status+": "+out.Message)
	return nil
}

// Continue completes the active step and activates the next one. It works only
// after a successful verification: a step can never be skipped (R-SCN-9). After
// the last step the run is finished.
func (e *Engine) Continue(ctx context.Context) error {
	r, _, done, err := e.begin(ctx)
	if err != nil {
		return err
	}
	defer done()

	e.mu.Lock()
	idx := r.activeIndex()
	if r.status != StatusActive || idx < 0 {
		e.mu.Unlock()
		return fmt.Errorf("continue: %w: there is no active step", ErrWrongState)
	}
	st := r.steps[idx]
	if !st.verified {
		e.mu.Unlock()
		return fmt.Errorf("continue: %w: step %q has not been verified", ErrWrongState, st.def.ID)
	}
	now := e.deps.Clock()
	st.state, st.ended = StepComplete, now
	last := idx == len(r.steps)-1
	if last {
		r.status, r.finishedAt, r.result = StatusFinished, now, resultCompleted
	} else {
		e.activateLocked(r, idx+1)
	}
	e.mu.Unlock()

	e.emit(r, EventStepComplete, st.def.ID, "step "+st.def.ID+" is complete")
	if last {
		e.emit(r, EventRunFinished, "", "the final step is complete")
		return nil
	}
	next := r.steps[idx+1].def.ID
	e.emit(r, EventStepActive, next, "step "+next+" is active")
	return nil
}

// SetNotes saves the instructor's notes for a step that has been reached. They
// appear in the report with the step's time frame (R-REPORT-5).
func (e *Engine) SetNotes(stepID, notes string) error {
	return e.annotate(stepID, func(st *stepState) error { st.notes = notes; return nil })
}

// SetRating saves the instructor's rating of a step, from 1 to 5. Zero clears it.
func (e *Engine) SetRating(stepID string, rating int) error {
	return e.annotate(stepID, func(st *stepState) error {
		if rating < 0 || rating > 5 {
			return fmt.Errorf("rating %d must be from 1 to 5, or 0 to clear it", rating)
		}
		st.rating = rating
		return nil
	})
}

func (e *Engine) annotate(stepID string, set func(*stepState) error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.run == nil {
		return ErrNoRun
	}
	for _, st := range e.run.steps {
		if st.def.ID != stepID {
			continue
		}
		if st.state == StepLocked {
			return fmt.Errorf("step %q: %w: it has not been reached yet", stepID, ErrWrongState)
		}
		return set(st)
	}
	return fmt.Errorf("there is no step %q", stepID)
}

// claim returns the active step after check approves it, in one locked step so
// the state cannot change between the check and the use.
func (e *Engine) claim(r *run, check func(*stepState) error) (*stepState, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	idx := r.activeIndex()
	if r.status != StatusActive || idx < 0 {
		return nil, fmt.Errorf("%w: there is no active step", ErrWrongState)
	}
	if err := check(r.steps[idx]); err != nil {
		return nil, err
	}
	return r.steps[idx], nil
}

const resultCompleted = "completed"

// runCalls runs a step's calls in order. It stops at the first one that fails or
// errors, and the outcome says which. Each call's arguments are rendered with
// the run's values first, and what each produces is kept for later calls.
func (e *Engine) runCalls(ctx context.Context, r *run, calls []scenario.Call, passMessage string) (out Outcome) {
	out = Outcome{Status: outcomePass, Message: passMessage}
	var evidence []string
	defer func() {
		out.Evidence = strings.Join(evidence, "\n")
		out.At = e.deps.Clock()
	}()

	for _, c := range calls {
		args, env, err := e.prepare(r, c)
		if err != nil {
			out.Status, out.Message = outcomeError, err.Error()
			return out
		}
		res, err := e.deps.Registry.Run(ctx, c.Primitive, env, args)
		if err != nil {
			out.Status, out.Message = outcomeError, err.Error()
			return out
		}
		if res.Evidence != "" {
			evidence = append(evidence, c.Primitive+":\n"+res.Evidence)
		}
		e.mu.Lock()
		for k, v := range res.Outputs {
			r.outputs[k] = v
		}
		e.mu.Unlock()

		if res.Status != primitive.Pass {
			out.Status, out.Message = outcomeFail, c.Primitive+": "+res.Message
			return out
		}
	}
	return out
}

// prepare renders a call's arguments and builds the environment it runs in.
func (e *Engine) prepare(r *run, c scenario.Call) (map[string]any, primitive.Env, error) {
	e.mu.Lock()
	data := e.templateData(r)
	env := primitive.Env{
		Runtime:  e.deps.Runtime,
		Run:      runtime.Run{ID: r.id, Scenario: r.sc.ID},
		Nodes:    r.deployment.Nodes,
		Creds:    r.deployment.Creds,
		ReadFile: r.sc.ReadFile,
		Render:   func(src string) (string, error) { return scenario.Render(src, data) },
		Redactor: r.redactor,
	}
	e.mu.Unlock()

	args, err := scenario.RenderArgs(c.Args, data)
	if err != nil {
		return nil, env, fmt.Errorf("%s: %w", c.Primitive, err)
	}
	return args, env, nil
}

// templateData builds what templates can read. The mutex must be held.
func (e *Engine) templateData(r *run) scenario.TemplateData {
	d := scenario.TemplateData{
		Params:  r.params,
		Nodes:   map[string]scenario.NodeData{},
		Creds:   r.deployment.Creds,
		Outputs: map[string]string{},
	}
	for name, n := range r.deployment.Nodes {
		d.Nodes[scenario.TemplateKey(name)] = scenario.NodeData{Name: n.Name, IP: n.IP, Cluster: n.Cluster}
	}
	for k, v := range r.outputs {
		d.Outputs[k] = v
	}
	return d
}
