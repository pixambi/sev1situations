//go:build integration

package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/engine"
)

// TestRaftLostQuorumOnRealPodman plays the scenario end to end on a real machine, as a
// maintainer must before it is merged (R-CONTRIB-4). The recovery is typed into the
// container exactly as the step's guide tells the instructor to, so this also proves that the
// guide's commands work. Needs a running Podman machine, a local alpine:3 image, and a cached
// Vault binary (task test:integration downloads one).
func TestRaftLostQuorumOnRealPodman(t *testing.T) {
	r := playScenario(t, "vault-raft-lost-quorum", "vault", 12*time.Minute)
	eng, ctx := r.eng, r.ctx

	r.start()
	snap := eng.Snapshot()
	if len(snap.Nodes) != 3 || snap.ActiveStep != 0 {
		t.Fatalf("after start: %d nodes, active step %d", len(snap.Nodes), snap.ActiveStep)
	}

	// Step 1: the breakage. Both nodes go, and the survivor loses its leader.
	r.step("apply the outage", func() error { return eng.Apply(ctx) })
	r.mustPass("outage")
	for _, n := range eng.Snapshot().Nodes {
		if (n.Name == "node-2" || n.Name == "node-3") && n.State != engine.NodeRemoved {
			t.Errorf("%s is %s, want removed", n.Name, n.State)
		}
	}
	if err := eng.Continue(ctx); err != nil {
		t.Fatal(err)
	}

	// Step 2 before the fix: the check must fail, which is what makes a pass mean something.
	r.step("verify before the fix fails", func() error { return eng.Verify(ctx) })
	if o := r.outcome("recover"); o == nil || o.Status == "pass" {
		t.Fatalf("the recovery check passed with the cluster still broken: %+v", o)
	}

	// The fix, typed as the guide says.
	var ip string
	for _, n := range eng.Snapshot().Nodes {
		if n.Name == "node-1" {
			ip = n.IP
		}
	}
	var keys []string
	for i := 1; i <= 3; i++ {
		keys = append(keys, r.secretFact("primary", "Unseal key "+string(rune('0'+i))))
	}
	env := map[string]string{"VAULT_ADDR": "http://127.0.0.1:8200"}

	r.step("1. stop Vault completely", func() error {
		r.sh("node-1", "pkill vault", env, false)
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			if r.sh("node-1", "pgrep vault", env, false).ExitCode != 0 {
				return nil
			}
			time.Sleep(time.Second)
		}
		return context.DeadlineExceeded
	})
	r.step("2. write peers.json", func() error {
		res := r.sh("node-1", `cat > /vault/data/raft/peers.json <<'JSON'
[
  { "id": "node-1", "address": "`+ip+`:8201", "non_voter": false }
]
JSON`, env, false)
		if res.ExitCode != 0 {
			t.Fatalf("writing peers.json: %s", res.Stderr)
		}
		return nil
	})
	r.step("3. start Vault again", func() error {
		r.sh("node-1", "vault server -config=/etc/vault.d/vault.hcl >> /vault/logs/operational.log 2>&1", env, true)
		return nil
	})
	r.step("4. unseal with three keys", func() error {
		deadline := time.Now().Add(60 * time.Second)
		for time.Now().Before(deadline) {
			if r.sh("node-1", "vault status", env, false).ExitCode != 1 { // 1 means it does not answer yet
				break
			}
			time.Sleep(time.Second)
		}
		for _, k := range keys {
			if res := r.sh("node-1", "vault operator unseal "+k, env, false); res.ExitCode != 0 {
				t.Fatalf("unseal: %s", res.Stderr)
			}
		}
		return nil
	})
	r.step("5. the log shows the recovery", func() error {
		deadline := time.Now().Add(60 * time.Second)
		var log string
		for time.Now().Before(deadline) {
			log = r.sh("node-1", "cat /vault/logs/operational.log", env, false).Stdout
			if strings.Contains(log, "raft recovery initiated") && strings.Contains(log, "entering leader state") {
				return nil
			}
			time.Sleep(2 * time.Second)
		}
		t.Fatalf("the log never showed the recovery. The last of it:\n%s", tail(log, 1500))
		return nil
	})
	if res := r.sh("node-1", "ls /vault/data/raft/peers.json", env, false); res.ExitCode == 0 {
		t.Error("Vault did not delete peers.json after reading it, which the guide says it does")
	}

	r.step("verify the recovery", func() error { return eng.Verify(ctx) })
	r.mustPass("recover")
	if err := eng.Continue(ctx); err != nil {
		t.Fatal(err)
	}

	// Step 3, the extension: fresh nodes from the Cluster tab.
	for _, node := range []string{"node-2", "node-3"} {
		r.step("revive "+node, func() error { return eng.NodeAction(ctx, node, engine.ActionRevive) })
	}
	r.step("verify three voters", func() error {
		deadline := time.Now().Add(4 * time.Minute)
		for time.Now().Before(deadline) {
			if err := eng.Verify(ctx); err != nil {
				return err
			}
			if o := r.outcome("restore"); o != nil && o.Status == "pass" {
				return nil
			}
			time.Sleep(5 * time.Second)
		}
		t.Fatalf("three voters never showed: %+v", r.outcome("restore"))
		return nil
	})
	if err := eng.Continue(ctx); err != nil {
		t.Fatal(err)
	}
	if s := eng.Snapshot(); s.Status != engine.StatusFinished || s.Result != "completed" {
		t.Errorf("after the last step: status %s, result %q", s.Status, s.Result)
	}
	r.finish(3)
}
