package engine

import (
	"context"
	"time"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/scenario"
)

// Status is the state of the run as a whole.
type Status string

const (
	// StatusIdle means there is no run.
	StatusIdle Status = "idle"
	// StatusDeploying means the Start step is building the environment.
	StatusDeploying Status = "deploying"
	// StatusDeployFailed means the deployment failed. The instructor can Retry or End.
	StatusDeployFailed Status = "deploy-failed"
	// StatusActive means the environment is up and the steps are in progress. A
	// sandbox stays here until it is ended.
	StatusActive Status = "active"
	// StatusFinished means the final step is complete. End closes the run.
	StatusFinished Status = "finished"
)

// StepState is the state of one step (4.6).
type StepState string

const (
	// StepLocked means an earlier step is not complete.
	StepLocked StepState = "locked"
	// StepActive means the step is the one in progress.
	StepActive StepState = "active"
	// StepApplying means a breakage is being applied.
	StepApplying StepState = "applying"
	// StepApplied means a breakage has been applied and is being verified.
	StepApplied StepState = "applied"
	// StepComplete means the step is done.
	StepComplete StepState = "complete"
)

// Outcome is how an operation on a step ended.
type Outcome struct {
	// Status is "pass", "fail" (it ran and the condition was not met) or
	// "error" (it could not run). They look different in the UI (4.6).
	Status string
	// Message is a short explanation.
	Message string
	// Evidence is captured output for the instructor, with credentials removed.
	Evidence string
	// At is when the operation ended.
	At time.Time
}

// Outcome statuses.
const (
	outcomePass  = "pass"
	outcomeFail  = "fail"
	outcomeError = "error"
)

// EventKind says what an event records.
type EventKind string

// Event kinds. The rollbacks, retries and the early end are what the report
// lists as events (R-REPORT-5).
const (
	EventRunStarted     EventKind = "run-started"
	EventDeployProgress EventKind = "deploy-progress"
	EventDeployComplete EventKind = "deploy-complete"
	EventDeployFailed   EventKind = "deploy-failed"
	EventStepActive     EventKind = "step-active"
	EventStepApplying   EventKind = "step-applying"
	EventStepApplied    EventKind = "step-applied"
	EventActionFailed   EventKind = "action-failed"
	EventVerification   EventKind = "verification"
	EventStepComplete   EventKind = "step-complete"
	EventRetry          EventKind = "retry"
	EventRollback       EventKind = "rollback"
	EventRollbackFailed EventKind = "rollback-failed"
	EventRunFinished    EventKind = "run-finished"
	EventRunEnded       EventKind = "run-ended"
	EventTeardown       EventKind = "teardown"
	EventNodeAction     EventKind = "node-action"
)

// Event is one thing that happened during a run.
type Event struct {
	// RunID is the run the event belongs to.
	RunID string
	// Seq numbers the run's events from 1.
	Seq int
	// Time is when it happened.
	Time time.Time
	// Kind says what happened.
	Kind EventKind
	// Step is the step it concerns, if any.
	Step string
	// Message describes it. It never holds a credential.
	Message string
}

// run is the state of the one run. The Engine's mutex guards every field.
type run struct {
	id         string
	sc         *scenario.Scenario
	params     map[string]any
	instructor string
	trainee    string
	ctx        context.Context
	cancel     context.CancelFunc

	status     Status
	startedAt  time.Time
	finishedAt time.Time
	deployErr  string
	result     string
	deployment Deployment
	nodeState  map[string]NodeState
	redactor   *primitive.Redactor
	steps      []*stepState
	events     []Event
	outputs    map[string]string

	// resumeFrom is the index of the reset point a rollback is carrying out, or
	// -1. A rollback that has to redeploy can fail and be retried, and this
	// remembers where to carry on.
	resumeFrom int

	// ending is set once End has begun, so a second End is refused.
	ending bool
}

// stepState is the engine's state for one step.
type stepState struct {
	def      scenario.Step
	state    StepState
	verified bool
	started  time.Time
	ended    time.Time
	prior    time.Duration // time spent in earlier attempts, kept across rollbacks
	attempts int
	notes    string
	rating   int
	last     *Outcome
}

// emit records an event and passes it to the sink and the subscribers. It must be called without
// the mutex held, because the sink may call back into the engine.
func (e *Engine) emit(r *run, kind EventKind, step, message string) {
	e.mu.Lock()
	ev := Event{RunID: r.id, Seq: len(r.events) + 1, Time: e.deps.Clock(), Kind: kind, Step: step, Message: message}
	r.events = append(r.events, ev)
	sink := e.deps.OnEvent
	subs := append([]*subscriber(nil), e.subs...)
	e.mu.Unlock()

	if sink != nil {
		sink(ev)
	}
	for _, s := range subs {
		s.send(ev) // a full subscriber misses the event rather than stalling the run
	}
}

// activateLocked makes step i the active one. The mutex must be held.
func (e *Engine) activateLocked(r *run, i int) {
	st := r.steps[i]
	st.state = StepActive
	st.verified = false
	st.started = e.deps.Clock()
	st.ended = time.Time{}
	st.attempts++
}
