package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pixambi/sev1situations/internal/runtime"
	"github.com/pixambi/sev1situations/internal/scenario"
)

func (r *rig) node(name string) NodeView {
	r.t.Helper()
	for _, n := range r.e.Snapshot().Nodes {
		if n.Name == name {
			return n
		}
	}
	r.t.Fatalf("no node %q in the snapshot", name)
	return NodeView{}
}

func actionsOf(n NodeView) string {
	out := make([]string, len(n.Actions))
	for i, a := range n.Actions {
		out[i] = string(a)
	}
	return strings.Join(out, ",")
}

func TestNodesAreListedWithTheActionsThatFitTheirState(t *testing.T) {
	r := newRig(t, testScenario)
	r.start()

	snap := r.e.Snapshot()
	if len(snap.Nodes) != 2 || snap.Nodes[0].Name != "node-1" || snap.Nodes[1].Name != "node-2" {
		t.Fatalf("nodes = %+v, want node-1 and node-2 in name order", snap.Nodes)
	}
	n := snap.Nodes[0]
	if n.State != NodeRunning || n.Cluster != "primary" || n.IP == "" || actionsOf(n) != "stop,remove" {
		t.Errorf("node-1 = %+v, want a running node offering stop and remove", n)
	}
}

func TestStopStartRemoveAndRevive(t *testing.T) {
	r := newRig(t, testScenario)
	r.start()

	r.ok(r.e.NodeAction(ctx, "node-2", ActionStop))
	if n := r.node("node-2"); n.State != NodeStopped || actionsOf(n) != "start,remove" {
		t.Errorf("after stop, node-2 = %+v", n)
	}
	if r.node("node-1").State != NodeRunning {
		t.Error("stopping one node changed another")
	}

	r.ok(r.e.NodeAction(ctx, "node-2", ActionStart))
	if n := r.node("node-2"); n.State != NodeRunning || actionsOf(n) != "stop,remove" {
		t.Errorf("after start, node-2 = %+v", n)
	}

	oldID := r.e.run.deployment.Nodes["node-2"].ContainerID
	r.ok(r.e.NodeAction(ctx, "node-2", ActionRemove))
	n := r.node("node-2")
	if n.State != NodeRemoved || n.IP != "" || n.Cluster != "primary" || actionsOf(n) != "revive" {
		t.Errorf("after remove, node-2 = %+v, want a placeholder that keeps its name and cluster and offers revive", n)
	}
	if r.containers() != 1 {
		t.Errorf("containers = %d, want the removed node's container gone", r.containers())
	}

	r.ok(r.e.NodeAction(ctx, "node-2", ActionRevive))
	n = r.node("node-2")
	if n.State != NodeRunning || n.IP != "10.89.1.99" || actionsOf(n) != "stop,remove" {
		t.Errorf("after revive, node-2 = %+v, want a running node with its new address", n)
	}
	if newID := r.e.run.deployment.Nodes["node-2"].ContainerID; newID == oldID || newID == "" {
		t.Errorf("container ID %q after revive, want a fresh container", newID)
	}
	if r.containers() != 2 || r.dep.revived != 1 {
		t.Errorf("containers=%d revives=%d", r.containers(), r.dep.revived)
	}

	var node []string
	for _, ev := range r.e.Snapshot().Events {
		if ev.Kind == EventNodeAction {
			node = append(node, ev.Message)
		}
	}
	if strings.Join(node, "|") != "stop node-2|start node-2|remove node-2|revive node-2" {
		t.Errorf("node events = %v", node)
	}
}

func TestOnlyWhatTheProductOffersIsAllowed(t *testing.T) {
	r := newRig(t, testScenario)
	r.dep.actions = map[string][]NodeAction{"node-1": {ActionStop, ActionStart}} // node-2 offers nothing
	r.start()

	if n := r.node("node-2"); len(n.Actions) != 0 {
		t.Errorf("node-2 offers %v, want nothing because its product declared nothing", n.Actions)
	}
	if actionsOf(r.node("node-1")) != "stop" {
		t.Errorf("node-1 offers %q, want only stop (remove was not declared)", actionsOf(r.node("node-1")))
	}

	for _, tt := range []struct {
		node   string
		action NodeAction
	}{
		{"node-2", ActionStop},   // not declared for the node
		{"node-1", ActionRemove}, // declared for no node
		{"node-1", ActionRevive}, // not declared, and the node is not removed
	} {
		err := r.e.NodeAction(ctx, tt.node, tt.action)
		if !errors.Is(err, ErrWrongState) {
			t.Errorf("%s %s = %v, want ErrWrongState", tt.action, tt.node, err)
		}
	}
	if r.containers() != 2 {
		t.Error("a refused action still changed a container")
	}
}

func TestActionsThatDoNotFitTheStateAreRefused(t *testing.T) {
	r := newRig(t, testScenario)
	r.start()

	for _, tt := range []struct {
		action NodeAction
		why    string
	}{
		{ActionStart, "the node is running"},
		{ActionRevive, "the node is running"},
	} {
		if err := r.e.NodeAction(ctx, "node-1", tt.action); !errors.Is(err, ErrWrongState) || !strings.Contains(err.Error(), tt.why) {
			t.Errorf("%s on a running node = %v, want ErrWrongState %q", tt.action, err, tt.why)
		}
	}

	r.ok(r.e.NodeAction(ctx, "node-1", ActionStop))
	if err := r.e.NodeAction(ctx, "node-1", ActionStop); !errors.Is(err, ErrWrongState) {
		t.Errorf("stop on a stopped node = %v, want ErrWrongState", err)
	}
	r.ok(r.e.NodeAction(ctx, "node-1", ActionRemove)) // a stopped node can still be removed
	if err := r.e.NodeAction(ctx, "node-1", ActionStop); !errors.Is(err, ErrWrongState) || !strings.Contains(err.Error(), "removed") {
		t.Errorf("stop on a removed node = %v", err)
	}

	if err := r.e.NodeAction(ctx, "node-9", ActionStop); err == nil || !strings.Contains(err.Error(), "no node") {
		t.Errorf("an unknown node = %v", err)
	}
}

func TestReviveNeedsADeployerThatCanDoIt(t *testing.T) {
	r := newRig(t, testScenario)
	r.start()
	r.ok(r.e.NodeAction(ctx, "node-2", ActionRemove))

	r.e.deps.Deployer = deployOnly{r.dep} // hides Revive
	if err := r.e.NodeAction(ctx, "node-2", ActionRevive); err == nil || !strings.Contains(err.Error(), "cannot recreate") {
		t.Errorf("revive without a Reviver = %v", err)
	}
	if r.node("node-2").State != NodeRemoved {
		t.Error("a refused revive changed the node")
	}
}

// deployOnly wraps a deployer without exposing its Revive method.
type deployOnly struct{ d *fakeDeployer }

func (w deployOnly) Deploy(c context.Context, run runtime.Run, sc *scenario.Scenario, p map[string]any, progress func(string)) (Deployment, error) {
	return w.d.Deploy(c, run, sc, p, progress)
}

func TestAFailedReviveLeavesTheNodeRemoved(t *testing.T) {
	r := newRig(t, testScenario)
	r.start()
	r.ok(r.e.NodeAction(ctx, "node-2", ActionRemove))
	r.dep.reviveErr = errors.New("could not rejoin the cluster")

	err := r.e.NodeAction(ctx, "node-2", ActionRevive)
	if err == nil || !strings.Contains(err.Error(), "could not rejoin") {
		t.Fatalf("revive = %v", err)
	}
	if r.node("node-2").State != NodeRemoved {
		t.Error("a failed revive changed the node")
	}
	found := false
	for _, ev := range r.e.Snapshot().Events {
		found = found || ev.Kind == EventNodeAction && strings.Contains(ev.Message, "failed")
	}
	if !found {
		t.Error("the failed action was not recorded")
	}
}

func TestNodeStateFollowsWhatTheStepsDoToTheContainers(t *testing.T) {
	r := newRig(t, testScenario)
	r.start()

	// A scenario's own primitives (container.stop, container.remove) change
	// containers behind the engine's back. The engine asks the runtime after
	// each operation, so the Cluster tab does not drift.
	r.ok(r.rt.Stop(ctx, r.e.run.deployment.Nodes["node-1"].ContainerID))
	r.ok(r.rt.Remove(ctx, r.e.run.deployment.Nodes["node-2"].ContainerID))
	r.ok(r.e.Apply(ctx))

	if r.node("node-1").State != NodeStopped || r.node("node-2").State != NodeRemoved {
		t.Errorf("nodes = %+v, want what the runtime really has", r.e.Snapshot().Nodes)
	}
	if actionsOf(r.node("node-2")) != "revive" {
		t.Errorf("node-2 offers %q", actionsOf(r.node("node-2")))
	}
}

func TestNodeActionsNeedAnActiveRun(t *testing.T) {
	r := newRig(t, testScenario)
	if err := r.e.NodeAction(ctx, "node-1", ActionStop); !errors.Is(err, ErrNoRun) {
		t.Errorf("with no run = %v, want ErrNoRun", err)
	}

	r.dep.fail = []error{errors.New("boom")}
	_ = r.e.Start(ctx, r.sc, StartInput{Instructor: "A", Trainee: "G", Params: map[string]any{"version": "1"}})
	if err := r.e.NodeAction(ctx, "node-1", ActionStop); !errors.Is(err, ErrWrongState) {
		t.Errorf("on a failed deployment = %v, want ErrWrongState", err)
	}
	if n := r.e.Snapshot().Nodes; len(n) != 0 {
		t.Errorf("a failed deployment lists nodes %+v", n)
	}
}

func TestRollbackBringsEveryNodeBack(t *testing.T) {
	r := newRig(t, testScenario)
	reachBreakAgain(r)
	r.ok(r.e.NodeAction(ctx, "node-2", ActionRemove))
	r.ok(r.e.NodeAction(ctx, "node-1", ActionStop))

	r.ok(r.e.Rollback(ctx, "fix"))

	for _, n := range r.e.Snapshot().Nodes {
		if n.State != NodeRunning {
			t.Errorf("after the redeploy %s is %s, want every node running again", n.Name, n.State)
		}
	}
}

func TestNodeActionsAreClosedWhileAnOperationRuns(t *testing.T) {
	yaml := strings.Replace(testScenario, "primitive: t.set\n        args: { key: broken }", "primitive: t.block", 1)
	r := newRig(t, yaml)
	r.start()

	go func() { _ = r.e.Apply(ctx) }()
	waitFor(t, "the step to start applying", func() bool { return contains(r.kinds(), EventStepApplying) })

	if n := r.node("node-1"); len(n.Actions) != 0 {
		t.Errorf("node-1 offers %v during an operation, want nothing", n.Actions)
	}
	if err := r.e.NodeAction(ctx, "node-1", ActionStop); !errors.Is(err, ErrBusy) {
		t.Errorf("a node action during an operation = %v, want ErrBusy", err)
	}
}
