package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/report"
	"github.com/pixambi/sev1situations/internal/runtime"
	"github.com/pixambi/sev1situations/internal/runtime/fake"
	"github.com/pixambi/sev1situations/internal/scenario"
)

const rootToken = "hvs.TestRootTokenValue"

// world is the pretend environment the stub primitives act on. A fresh deploy
// resets it, as a real redeploy would.
type world struct {
	mu    sync.Mutex
	flags map[string]bool
}

func (w *world) set(k string, v bool) { w.mu.Lock(); w.flags[k] = v; w.mu.Unlock() }
func (w *world) get(k string) bool    { w.mu.Lock(); defer w.mu.Unlock(); return w.flags[k] }
func (w *world) reset()               { w.mu.Lock(); w.flags = map[string]bool{}; w.mu.Unlock() }

// fakeDeployer creates one container per node on the fake runtime, resets the
// world, and counts its calls.
type fakeDeployer struct {
	rt    *fake.Runtime
	world *world

	mu      sync.Mutex
	deploys int
	fail    []error // each Deploy takes the next error, if any
	params  []map[string]any
	runs    []runtime.Run

	// actions, if set, replaces the default of every action on every node.
	actions   map[string][]NodeAction
	reviveErr error
	revived   int
}

func (d *fakeDeployer) Deploy(ctx context.Context, run runtime.Run, sc *scenario.Scenario, params map[string]any, progress func(string)) (Deployment, error) {
	d.mu.Lock()
	d.deploys++
	d.params = append(d.params, params)
	d.runs = append(d.runs, run)
	var failure error
	if len(d.fail) > 0 {
		failure, d.fail = d.fail[0], d.fail[1:]
	}
	d.mu.Unlock()

	progress("creating the network")
	nodes := map[string]primitive.Node{}
	for _, name := range sc.NodeNames() {
		c, err := d.rt.StartContainer(ctx, runtime.ContainerOptions{Run: run, Name: name, Image: "alpine:3"})
		if err != nil {
			return Deployment{}, err
		}
		nodes[name] = primitive.Node{Name: name, Cluster: "primary", ContainerID: c.ID, IP: "10.89.1." + fmt.Sprint(len(nodes)+2)}
	}
	if failure != nil {
		return Deployment{}, failure // containers are left behind, as a real failed deploy leaves them
	}
	d.world.reset()
	progress("the cluster is healthy")

	actions := d.actions
	if actions == nil {
		actions = map[string][]NodeAction{}
		for name := range nodes {
			actions[name] = []NodeAction{ActionStop, ActionStart, ActionRemove, ActionRevive}
		}
	}
	return Deployment{Nodes: nodes, Actions: actions, Creds: map[string]map[string]string{"primary": {"rootToken": rootToken}}}, nil
}

// Revive recreates a removed node, as the real deployer would rejoin it to its cluster.
func (d *fakeDeployer) Revive(ctx context.Context, run runtime.Run, _ *scenario.Scenario, _ map[string]any, _ Deployment, node string) (primitive.Node, error) {
	d.mu.Lock()
	d.revived++
	err := d.reviveErr
	d.mu.Unlock()
	if err != nil {
		return primitive.Node{}, err
	}
	c, e := d.rt.StartContainer(ctx, runtime.ContainerOptions{Run: run, Name: node, Image: "alpine:3"})
	if e != nil {
		return primitive.Node{}, e
	}
	return primitive.Node{Name: node, Cluster: "primary", ContainerID: c.ID, IP: "10.89.1.99"}, nil
}

func (d *fakeDeployer) count() int { d.mu.Lock(); defer d.mu.Unlock(); return d.deploys }

// stub primitives -----------------------------------------------------------

type stubPrim struct {
	spec primitive.Spec
	run  func(ctx context.Context, env primitive.Env, a primitive.Args) (primitive.Result, error)
}

func (s stubPrim) Spec() primitive.Spec { return s.spec }
func (s stubPrim) Run(ctx context.Context, env primitive.Env, a primitive.Args) (primitive.Result, error) {
	return s.run(ctx, env, a)
}

func spec(name string, args ...primitive.Arg) primitive.Spec {
	return primitive.Spec{Name: name, Summary: "A test primitive.", Args: args, Timeout: 10 * time.Second}
}

var (
	keyArg   = primitive.Arg{Name: "key", Type: primitive.String, Required: true, Help: "The flag."}
	valueArg = primitive.Arg{Name: "value", Type: primitive.Bool, Default: true, Help: "The value."}
	textArg  = primitive.Arg{Name: "text", Type: primitive.String, Help: "Some text."}
)

// recorder remembers the rendered arguments the stubs received.
type recorder struct {
	mu   sync.Mutex
	args []primitive.Args
}

func (r *recorder) add(a primitive.Args) { r.mu.Lock(); r.args = append(r.args, a); r.mu.Unlock() }

func registerStubs(t *testing.T, w *world, rec *recorder) *primitive.Registry {
	t.Helper()
	reg := primitive.NewRegistry()
	for _, p := range []primitive.Primitive{
		stubPrim{spec(("t.set"), keyArg, valueArg), func(_ context.Context, _ primitive.Env, a primitive.Args) (primitive.Result, error) {
			rec.add(a)
			w.set(a.String("key"), a.Bool("value"))
			return primitive.Result{Status: primitive.Pass, Message: "set " + a.String("key")}, nil
		}},
		stubPrim{spec("t.expect", keyArg, valueArg), func(_ context.Context, _ primitive.Env, a primitive.Args) (primitive.Result, error) {
			rec.add(a)
			if got := w.get(a.String("key")); got != a.Bool("value") {
				return primitive.Result{Status: primitive.Fail, Message: fmt.Sprintf("%s is %v", a.String("key"), got), Evidence: "token " + rootToken + " was used"}, nil
			}
			return primitive.Result{Status: primitive.Pass, Message: a.String("key") + " as expected"}, nil
		}},
		stubPrim{spec("t.error"), func(context.Context, primitive.Env, primitive.Args) (primitive.Result, error) {
			return primitive.Result{}, errors.New("the container is gone")
		}},
		stubPrim{spec("t.fail"), func(context.Context, primitive.Env, primitive.Args) (primitive.Result, error) {
			return primitive.Result{Status: primitive.Fail, Message: "it did not work", Evidence: "output with " + rootToken}, nil
		}},
		stubPrim{spec("t.block"), func(ctx context.Context, _ primitive.Env, _ primitive.Args) (primitive.Result, error) {
			<-ctx.Done()
			return primitive.Result{}, ctx.Err()
		}},
		stubPrim{spec("t.echo", textArg), func(_ context.Context, _ primitive.Env, a primitive.Args) (primitive.Result, error) {
			rec.add(a)
			return primitive.Result{Status: primitive.Pass, Message: "echoed", Outputs: map[string]string{"echoed": a.String("text")}}, nil
		}},
	} {
		if err := reg.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	return reg
}

// scenario -------------------------------------------------------------------

const testScenario = `id: test-scenario
name: Test Scenario
summary: A scenario for the engine tests.
product: vault
editions: [community, enterprise]
difficulty: beginner
estimatedMinutes: 10
tags: [test]
objectives: [Test]
prerequisites: [None]
parameters:
  - id: version
    type: version
    product: vault
    label: Version
  - id: nodes
    type: int
    label: Node count
    default: 2
    min: 1
    max: 5
topology:
  clusters:
    - name: primary
      product: vault
      nodes: 2
initialState: Healthy.
failureCreated: Broken.
successCriteria: Fixed.
pitfalls: [None]
references:
  - title: Docs
    url: https://example.com
steps:
  - id: break
    kind: breakage
    title: Break it
    guide: steps/break.md
    action:
      - primitive: t.set
        args: { key: broken }
    verify:
      - primitive: t.expect
        args: { key: broken }
  - id: fix
    kind: verification
    title: Fix it
    guide: steps/fix.md
    resetPoint: true
    verify:
      - primitive: t.expect
        args: { key: fixed }
  - id: break-again
    kind: breakage
    title: Break it again
    guide: steps/break2.md
    action:
      - primitive: t.set
        args: { key: broken2 }
    verify:
      - primitive: t.expect
        args: { key: broken2 }
  - id: fix-again
    kind: verification
    title: Fix it again
    guide: steps/fix2.md
    verify:
      - primitive: t.expect
        args: { key: fixed2 }
`

const sandboxScenario = `id: sandbox
name: Sandbox
summary: Free-form.
product: vault
editions: [community]
difficulty: beginner
estimatedMinutes: 60
tags: [test]
objectives: [Explore]
prerequisites: [None]
parameters:
  - id: version
    type: version
    product: vault
    label: Version
topology:
  clusters:
    - name: primary
      product: vault
      nodes: 1
initialState: Running.
references:
  - title: Docs
    url: https://example.com
`

func loadScenario(t *testing.T, yaml string) *scenario.Scenario {
	t.Helper()
	fsys := fstest.MapFS{"s/scenario.yaml": {Data: []byte(yaml)}}
	for _, f := range []string{"break", "fix", "break2", "fix2"} {
		fsys["s/steps/"+f+".md"] = &fstest.MapFile{Data: []byte("# " + f + "\n")}
	}
	sc, err := scenario.Load(fsys, "s")
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

// rig bundles an engine with everything a test needs to drive and inspect it.
type rig struct {
	t      *testing.T
	e      *Engine
	rt     *fake.Runtime
	dep    *fakeDeployer
	world  *world
	rec    *recorder
	sc     *scenario.Scenario
	clock  *fakeClock
	mu     sync.Mutex
	events []Event
	// reports collects what the engine saves when a run ends.
	reports *memReports
}

// memReports is a ReportSaver that remembers what it was given.
type memReports struct {
	mu    sync.Mutex
	saved []*report.Report
	fail  error
}

func (m *memReports) Save(r *report.Report) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	m.saved = append(m.saved, r)
	return nil
}

func (m *memReports) all() []*report.Report {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*report.Report(nil), m.saved...)
}

// fakeClock advances only when a test says so, so timing is exact.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newRig(t *testing.T, scenarioYAML string) *rig {
	t.Helper()
	w := &world{flags: map[string]bool{}}
	rt := fake.New()
	rec := &recorder{}
	clock := &fakeClock{t: time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)}
	r := &rig{t: t, rt: rt, world: w, rec: rec, clock: clock, sc: loadScenario(t, scenarioYAML)}
	r.dep = &fakeDeployer{rt: rt, world: w}
	r.reports = &memReports{}

	nextID := 0
	r.e = New(Deps{
		Registry: registerStubs(t, w, rec),
		Deployer: r.dep,
		Runtime:  rt,
		Reports:  r.reports,
		Clock:    clock.Now,
		NewRunID: func() string { nextID++; return fmt.Sprintf("run%d", nextID) },
		OnEvent:  func(ev Event) { r.mu.Lock(); r.events = append(r.events, ev); r.mu.Unlock() },
	})
	t.Cleanup(func() { _, _ = r.e.End(context.Background()) })
	return r
}

var ctx = context.Background()

func (r *rig) start() {
	r.t.Helper()
	if err := r.e.Start(ctx, r.sc, StartInput{Instructor: "Ada", Trainee: "Grace", Params: map[string]any{"version": "1.21.0"}}); err != nil {
		r.t.Fatalf("Start: %v", err)
	}
}

func (r *rig) step(id string) StepView {
	r.t.Helper()
	for _, s := range r.e.Snapshot().Steps {
		if s.ID == id {
			return s
		}
	}
	r.t.Fatalf("no step %q", id)
	return StepView{}
}

func (r *rig) ok(err error) {
	r.t.Helper()
	if err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) kinds() []EventKind {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]EventKind, len(r.events))
	for i, e := range r.events {
		out[i] = e.Kind
	}
	return out
}

func (r *rig) containers() int {
	r.t.Helper()
	cs, err := r.rt.List(ctx, "run1")
	if err != nil {
		r.t.Fatal(err)
	}
	return len(cs)
}
