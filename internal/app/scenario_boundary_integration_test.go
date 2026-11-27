//go:build integration

package app

import (
	"strings"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/engine"
)

// TestBoundarySandboxOnRealPodman plays the sandbox through the engine on a real machine (R-CONTRIB-4): it deploys, shows what
// the Cluster tab needs, proves a session works, takes node actions as an instructor would, and ends with nothing left.
func TestBoundarySandboxOnRealPodman(t *testing.T) {
	r := playScenario(t, "boundary-sandbox", "boundary", 15*time.Minute)
	eng, ctx := r.eng, r.ctx

	r.step("deploy", func() error {
		return eng.Start(ctx, r.entry.Scenario, engine.StartInput{
			Instructor: "Ada", Trainee: "Grace", Params: map[string]any{"version": r.newer, "edition": "community", "controllers": 2},
		})
	})
	snap := eng.Snapshot()
	names := map[string]engine.NodeState{}
	for _, n := range snap.Nodes {
		names[n.Name] = n.State
	}
	for _, want := range []string{"node-1", "node-2", "worker-node-1", "lb-node-1", "db-node-1", "target-node-1"} {
		if names[want] != engine.NodeRunning {
			t.Errorf("%s is %q, want running", want, names[want])
		}
	}
	if len(snap.Steps) != 0 || snap.Status != engine.StatusActive {
		t.Errorf("a sandbox has no steps and stays active: %d steps, %s", len(snap.Steps), snap.Status)
	}

	// The Cluster tab shows where to sign in and the credentials.
	view := r.conn.Deployer.View(ctx, snap.RunID)
	var facts = map[string]string{}
	for _, sec := range view.Sections {
		for _, f := range sec.Facts {
			facts[f.Label] = f.Value
		}
	}
	if !strings.HasPrefix(facts["Cluster URL"], "http://127.0.0.1:") || facts["Admin login name"] != "admin" || facts["Admin password"] == "" || facts["Target ID"] == "" {
		t.Errorf("the Cluster tab = %v", facts)
	}

	// Node actions, as an instructor takes them: stop and start a controller, and remove and revive the worker.
	r.step("stop node-2", func() error { return eng.NodeAction(ctx, "node-2", engine.ActionStop) })
	r.step("start node-2", func() error { return eng.NodeAction(ctx, "node-2", engine.ActionStart) })
	r.step("remove the worker", func() error { return eng.NodeAction(ctx, "worker-node-1", engine.ActionRemove) })
	r.step("revive the worker", func() error { return eng.NodeAction(ctx, "worker-node-1", engine.ActionRevive) })
	for _, n := range eng.Snapshot().Nodes {
		if n.Name == "db-node-1" {
			for _, a := range n.Actions {
				if a == engine.ActionRemove || a == engine.ActionRevive {
					t.Errorf("the database offers %s", a)
				}
			}
		}
	}

	r.finish(0)
}
