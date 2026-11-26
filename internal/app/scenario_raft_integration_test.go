//go:build integration

package app

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/engine"
	"github.com/pixambi/sev1situations/internal/runtime"
)

// TestRaftLostQuorumOnRealPodman plays the scenario end to end on a real machine, as a
// maintainer must before it is merged (R-CONTRIB-4). The recovery is typed into the
// container exactly as the step's guide tells the instructor to, so this also proves that the
// guide's commands work. Needs a running Podman machine, a local alpine:3 image, and a cached
// Vault binary (task test:integration downloads one).
func TestRaftLostQuorumOnRealPodman(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	cache := filepath.Join(userCache(t), "sev1situations-integration", "vault")
	if _, err := os.Stat(cache); err != nil {
		t.Skipf("no cached Vault binary at %s: run the Vault integration tests first", cache)
	}
	data := t.TempDir()
	t.Setenv("S1S_DATA_DIR", data)
	if err := os.MkdirAll(filepath.Join(data, "cache", "binaries"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := shellOut("cp", "-r", cache, filepath.Join(data, "cache", "binaries")); err != nil {
		t.Fatalf("seed the binary cache: %v %s", err, out)
	}

	opts := Options{Builtin: os.DirFS("../.."), Stdin: strings.NewReader("")}
	conn, err := Connect(ctx, opts, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		t.Skipf("Podman is not available: %v", err)
	}
	eng := conn.Engine
	t.Cleanup(func() { _, _ = eng.End(context.Background()) })

	opts.Products = conn.Products // so the scenario's vault primitives are known when it is checked
	reg, err := LoadCatalogue(opts)
	if err != nil {
		t.Fatalf("catalogue: %v", err)
	}
	entry, ok := reg.Get("vault-raft-lost-quorum")
	if !ok {
		t.Fatal("the scenario is not in the catalogue")
	}
	versions, err := Versions(conn.Products)(ctx, "vault", "community")
	if err != nil || len(versions) == 0 {
		t.Skipf("cannot list Vault versions: %v", err)
	}

	step := func(what string, f func() error) {
		t.Helper()
		t0 := time.Now()
		if err := f(); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		t.Logf("%-34s %s", what, time.Since(t0).Round(time.Second))
	}
	lastOutcome := func(id string) *engine.Outcome {
		for _, s := range eng.Snapshot().Steps {
			if s.ID == id {
				return s.Last
			}
		}
		return nil
	}
	mustPass := func(id string) {
		t.Helper()
		o := lastOutcome(id)
		if o == nil || o.Status != "pass" {
			t.Fatalf("step %s did not pass: %+v", id, o)
		}
	}

	step("deploy three nodes", func() error {
		return eng.Start(ctx, entry.Scenario, engine.StartInput{
			Instructor: "Ada", Trainee: "Grace", Params: map[string]any{"version": versions[0], "edition": "community"},
		})
	})
	snap := eng.Snapshot()
	if len(snap.Nodes) != 3 || snap.ActiveStep != 0 {
		t.Fatalf("after start: %d nodes, active step %d", len(snap.Nodes), snap.ActiveStep)
	}

	// Step 1: the breakage. Both nodes go, and the survivor loses its leader.
	step("apply the outage", func() error { return eng.Apply(ctx) })
	mustPass("outage")
	if !eng.Snapshot().Steps[0].CanContinue {
		t.Fatalf("the outage is not verified: %+v", eng.Snapshot().Steps[0])
	}
	for _, n := range eng.Snapshot().Nodes {
		if (n.Name == "node-2" || n.Name == "node-3") && n.State != engine.NodeRemoved {
			t.Errorf("%s is %s, want removed", n.Name, n.State)
		}
	}
	if err := eng.Continue(ctx); err != nil {
		t.Fatal(err)
	}

	// Step 2 before the fix: the check must fail, which is what makes a pass mean something.
	step("verify before the fix fails", func() error { return eng.Verify(ctx) })
	if o := lastOutcome("recover"); o == nil || o.Status == "pass" {
		t.Fatalf("the recovery check passed with the cluster still broken: %+v", o)
	}

	// The fix, typed as the guide says.
	container, err := eng.ContainerOf("node-1")
	if err != nil {
		t.Fatal(err)
	}
	var ip string
	for _, n := range eng.Snapshot().Nodes {
		if n.Name == "node-1" {
			ip = n.IP
		}
	}
	keys := unsealKeysFrom(conn.Deployer.View(ctx, snap.RunID))
	if len(keys) < 3 || ip == "" {
		t.Fatalf("keys = %d, ip = %q", len(keys), ip)
	}
	env := map[string]string{"VAULT_ADDR": "http://127.0.0.1:8200"}
	sh := func(script string, detach bool) runtime.ExecResult {
		t.Helper()
		res, err := conn.Runtime.Exec(ctx, container, []string{"sh", "-c", script}, runtime.ExecOptions{Env: env, Detach: detach})
		if err != nil {
			t.Fatalf("%q: %v", script, err)
		}
		return res
	}

	step("1. stop Vault completely", func() error {
		sh("pkill vault", false)
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			if sh("pgrep vault", false).ExitCode != 0 {
				return nil
			}
			time.Sleep(time.Second)
		}
		return context.DeadlineExceeded
	})
	step("2. write peers.json", func() error {
		res := sh(`cat > /vault/data/raft/peers.json <<'JSON'
[
  { "id": "node-1", "address": "`+ip+`:8201", "non_voter": false }
]
JSON`, false)
		if res.ExitCode != 0 {
			t.Fatalf("writing peers.json: %s", res.Stderr)
		}
		return nil
	})
	step("3. start Vault again", func() error {
		sh("vault server -config=/etc/vault.d/vault.hcl >> /vault/logs/operational.log 2>&1", true)
		return nil
	})
	step("4. unseal with three keys", func() error {
		deadline := time.Now().Add(60 * time.Second)
		for time.Now().Before(deadline) {
			if sh("vault status", false).ExitCode != 1 { // 1 means it does not answer yet
				break
			}
			time.Sleep(time.Second)
		}
		for _, k := range keys[:3] {
			if res := sh("vault operator unseal "+k, false); res.ExitCode != 0 {
				t.Fatalf("unseal: %s", res.Stderr)
			}
		}
		return nil
	})
	step("5. the log shows the recovery", func() error {
		deadline := time.Now().Add(60 * time.Second)
		var log string
		for time.Now().Before(deadline) {
			log = sh("cat /vault/logs/operational.log", false).Stdout
			if strings.Contains(log, "raft recovery initiated") && strings.Contains(log, "entering leader state") {
				return nil
			}
			time.Sleep(2 * time.Second)
		}
		t.Fatalf("the log never showed the recovery. The last of it:\n%s", tail(log, 1500))
		return nil
	})
	if res := sh("ls /vault/data/raft/peers.json", false); res.ExitCode == 0 {
		t.Error("Vault did not delete peers.json after reading it, which the guide says it does")
	}

	step("verify the recovery", func() error { return eng.Verify(ctx) })
	if o := lastOutcome("recover"); o == nil || o.Status != "pass" {
		t.Fatalf("the recovery was not accepted: %+v", o)
	}
	if err := eng.Continue(ctx); err != nil {
		t.Fatal(err)
	}

	// Step 3, the extension: fresh nodes from the Cluster tab.
	for _, node := range []string{"node-2", "node-3"} {
		step("revive "+node, func() error { return eng.NodeAction(ctx, node, engine.ActionRevive) })
	}
	var last *engine.Outcome
	step("verify three voters", func() error {
		deadline := time.Now().Add(4 * time.Minute)
		for time.Now().Before(deadline) {
			if err := eng.Verify(ctx); err != nil {
				return err
			}
			if last = lastOutcome("restore"); last != nil && last.Status == "pass" {
				return nil
			}
			time.Sleep(5 * time.Second)
		}
		t.Fatalf("three voters never showed: %+v", last)
		return nil
	})
	if err := eng.Continue(ctx); err != nil {
		t.Fatal(err)
	}
	if s := eng.Snapshot(); s.Status != engine.StatusFinished || s.Result != "completed" {
		t.Errorf("after the last step: status %s, result %q", s.Status, s.Result)
	}

	final, err := eng.End(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := conn.Reports.Get(rid(final))
	if err != nil {
		t.Fatalf("no report: %v", err)
	}
	if rep.Result != "completed" || len(rep.Steps) != 3 {
		t.Errorf("report = %s with %d steps", rep.Result, len(rep.Steps))
	}
	if left, _ := conn.Runtime.Runs(ctx); len(left) != 0 {
		t.Errorf("runs left behind: %v", left)
	}
}
