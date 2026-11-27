//go:build integration

package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/engine"
)

// TestBrokenKMSOnRealPodman plays the scenario end to end on a real machine (R-CONTRIB-4). The repair is
// typed into the containers as the step's guide tells the instructor to, so the guide's commands are proved.
func TestBrokenKMSOnRealPodman(t *testing.T) {
	r := playScenario(t, "vault-broken-kms", "vault", 14*time.Minute)
	eng, ctx := r.eng, r.ctx

	r.start()
	snap := eng.Snapshot()
	names := map[string]bool{}
	for _, n := range snap.Nodes {
		names[n.Name] = true
	}
	for _, want := range []string{"node-1", "node-2", "node-3", "kms-node-1"} {
		if !names[want] {
			t.Fatalf("nodes = %v, want %s", names, want)
		}
	}
	env := map[string]string{"VAULT_ADDR": "http://127.0.0.1:8200"}

	// The workload cluster unseals itself through the KMS, and has recovery keys, not unseal keys.
	status := r.sh("node-2", "vault status -format=json", env, false).Stdout
	var st struct {
		Sealed bool   `json:"sealed"`
		Type   string `json:"type"`
	}
	if err := json.Unmarshal([]byte(status), &st); err != nil || st.Sealed || st.Type != "transit" {
		t.Fatalf("node-2 status = %s (%v), want an unsealed transit seal", status, err)
	}
	r.secretFact("primary", "Recovery key 1") // shown on the Cluster tab
	t.Logf("seal token shown on the Cluster tab: %v", r.secretFact("primary", "Seal token") != "")

	// Step 1: the breakage.
	r.step("apply the breakage", func() error { return eng.Apply(ctx) })
	r.mustPass("revoke-token")
	log := r.sh("node-1", "tail -n 25 /vault/logs/operational.log", env, false).Stdout
	t.Logf("what the log of node-1 ends with:\n%s", tail(log, 1800))
	if err := eng.Continue(ctx); err != nil {
		t.Fatal(err)
	}

	// Step 2 before the fix must fail.
	r.step("verify before the fix fails", func() error { return eng.Verify(ctx) })
	if o := r.outcome("unsealed"); o == nil || o.Status == "pass" {
		t.Fatalf("the check passed with the cluster still down: %+v", o)
	}

	// The repair, as the guide gives it.
	kms := map[string]string{"VAULT_ADDR": "http://127.0.0.1:8200", "VAULT_TOKEN": r.secretFact("kms", "Root token")}
	var token string
	r.step("1. check the policy and create a token", func() error {
		pol := r.sh("kms-node-1", "vault policy read autounseal", kms, false)
		if pol.ExitCode != 0 || !strings.Contains(pol.Stdout, "transit/encrypt/autounseal") {
			t.Fatalf("the policy is not there as the guide says: %+v", pol)
		}
		res := r.sh("kms-node-1", "vault token create -orphan -policy=autounseal -period=720h -format=json", kms, false)
		var out struct {
			Auth struct {
				ClientToken string `json:"client_token"`
			} `json:"auth"`
		}
		if res.ExitCode != 0 || json.Unmarshal([]byte(res.Stdout), &out) != nil || out.Auth.ClientToken == "" {
			t.Fatalf("creating the token: %+v", res)
		}
		token = out.Auth.ClientToken
		return nil
	})
	for _, node := range []string{"node-1", "node-2", "node-3"} {
		r.step("2. new token and start Vault on "+node, func() error {
			res := r.sh(node, `sed -i 's/token .*/token      = "`+token+`"/' /etc/vault.d/vault.hcl && grep -c "`+token+`" /etc/vault.d/vault.hcl`, env, false)
			if res.ExitCode != 0 {
				t.Fatalf("editing the configuration: %+v", res)
			}
			r.sh(node, "vault server -config=/etc/vault.d/vault.hcl >> /vault/logs/operational.log 2>&1", env, true)
			return nil
		})
	}

	r.step("verify the cluster is unsealed", func() error { return eng.Verify(ctx) })
	if o := r.outcome("unsealed"); o == nil || o.Status != "pass" {
		t.Fatalf("the repair was not accepted: %+v\nlog of node-1:\n%s", o, tail(r.sh("node-1", "tail -n 20 /vault/logs/operational.log", env, false).Stdout, 1500))
	}
	if err := eng.Continue(ctx); err != nil {
		t.Fatal(err)
	}

	r.step("verify auto-unseal after a restart", func() error { return eng.Verify(ctx) })
	if o := r.outcome("proven"); o == nil || o.Status != "pass" {
		t.Fatalf("auto-unseal was not proven: %+v", o)
	}
	if err := eng.Continue(ctx); err != nil {
		t.Fatal(err)
	}
	if s := eng.Snapshot(); s.Status != engine.StatusFinished || s.Result != "completed" {
		t.Errorf("after the last step: status %s, result %q", s.Status, s.Result)
	}
	r.finish(3)
}
