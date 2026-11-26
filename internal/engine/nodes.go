package engine

import (
	"context"
	"fmt"
	"sort"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/runtime"
	"github.com/pixambi/sev1situations/internal/scenario"
)

// NodeAction is something the instructor can do to a node (R-VIEW-6).
type NodeAction string

// Node actions. Which of them a node offers is declared by its product
// (Deployment.Actions), and which apply right now depends on its state.
const (
	// ActionStop stops a running node.
	ActionStop NodeAction = "stop"
	// ActionStart starts a stopped node.
	ActionStart NodeAction = "start"
	// ActionRemove removes a node. It stays in the snapshot as a placeholder.
	ActionRemove NodeAction = "remove"
	// ActionRevive recreates a removed node, with the same configuration, and
	// joins it back to its cluster.
	ActionRevive NodeAction = "revive"
)

// NodeState is the state of a node's container.
type NodeState string

const (
	// NodeRunning means the container is running.
	NodeRunning NodeState = "running"
	// NodeStopped means the container exists but is not running.
	NodeStopped NodeState = "stopped"
	// NodeRemoved means the container is gone. The node remains as a placeholder.
	NodeRemoved NodeState = "removed"
)

// Reviver is implemented by a Deployer that can recreate a removed node. How a
// node rejoins its cluster is product knowledge, so the engine only asks.
type Reviver interface {
	// Revive recreates the named node as it was configured and returns it, with
	// its new container and address.
	Revive(ctx context.Context, run runtime.Run, sc *scenario.Scenario, params map[string]any, dep Deployment, node string) (primitive.Node, error)
}

// NodeView is one node as the Cluster tab sees it.
type NodeView struct {
	// Name is the node's name, for example node-1.
	Name string
	// Cluster is the cluster or service the node belongs to.
	Cluster string
	// IP is the node's address, empty for a removed node.
	IP string
	// State is running, stopped or removed.
	State NodeState
	// Actions are the actions that can be taken on the node right now: those
	// its product declares that fit its state. They are empty while an
	// operation is in progress.
	Actions []NodeAction
}

// applicable says which actions fit a node in a given state.
func applicable(s NodeState) []NodeAction {
	switch s {
	case NodeRunning:
		return []NodeAction{ActionStop, ActionRemove}
	case NodeStopped:
		return []NodeAction{ActionStart, ActionRemove}
	default:
		return []NodeAction{ActionRevive}
	}
}

// nodeViewsLocked lists the nodes in name order. The mutex must be held.
func (e *Engine) nodeViewsLocked(r *run, idle bool) []NodeView {
	names := make([]string, 0, len(r.deployment.Nodes))
	for n := range r.deployment.Nodes {
		names = append(names, n)
	}
	sort.Strings(names)

	views := make([]NodeView, 0, len(names))
	for _, name := range names {
		n := r.deployment.Nodes[name]
		state := r.nodeState[name]
		v := NodeView{Name: name, Cluster: n.Cluster, IP: n.IP, State: state}
		if state == NodeRemoved {
			v.IP = ""
		}
		if idle && r.status == StatusActive {
			for _, a := range applicable(state) {
				if declared(r.deployment.Actions[name], a) {
					v.Actions = append(v.Actions, a)
				}
			}
		}
		views = append(views, v)
	}
	return views
}

func declared(list []NodeAction, a NodeAction) bool {
	for _, x := range list {
		if x == a {
			return true
		}
	}
	return false
}

// NodeAction performs an action on a node. The product must declare the action
// for the node and it must fit the node's state, so the instructor can never do
// something the Cluster tab did not offer.
func (e *Engine) NodeAction(ctx context.Context, node string, action NodeAction) error {
	r, opCtx, done, err := e.begin(ctx)
	if err != nil {
		return err
	}
	defer done()

	e.mu.Lock()
	n, ok := r.deployment.Nodes[node]
	state := r.nodeState[node]
	switch {
	case r.status != StatusActive:
		e.mu.Unlock()
		return fmt.Errorf("%s %s: %w: the run is not in progress", action, node, ErrWrongState)
	case !ok:
		e.mu.Unlock()
		return fmt.Errorf("%s: there is no node %q", action, node)
	case !declared(r.deployment.Actions[node], action):
		e.mu.Unlock()
		return fmt.Errorf("%s %s: %w: its product does not offer that action for this node", action, node, ErrWrongState)
	case !declared(applicable(state), action):
		e.mu.Unlock()
		return fmt.Errorf("%s %s: %w: the node is %s", action, node, ErrWrongState, state)
	}
	dep, params := r.deployment, r.params
	e.mu.Unlock()

	rt := e.deps.Runtime
	switch action {
	case ActionStop:
		err = rt.Stop(opCtx, n.ContainerID)
	case ActionStart:
		err = rt.Start(opCtx, n.ContainerID)
	case ActionRemove:
		err = rt.Remove(opCtx, n.ContainerID)
	case ActionRevive:
		reviver, ok := e.deps.Deployer.(Reviver)
		if !ok {
			return fmt.Errorf("revive %s: this deployment cannot recreate a node", node)
		}
		var fresh primitive.Node
		fresh, err = reviver.Revive(opCtx, runtime.Run{ID: r.id, Scenario: r.sc.ID}, r.sc, params, dep, node)
		if err == nil {
			e.mu.Lock()
			r.deployment.Nodes[node] = fresh
			e.mu.Unlock()
		}
	}
	if err != nil {
		if opCtx.Err() != nil {
			return opCtx.Err()
		}
		e.emit(r, EventNodeAction, "", fmt.Sprintf("%s %s failed: %v", action, node, err))
		return fmt.Errorf("%s %s: %w", action, node, err)
	}

	e.refreshNodes(opCtx, r)
	e.emit(r, EventNodeAction, "", fmt.Sprintf("%s %s", action, node))
	return nil
}

// refreshNodes reads the real state of every node's container. A scenario's own
// steps can stop or remove containers too, so the engine asks instead of
// assuming. A container that cannot be found is a removed node. If the runtime
// cannot be asked, the states are left as they were.
func (e *Engine) refreshNodes(ctx context.Context, r *run) {
	if ctx.Err() != nil {
		return
	}
	containers, err := e.deps.Runtime.List(ctx, r.id)
	if err != nil {
		return
	}
	byID := map[string]runtime.State{}
	for _, c := range containers {
		byID[c.ID] = c.State
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if r.nodeState == nil {
		r.nodeState = map[string]NodeState{}
	}
	for name, n := range r.deployment.Nodes {
		st, found := byID[n.ContainerID]
		switch {
		case !found:
			r.nodeState[name] = NodeRemoved
		case st == runtime.StateRunning:
			r.nodeState[name] = NodeRunning
		default:
			r.nodeState[name] = NodeStopped
		}
	}
}
