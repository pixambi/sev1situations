package gui

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/pixambi/sev1situations/internal/engine"
	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/runtime"
	"github.com/pixambi/sev1situations/internal/runtime/fake"
	"github.com/pixambi/sev1situations/internal/scenario"
)

// scenarioYAML builds a small scenario. A scenario with steps has a breakage and a fix.
func scenarioYAML(id, name, product string, withSteps bool) string {
	y := fmt.Sprintf(`id: %s
name: %s
summary: Summary of %s.
product: %s
editions: [community]
difficulty: beginner
estimatedMinutes: 10
tags: [test]
objectives: [Learn it]
prerequisites: [None]
parameters:
  - id: nodes
    type: int
    label: Nodes
    default: 2
    min: 1
    max: 5
topology:
  clusters:
    - name: primary
      product: %s
      nodesFrom: nodes
initialState: Healthy.
references:
  - title: Docs
    url: https://example.com
`, id, name, name, product, product)
	if !withSteps {
		return y
	}
	return y + `failureCreated: Broken.
successCriteria: Fixed.
pitfalls: [None]
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
`
}

// world is the pretend environment the test primitives act on.
type world struct {
	mu    sync.Mutex
	flags map[string]bool
}

func (w *world) set(k string, v bool) { w.mu.Lock(); w.flags[k] = v; w.mu.Unlock() }
func (w *world) get(k string) bool    { w.mu.Lock(); defer w.mu.Unlock(); return w.flags[k] }

type testPrimitive struct {
	spec primitive.Spec
	run  func(a primitive.Args) primitive.Result
}

func (p testPrimitive) Spec() primitive.Spec { return p.spec }
func (p testPrimitive) Run(_ context.Context, _ primitive.Env, a primitive.Args) (primitive.Result, error) {
	return p.run(a), nil
}

func newPrimitives(t *testing.T, w *world) *primitive.Registry {
	t.Helper()
	key := primitive.Arg{Name: "key", Type: primitive.String, Required: true, Help: "The flag."}
	spec := func(name string) primitive.Spec {
		return primitive.Spec{Name: name, Summary: "A test primitive.", Args: []primitive.Arg{key}, Timeout: 10 * time.Second}
	}
	reg := primitive.NewRegistry()
	for _, p := range []primitive.Primitive{
		testPrimitive{spec("t.set"), func(a primitive.Args) primitive.Result {
			w.set(a.String("key"), true)
			return primitive.Result{Status: primitive.Pass, Message: "set"}
		}},
		testPrimitive{spec("t.expect"), func(a primitive.Args) primitive.Result {
			if !w.get(a.String("key")) {
				return primitive.Result{Status: primitive.Fail, Message: a.String("key") + " is not set"}
			}
			return primitive.Result{Status: primitive.Pass, Message: a.String("key") + " is set"}
		}},
	} {
		if err := reg.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	return reg
}

// deployer starts one container per node on the fake runtime, as a real one would.
type deployer struct{ rt *fake.Runtime }

func (d deployer) Deploy(ctx context.Context, run runtime.Run, sc *scenario.Scenario, params map[string]any, progress func(string)) (engine.Deployment, error) {
	progress("creating the network")
	nodes := map[string]primitive.Node{}
	for n := 1; n <= sc.Topology.Clusters[0].NodeCount(params); n++ {
		name := fmt.Sprintf("node-%d", n)
		c, err := d.rt.StartContainer(ctx, runtime.ContainerOptions{Run: run, Name: name, Image: "alpine:3"})
		if err != nil {
			return engine.Deployment{}, err
		}
		nodes[name] = primitive.Node{Name: name, Cluster: "primary", ContainerID: c.ID, IP: "10.89.1.2"}
	}
	progress("the cluster is healthy")
	return engine.Deployment{Nodes: nodes, Actions: map[string][]engine.NodeAction{}, Creds: map[string]map[string]string{}}, nil
}

// emitted records what the services publish.
type emitted struct {
	mu     sync.Mutex
	events []emittedEvent
}

type emittedEvent struct {
	Name string
	Data any
}

func (e *emitted) emit(name string, data any) {
	e.mu.Lock()
	e.events = append(e.events, emittedEvent{name, data})
	e.mu.Unlock()
}

func (e *emitted) named(name string) []any {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []any
	for _, ev := range e.events {
		if ev.Name == name {
			out = append(out, ev.Data)
		}
	}
	return out
}

// backend is everything a service test needs.
type backend struct {
	eng       *engine.Engine
	rt        *fake.Runtime
	world     *world
	catalogue Catalogue
	out       *emitted
}

// catalogueOf makes a catalogue of the given scenarios, each keyed by its folder name.
// A key starting with "bad-" is written as a broken scenario.
func catalogueOf(t *testing.T, w *world, scenarios map[string]string) Catalogue {
	t.Helper()
	fsys := fstest.MapFS{}
	for dir, yaml := range scenarios {
		fsys[dir+"/scenario.yaml"] = &fstest.MapFile{Data: []byte(yaml)}
		for _, g := range []string{"break", "fix"} {
			fsys[dir+"/steps/"+g+".md"] = &fstest.MapFile{Data: []byte("# " + g + "\n")}
		}
	}
	return func() (*scenario.Registry, error) {
		reg := scenario.NewRegistry(scenario.Options{Primitives: newPrimitives(t, w), Products: []string{"vault", "boundary"}})
		return reg, reg.Load(fsys, ".", scenario.BuiltIn)
	}
}

func newBackend(t *testing.T, scenarios map[string]string) *backend {
	t.Helper()
	w := &world{flags: map[string]bool{}}
	rt := fake.New()
	eng := engine.New(engine.Deps{
		Registry: newPrimitives(t, w), Deployer: deployer{rt: rt}, Runtime: rt,
		NewRunID: func() string { return "run1" },
	})
	t.Cleanup(func() { _, _ = eng.End(context.Background()) })
	return &backend{eng: eng, rt: rt, world: w, catalogue: catalogueOf(t, w, scenarios), out: &emitted{}}
}

func (b *backend) runService(ctx context.Context) *RunService {
	return NewRunService(ctx, func() (*engine.Engine, error) { return b.eng, nil }, b.catalogue, b.out.emit)
}
