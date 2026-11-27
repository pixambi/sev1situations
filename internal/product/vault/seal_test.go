package vault

import (
	"context"
	"strings"
	"testing"

	"github.com/pixambi/sev1situations/internal/product"
)

var transit = &product.SealSpec{Type: "transit", Transit: product.TransitSeal{
	Address: "http://10.89.1.9:8200", Token: simSealToken, MountPath: "transit/", KeyName: "autounseal",
}}

func TestATransitSealIsWrittenIntoEveryNodesConfiguration(t *testing.T) {
	with := nodeConfig{NodeID: "node-1", IP: "10.89.1.2", Seal: transit}.render()
	for _, want := range []string{
		`seal "transit" {`, `address    = "http://10.89.1.9:8200"`, `token      = "` + simSealToken + `"`,
		`key_name   = "autounseal"`, `mount_path = "transit/"`,
	} {
		if !strings.Contains(with, want) {
			t.Errorf("the configuration lacks %q:\n%s", want, with)
		}
	}
	if strings.Contains(nodeConfig{NodeID: "node-1", IP: "10.89.1.2"}.render(), "seal") {
		t.Error("a node with no seal setting has a seal stanza")
	}
	// The stanza comes before the storage, as in Vault's documentation, and the file stays one block per concern.
	if strings.Index(with, `seal "transit"`) > strings.Index(with, `storage "raft"`) {
		t.Error("the seal stanza is after the storage stanza")
	}
}

func TestParseInitReadsRecoveryKeysForAnAutoUnsealedCluster(t *testing.T) {
	out := `{"recovery_keys_b64":["a","b","c","d","e"],"root_token":"hvs.x"}`
	if r, err := parseInit(out, true); err != nil || len(r.RecoveryKeys) != 5 || r.RootToken != "hvs.x" {
		t.Errorf("auto: %+v, %v", r, err)
	}
	// Recovery keys are not unseal keys, and unseal keys are not recovery keys.
	if _, err := parseInit(out, false); err == nil {
		t.Error("recovery keys were accepted where unseal keys are needed")
	}
	if _, err := parseInit(`{"unseal_keys_b64":["a","b","c"],"root_token":"hvs.x"}`, true); err == nil {
		t.Error("unseal keys were accepted where recovery keys are needed")
	}
}

func TestAClusterWithATransitSealIsInitialisedWithRecoveryKeysAndUnsealsItself(t *testing.T) {
	tb := newTestbed(t, 3)
	tb.spec.Seal = transit
	d := tb.mustDeploy()

	init := tb.sim.count(isCmd("node-1", "operator", "init", "-recovery-shares=5", "-recovery-threshold=3"))
	if init != 1 || tb.sim.count(isCmd("", "operator", "init", "-key-shares=5")) != 0 {
		t.Errorf("init with recovery flags ran %d times, and the key-share form ran %d times", init, tb.sim.count(isCmd("", "operator", "init", "-key-shares=5")))
	}
	if n := tb.sim.count(isCmd("", "operator", "unseal")); n != 0 {
		t.Errorf("Vault was asked to unseal %d times, but it unseals itself through the seal", n)
	}
	for _, n := range d.Nodes {
		if st := tb.sim.state(n.Name); st.sealed || !st.initialized {
			t.Errorf("%s = %+v, want it unsealed by itself", n.Name, st)
		}
	}

	for i := 1; i <= 5; i++ {
		if d.Creds["recoveryKey"+string(rune('0'+i))] == "" {
			t.Errorf("recovery key %d was not kept", i)
		}
	}
	if d.Creds["unsealKey1"] != "" {
		t.Error("an unseal key was kept for a cluster that has none")
	}
	if d.Creds["sealToken"] != simSealToken || d.State["seal"] != "transit" || d.State["sealAddress"] != transit.Transit.Address || d.State["recoveryThreshold"] != "3" {
		t.Errorf("creds = %v, state = %v", d.Creds, d.State)
	}
	if !tb.said("unseal itself through http://10.89.1.9:8200") {
		t.Error("the progress did not say the nodes unseal themselves")
	}

	// The seal is in the configuration each node was started with.
	for _, n := range d.Nodes {
		f, ok := tb.rt.File(n.ContainerID, configPath)
		if !ok || !strings.Contains(string(f.Content), `token      = "`+simSealToken+`"`) {
			t.Errorf("the configuration of %s has no seal token", n.Name)
		}
	}
}

func TestTheClusterTabShowsTheSealAndTheRecoveryKeysAsSecrets(t *testing.T) {
	tb := newTestbed(t, 1)
	tb.spec.Seal = transit
	d := tb.mustDeploy()

	view := tb.p.Describe(context.Background(), tb.rt, d)
	facts := map[string]product.Fact{}
	for _, f := range view.Sections[0].Facts {
		facts[f.Label] = f
	}
	if f := facts["Seal"]; !strings.Contains(f.Value, "transit through http://10.89.1.9:8200") || f.Secret {
		t.Errorf("Seal = %+v", f)
	}
	for _, label := range []string{"Seal token", "Recovery key 1", "Recovery key 5", "Root token"} {
		if f, ok := facts[label]; !ok || !f.Secret {
			t.Errorf("%s = %+v, want a secret fact", label, f)
		}
	}
	if f := facts["Recovery threshold"]; f.Value != "3 of 5" {
		t.Errorf("Recovery threshold = %+v", f)
	}
	if _, ok := facts["Unseal key 1"]; ok {
		t.Error("the tab shows an unseal key for a cluster that has none")
	}

	// An ordinary cluster shows what it always did, and nothing about a seal.
	plain := newTestbed(t, 1)
	pd := plain.mustDeploy()
	for _, f := range plain.p.Describe(context.Background(), plain.rt, pd).Sections[0].Facts {
		if strings.HasPrefix(f.Label, "Seal") || strings.HasPrefix(f.Label, "Recovery") {
			t.Errorf("an ordinary cluster shows %q", f.Label)
		}
	}
}

func TestAReviveInATransitClusterWaitsForTheSealInsteadOfUnsealing(t *testing.T) {
	tb := newTestbed(t, 3)
	tb.spec.Seal = transit
	d := tb.mustDeploy()
	tb.removeNode(d, "node-3")

	if _, err := tb.p.Revive(context.Background(), tb.rt, tb.spec, d, "node-3"); err != nil {
		t.Fatalf("Revive: %v", err)
	}
	if n := tb.sim.count(isCmd("", "operator", "unseal")); n != 0 {
		t.Errorf("Vault was asked to unseal %d times", n)
	}
	if st := tb.sim.state("node-3"); st.sealed {
		t.Errorf("node-3 = %+v, want it open", st)
	}
}

func provider(t *testing.T) (*testbed, product.Deployment) {
	t.Helper()
	tb := newTestbed(t, 1)
	return tb, tb.mustDeploy()
}

func TestProvideSealPreparesTheTransitEngineAndReturnsHowToReachIt(t *testing.T) {
	tb, d := provider(t)
	spec, err := tb.p.ProvideSeal(context.Background(), tb.rt, d)
	if err != nil {
		t.Fatal(err)
	}

	want := product.SealSpec{Type: "transit", Transit: product.TransitSeal{
		Address: "http://" + d.Nodes[0].IP + ":8200", Token: simSealToken, MountPath: "transit/", KeyName: "autounseal",
	}}
	if spec != want {
		t.Errorf("seal = %+v, want %+v", spec, want)
	}

	// In order, on the provider's first node, with its root token.
	var ran []string
	tb.sim.mu.Lock()
	for _, c := range tb.sim.calls {
		if len(c.argv) > 1 && c.argv[1] != "status" && c.argv[1] != "operator" && c.argv[1] != "audit" {
			ran = append(ran, strings.Join(c.argv[1:], " "))
			if c.env["VAULT_TOKEN"] != simToken || c.node != "node-1" {
				t.Errorf("%q ran on %s with the token %q", c.argv[1:], c.node, c.env["VAULT_TOKEN"])
			}
		}
	}
	tb.sim.mu.Unlock()
	if got := strings.Join(ran, " | "); got != "secrets enable -path=transit transit | write -f transit/keys/autounseal | policy write autounseal /tmp/autounseal.hcl | token create -orphan -policy=autounseal -period=720h -format=json" {
		t.Errorf("commands = %s", got)
	}
}

func TestTheSealTokenCanOnlyUseTheOneKey(t *testing.T) {
	tb, d := provider(t)
	if _, err := tb.p.ProvideSeal(context.Background(), tb.rt, d); err != nil {
		t.Fatal(err)
	}
	f, ok := tb.rt.File(d.Nodes[0].ContainerID, policyPath)
	if !ok {
		t.Fatal("no policy was written")
	}
	policy := string(f.Content)
	if strings.Count(policy, "path ") != 2 || !strings.Contains(policy, `path "transit/encrypt/autounseal"`) || !strings.Contains(policy, `path "transit/decrypt/autounseal"`) {
		t.Errorf("the policy allows more or less than encrypt and decrypt on the key:\n%s", policy)
	}
	for _, broad := range []string{"*", "sudo", "delete", "list", `"read"`, "sys/"} {
		if strings.Contains(policy, broad) {
			t.Errorf("the policy contains %q", broad)
		}
	}
}

func TestProvideSealSaysWhichStepFailedWithoutShowingAToken(t *testing.T) {
	tb, d := provider(t)
	d.Creds["rootToken"] = "hvs.WrongRootToken" // every command is refused
	_, err := tb.p.ProvideSeal(context.Background(), tb.rt, d)
	if err == nil || !strings.Contains(err.Error(), "enable the transit engine on node-1") {
		t.Errorf("err = %v", err)
	}
	if err != nil && (strings.Contains(err.Error(), "WrongRootToken") || strings.Contains(err.Error(), simToken)) {
		t.Errorf("the error shows a token: %v", err)
	}

	if _, err := tb.p.ProvideSeal(context.Background(), tb.rt, product.Deployment{Cluster: "kms"}); err == nil {
		t.Error("a deployment with no node was accepted")
	}
}

// What the token command printed is a credential, so a bad answer must not be quoted.
func TestAnUnreadableTokenAnswerIsNotEchoed(t *testing.T) {
	tb, d := provider(t)
	tb.sim.mu.Lock()
	tb.sim.badToken = true
	tb.sim.mu.Unlock()
	_, err := tb.p.ProvideSeal(context.Background(), tb.rt, d)
	if err == nil || !strings.Contains(err.Error(), "create the seal token") || strings.Contains(err.Error(), "SECRET-TOKEN-OUTPUT") {
		t.Errorf("err = %v", err)
	}
}
