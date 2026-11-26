package engine

import (
	"maps"
	"slices"
	"time"

	"github.com/pixambi/sev1situations/internal/scenario"
)

// Snapshot is a copy of the run's state for the UI and the report. It never
// holds a credential (R-REPORT-5, R-VIEW-5).
type Snapshot struct {
	// Status is the state of the run as a whole.
	Status Status
	// RunID identifies the run.
	RunID string
	// ScenarioID and ScenarioName say which scenario is running.
	ScenarioID, ScenarioName string
	// Instructor and Trainee are the names entered on the Start step.
	Instructor, Trainee string
	// Params are the chosen parameter values.
	Params map[string]any
	// StartedAt is when the run began, and EndedAt when it finished or was ended.
	StartedAt, EndedAt time.Time
	// Result is "completed" or "ended early" once the run is over, else empty.
	Result string
	// DeployError is why the deployment failed, if it did.
	DeployError string
	// ActiveStep is the index of the step in progress, or -1.
	ActiveStep int
	// Steps lists every step in order.
	Steps []StepView
	// Events lists everything that happened, in order.
	Events []Event
}

// StepView is one step as the UI and the report see it.
type StepView struct {
	// ID, Title and Kind come from the scenario.
	ID, Title, Kind string
	// Guide is the path of the step's guidance file.
	Guide string
	// State is the step's state.
	State StepState
	// Verified reports that the verification passed, which is what lets Continue through.
	Verified bool
	// ResetPoint reports that the scenario lets the instructor roll back to this step.
	ResetPoint bool
	// Started and Ended are the current attempt's times. Elapsed is the total
	// time spent on the step, including earlier attempts before a rollback.
	Started, Ended time.Time
	Elapsed        time.Duration
	// Attempts counts how many times the step has been activated.
	Attempts int
	// Notes and Rating are the instructor's notes and 1 to 5 rating (0 is none).
	Notes  string
	Rating int
	// Last is how the most recent operation on the step ended.
	Last *Outcome
	// CanApply, CanVerify, CanContinue and CanRollBack say which buttons are
	// enabled. The UI shows them and decides nothing itself (R-UI-5).
	CanApply, CanVerify, CanContinue, CanRollBack bool
}

// Snapshot returns a copy of the current state. With no run it returns an idle
// snapshot.
func (e *Engine) Snapshot() Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.run == nil {
		return Snapshot{Status: StatusIdle, ActiveStep: -1}
	}
	return e.snapshotLocked(e.run)
}

func (e *Engine) snapshotLocked(r *run) Snapshot {
	now := e.deps.Clock()
	end := r.finishedAt
	idle := !e.busy

	s := Snapshot{
		Status: r.status, RunID: r.id, ScenarioID: r.sc.ID, ScenarioName: r.sc.Name,
		Instructor: r.instructor, Trainee: r.trainee, Params: maps.Clone(r.params),
		StartedAt: r.startedAt, EndedAt: end, Result: r.result, DeployError: r.deployErr, ActiveStep: -1,
		Events: slices.Clone(r.events),
	}
	active := r.activeIndex()
	s.ActiveStep = active

	for i, st := range r.steps {
		v := StepView{
			ID: st.def.ID, Title: st.def.Title, Kind: st.def.Kind, Guide: st.def.Guide,
			State: st.state, Verified: st.verified, ResetPoint: st.def.ResetPoint,
			Started: st.started, Ended: st.ended, Attempts: st.attempts,
			Notes: st.notes, Rating: st.rating,
		}
		if st.last != nil {
			last := *st.last
			v.Last = &last
		}
		v.Elapsed = st.prior
		switch {
		case !st.started.IsZero() && !st.ended.IsZero():
			v.Elapsed += st.ended.Sub(st.started)
		case !st.started.IsZero():
			v.Elapsed += now.Sub(st.started)
		}

		if r.status == StatusActive && idle {
			isActive := i == active
			v.CanApply = isActive && st.def.Kind == scenario.KindBreakage && st.state == StepActive
			v.CanVerify = isActive && (st.def.Kind == scenario.KindVerification && st.state == StepActive || st.def.Kind == scenario.KindBreakage && st.state == StepApplied)
			v.CanContinue = isActive && st.verified
			v.CanRollBack = st.def.ResetPoint && st.state == StepComplete
		}
		s.Steps = append(s.Steps, v)
	}
	return s
}

// activeIndex returns the index of the step in progress, or -1.
func (r *run) activeIndex() int {
	for i, st := range r.steps {
		if st.state == StepActive || st.state == StepApplying || st.state == StepApplied {
			return i
		}
	}
	return -1
}
