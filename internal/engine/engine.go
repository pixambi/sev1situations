// Package engine owns the single run: it deploys a scenario, moves the
// instructor through its steps, records times, notes and ratings, rolls back to
// a reset point and tears everything down (R-SCN-8, sections 4.2, 4.3, 4.6 and
// 4.7).
//
// There is one run at a time. Every operation is exclusive and runs under one
// cancelable context owned by the run, so ending the scenario stops whatever is
// in flight before the environment is destroyed (R-APP-6). The engine does not
// know how a scenario is deployed: that comes in through the Deployer
// interface, and what it records for the report comes out as events and
// snapshots.
package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/runtime"
	"github.com/pixambi/sev1situations/internal/scenario"
)

// Errors callers can match with errors.Is.
var (
	// ErrBusy means another operation is in progress, or a run already exists
	// (R-SCN-8).
	ErrBusy = errors.New("another operation is in progress")
	// ErrNoRun means there is no run to act on.
	ErrNoRun = errors.New("no scenario is running")
	// ErrWrongState means the operation is not allowed in the run's or the
	// step's current state, for example Continue before the verification passed.
	ErrWrongState = errors.New("not allowed in this state")
)

// Deployment is what a successful deploy hands back.
type Deployment struct {
	// Nodes are the deployment's containers by node name, for example node-1.
	Nodes map[string]primitive.Node
	// Actions declares which node actions each node offers, by node name
	// (R-VIEW-6). The product decides: a node with no entry offers none.
	Actions map[string][]NodeAction
	// Creds are credentials keyed by cluster or service name, for example
	// primary -> rootToken. They are used in templates and redacted from
	// output. They never leave the engine in a Snapshot (R-REPORT-5).
	Creds map[string]map[string]string
}

// Deployer builds a scenario's environment from its topology and the chosen
// parameters (R-SCN-11). The deploy package implements it.
type Deployer interface {
	// Deploy builds the environment for run. progress receives status lines
	// shown to the instructor. A failed deploy leaves its containers in place
	// so their logs can be read; the engine removes them on Retry or End.
	Deploy(ctx context.Context, run runtime.Run, sc *scenario.Scenario, params map[string]any, progress func(string)) (Deployment, error)
}

// Deps are what an Engine needs.
type Deps struct {
	// Registry runs the steps' primitives.
	Registry *primitive.Registry
	// Deployer builds the environment.
	Deployer Deployer
	// Runtime removes a run's objects on teardown.
	Runtime runtime.Runtime
	// OnEvent, if set, receives every event as it happens. It must not block.
	OnEvent func(Event)
	// Clock returns the current time. It defaults to time.Now.
	Clock func() time.Time
	// NewRunID returns a short unique run ID (D6). It defaults to random hex.
	NewRunID func() string
	// TeardownTimeout bounds how long teardown may take. It defaults to two minutes.
	TeardownTimeout time.Duration
}

// StartInput is what the Start step collects (R-SCN-11).
type StartInput struct {
	// Instructor is the instructor's name.
	Instructor string
	// Trainee is the trainee's name.
	Trainee string
	// Params are the scenario's parameter values by parameter ID. Missing
	// values take the parameter's default.
	Params map[string]any
}

// Engine runs one scenario at a time.
type Engine struct {
	deps Deps

	mu   sync.Mutex
	cond *sync.Cond
	run  *run
	busy bool
	subs []*subscriber
}

// New returns an idle engine.
func New(deps Deps) *Engine {
	if deps.Clock == nil {
		deps.Clock = time.Now
	}
	if deps.NewRunID == nil {
		deps.NewRunID = randomRunID
	}
	if deps.TeardownTimeout <= 0 {
		deps.TeardownTimeout = 2 * time.Minute
	}
	e := &Engine{deps: deps}
	e.cond = sync.NewCond(&e.mu)
	return e
}

func randomRunID() string {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%06x", time.Now().UnixNano()&0xffffff)
	}
	return hex.EncodeToString(b)
}

// Start begins a run: it checks the inputs, then deploys the scenario. It
// returns when the deployment is healthy or has failed. After a failure the run
// stays, in StatusDeployFailed, so the instructor can Retry or End it.
func (e *Engine) Start(ctx context.Context, sc *scenario.Scenario, in StartInput) error {
	in.Instructor, in.Trainee = strings.TrimSpace(in.Instructor), strings.TrimSpace(in.Trainee)
	if in.Instructor == "" || in.Trainee == "" {
		return errors.New("start scenario: the instructor's and the trainee's names are required")
	}
	params, err := ResolveParams(sc, in.Params)
	if err != nil {
		return fmt.Errorf("start scenario: %w", err)
	}

	e.mu.Lock()
	if e.run != nil {
		e.mu.Unlock()
		return fmt.Errorf("start scenario: %w: a scenario is already running", ErrBusy)
	}
	runCtx, cancel := context.WithCancel(context.Background())
	r := &run{
		id: e.deps.NewRunID(), sc: sc, params: params, instructor: in.Instructor, trainee: in.Trainee,
		ctx: runCtx, cancel: cancel, status: StatusDeploying, startedAt: e.deps.Clock(),
		outputs: map[string]string{}, resumeFrom: -1,
	}
	for _, st := range sc.Steps {
		r.steps = append(r.steps, &stepState{def: st, state: StepLocked})
	}
	e.run = r
	e.busy = true
	e.mu.Unlock()

	e.emit(r, EventRunStarted, "", fmt.Sprintf("%s started %s for %s", in.Instructor, sc.ID, in.Trainee))
	defer e.release()
	return e.deploy(ctx, r)
}

// Retry tears a failed deployment down and deploys again.
func (e *Engine) Retry(ctx context.Context) error {
	r, opCtx, done, err := e.begin(ctx)
	if err != nil {
		return err
	}
	defer done()

	e.mu.Lock()
	if r.status != StatusDeployFailed {
		e.mu.Unlock()
		return fmt.Errorf("retry: %w: the deployment has not failed", ErrWrongState)
	}
	r.status = StatusDeploying
	r.deployErr = ""
	e.mu.Unlock()

	e.emit(r, EventRetry, "", "retrying the deployment")
	return e.redeploy(opCtx, r)
}

// deploy builds the environment for a run that has none.
func (e *Engine) deploy(ctx context.Context, r *run) error {
	opCtx, stop := e.merge(ctx, r)
	defer stop()
	return e.redeployFrom(opCtx, r, false)
}

// redeploy removes whatever the run has and builds the environment again.
func (e *Engine) redeploy(ctx context.Context, r *run) error {
	return e.redeployFrom(ctx, r, true)
}

func (e *Engine) redeployFrom(ctx context.Context, r *run, removeFirst bool) error {
	if removeFirst {
		if err := e.removeObjects(r); err != nil {
			e.failDeploy(r, fmt.Errorf("remove the old environment: %w", err))
			return fmt.Errorf("deploy: %w", err)
		}
	}

	progress := func(line string) { e.emit(r, EventDeployProgress, "", line) }
	dep, err := e.deps.Deployer.Deploy(ctx, runtime.Run{ID: r.id, Scenario: r.sc.ID}, r.sc, r.params, progress)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err() // the run is ending, so there is nothing to retry
		}
		e.failDeploy(r, err)
		return fmt.Errorf("deploy %s: %w", r.sc.ID, err)
	}

	e.mu.Lock()
	r.deployment = dep
	r.redactor = redactorFor(dep)
	r.nodeState = map[string]NodeState{}
	for name := range dep.Nodes {
		r.nodeState[name] = NodeRunning
	}
	e.mu.Unlock()
	e.refreshNodes(ctx, r)
	e.emit(r, EventDeployComplete, "", "the deployment is healthy")

	// A rollback that had to redeploy carries on by replaying the actions.
	if r.resumeFrom >= 0 {
		return e.finishRollback(ctx, r)
	}
	e.activateFirst(r)
	return nil
}

func (e *Engine) failDeploy(r *run, err error) {
	e.mu.Lock()
	r.status = StatusDeployFailed
	r.deployErr = err.Error()
	e.mu.Unlock()
	e.emit(r, EventDeployFailed, "", err.Error())
}

// activateFirst makes the run ready and the first step active. A scenario with
// no steps is a sandbox and just stays ready (R-SCN-17).
func (e *Engine) activateFirst(r *run) {
	e.mu.Lock()
	r.status = StatusActive
	if len(r.steps) > 0 {
		e.activateLocked(r, 0)
	}
	e.mu.Unlock()
	if len(r.steps) > 0 {
		e.emit(r, EventStepActive, r.steps[0].def.ID, "step "+r.steps[0].def.ID+" is active")
	}
}

func redactorFor(d Deployment) *primitive.Redactor {
	var secrets []string
	for _, creds := range d.Creds {
		for _, v := range creds {
			secrets = append(secrets, v)
		}
	}
	return primitive.NewRedactor(secrets...)
}

// begin starts an exclusive operation on the current run. The returned context
// ends when the caller's context ends or the run is ended. done must be called
// when the operation finishes.
func (e *Engine) begin(ctx context.Context) (*run, context.Context, func(), error) {
	e.mu.Lock()
	r := e.run
	switch {
	case r == nil:
		e.mu.Unlock()
		return nil, nil, nil, ErrNoRun
	case e.busy:
		e.mu.Unlock()
		return nil, nil, nil, ErrBusy
	}
	e.busy = true
	e.mu.Unlock()

	opCtx, stop := e.merge(ctx, r)
	return r, opCtx, func() { stop(); e.release() }, nil
}

// merge returns a context that ends when either the caller's context or the
// run's context ends.
func (e *Engine) merge(ctx context.Context, r *run) (context.Context, func()) {
	opCtx, cancel := context.WithCancel(r.ctx)
	stop := context.AfterFunc(ctx, cancel)
	return opCtx, func() { stop(); cancel() }
}

func (e *Engine) release() {
	e.mu.Lock()
	e.busy = false
	e.cond.Broadcast()
	e.mu.Unlock()
}
