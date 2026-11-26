package vault

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/release"
	"github.com/pixambi/sev1situations/internal/runtime"
)

// removeNode removes a node's container, as the instructor's Remove action does.
func (tb *testbed) removeNode(d product.Deployment, name string) primitive.Node {
	tb.t.Helper()
	for _, n := range d.Nodes {
		if n.Name == name {
			if err := tb.rt.Remove(context.Background(), n.ContainerID); err != nil {
				tb.t.Fatal(err)
			}
			return n
		}
	}
	tb.t.Fatalf("no node %q", name)
	return primitive.Node{}
}

func TestReviveRecreatesARemovedNodeAndRejoinsIt(t *testing.T) {
	tb := newTestbed(t, 3)
	tb.spec.Ports = runtime.NewPortAllocator()
	d := tb.mustDeploy()
	old := tb.removeNode(d, "node-3")
	joinedAtDeploy := tb.sim.count(isCmd("node-3", "operator", "raft", "join"))

	fresh, err := tb.p.Revive(context.Background(), tb.rt, tb.spec, d, "node-3")
	if err != nil {
		t.Fatalf("Revive: %v", err)
	}

	if fresh.Name != "node-3" || fresh.Cluster != "primary" || fresh.ContainerID == "" || fresh.ContainerID == old.ContainerID || fresh.IP == "" {
		t.Errorf("revived node = %+v (was %+v), want a new container with the same name", fresh, old)
	}
	st := tb.sim.state("node-3")
	if !st.started || !st.initialized || st.sealed {
		t.Errorf("node-3 = %+v, want it started, back in the cluster and unsealed", st)
	}
	if tb.sim.voterCount() != 3 {
		t.Errorf("voters = %d, want the replacement to take the old node's place and not add a fourth", tb.sim.voterCount())
	}

	// The old membership is dropped before the new node joins, both with the root token.
	removed := tb.sim.count(isCmd("", "operator", "raft", "remove-peer", "node-3"))
	joined := tb.sim.count(isCmd("node-3", "operator", "raft", "join")) - joinedAtDeploy
	if removed != 1 || joined != 1 {
		t.Errorf("remove-peer ran %d times and join %d times, want once each", removed, joined)
	}
	tb.sim.mu.Lock()
	for _, c := range tb.sim.calls {
		if len(c.argv) > 3 && c.argv[3] == "remove-peer" && c.env["VAULT_TOKEN"] != simToken {
			t.Error("remove-peer did not carry the root token")
		}
	}
	tb.sim.mu.Unlock()

	// It is configured with its own new address and the same raft node ID, with the binary installed.
	cfg, ok := tb.rt.File(fresh.ContainerID, configPath)
	if !ok || !strings.Contains(string(cfg.Content), `node_id = "node-3"`) || !strings.Contains(string(cfg.Content), `api_addr = "http://`+fresh.IP+`:8200"`) {
		t.Errorf("config = %s", cfg.Content)
	}
	if bin, ok := tb.rt.File(fresh.ContainerID, binaryPath); !ok || bin.Mode != 0o755 {
		t.Errorf("binary = %+v, %v", bin, ok)
	}

	// The Cluster tab's address stays valid: the same host port is published again.
	cs, _ := tb.rt.List(context.Background(), "r1")
	for _, c := range cs {
		if c.ID == fresh.ContainerID && (len(c.Ports) != 1 || c.Ports[0].Host != d.Published[2].Host) {
			t.Errorf("revived ports = %+v, want the original host port %d", c.Ports, d.Published[2].Host)
		}
	}
	if !tb.said("reviving node-3") || !tb.said("node-3 is back in the cluster") {
		t.Error("progress did not say what was happening")
	}
}

func TestRevivedNodeIsUnsealedWithTheStoredKeys(t *testing.T) {
	tb := newTestbed(t, 2)
	d := tb.mustDeploy()
	tb.removeNode(d, "node-2")
	atDeploy := tb.sim.count(isCmd("node-2", "operator", "unseal"))

	if _, err := tb.p.Revive(context.Background(), tb.rt, tb.spec, d, "node-2"); err != nil {
		t.Fatal(err)
	}
	if got := tb.sim.count(isCmd("node-2", "operator", "unseal")) - atDeploy; got != 3 {
		t.Errorf("the revived node saw %d unseal commands, want the threshold (3)", got)
	}
}

func TestReviveRefusesWhatItCannotDo(t *testing.T) {
	t.Run("a node that still exists", func(t *testing.T) {
		tb := newTestbed(t, 3)
		d := tb.mustDeploy()
		_, err := tb.p.Revive(context.Background(), tb.rt, tb.spec, d, "node-2")
		if err == nil || !strings.Contains(err.Error(), "still exists. Start it instead") {
			t.Errorf("Revive = %v", err)
		}
		if tb.containers() != 3 {
			t.Error("a refused revive changed the containers")
		}
	})
	t.Run("a node that is not in the cluster", func(t *testing.T) {
		tb := newTestbed(t, 3)
		d := tb.mustDeploy()
		if _, err := tb.p.Revive(context.Background(), tb.rt, tb.spec, d, "node-9"); err == nil || !strings.Contains(err.Error(), `no node "node-9"`) {
			t.Errorf("Revive = %v", err)
		}
	})
	t.Run("the only node of a cluster", func(t *testing.T) {
		tb := newTestbed(t, 1)
		d := tb.mustDeploy()
		tb.removeNode(d, "node-1")
		if _, err := tb.p.Revive(context.Background(), tb.rt, tb.spec, d, "node-1"); err == nil || !strings.Contains(err.Error(), "no other node to join") {
			t.Errorf("Revive = %v, want it to say a cluster of one has nothing to rejoin", err)
		}
	})
}

func TestReviveThatCannotJoinSaysWhy(t *testing.T) {
	tb := newTestbed(t, 2)
	d := tb.mustDeploy()
	tb.removeNode(d, "node-2")
	tb.sim.joinFailures = 1_000_000

	fresh, err := tb.p.Revive(context.Background(), tb.rt, tb.spec, d, "node-2")
	if err == nil || !strings.Contains(err.Error(), "node-2 did not join the cluster within") || !strings.Contains(err.Error(), "Last seen: leader not ready") {
		t.Errorf("Revive = %v", err)
	}
	if fresh.ContainerID == "" {
		t.Error("the new container is not returned, so it cannot be cleaned up or inspected")
	}
}

func TestEnterpriseReviveCarriesTheLicense(t *testing.T) {
	tb := newTestbed(t, 2)
	tb.spec.Edition = release.Enterprise
	tb.spec.Version = release.Version{Product: "vault", Core: "2.2.2", Full: "2.2.2+ent"}
	tb.spec.Settings = settingsStub{value: "THE-LICENSE", ok: true}
	d := tb.mustDeploy()
	tb.removeNode(d, "node-2")

	fresh, err := tb.p.Revive(context.Background(), tb.rt, tb.spec, d, "node-2")
	if err != nil {
		t.Fatal(err)
	}
	lic, ok := tb.rt.File(fresh.ContainerID, licensePath)
	if !ok || lic.Mode != 0o600 || string(lic.Content) != "THE-LICENSE" {
		t.Errorf("license on the revived node = %+v, %v", lic, ok)
	}

	// Without a saved license the revive fails before creating anything.
	tb.removeNode(d, "node-1") // keep a second node to revive
	tb.spec.Settings = settingsStub{}
	before := tb.containers()
	if _, err := tb.p.Revive(context.Background(), tb.rt, tb.spec, d, "node-1"); !errors.Is(err, ErrNoLicense) {
		t.Errorf("Revive = %v, want ErrNoLicense", err)
	}
	if tb.containers() != before {
		t.Error("a refused revive created a container")
	}
}

func TestEveryNodeOffersAllFourActions(t *testing.T) {
	got := New(Options{}).NodeActions(product.Deployment{}, "node-1")
	want := []product.NodeAction{product.ActionStop, product.ActionStart, product.ActionRemove, product.ActionRevive}
	if len(got) != len(want) {
		t.Fatalf("actions = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("action %d = %q, want %q (R-VIEW-6)", i, got[i], want[i])
		}
	}
}

func TestReviveWaitsUntilTheNewNodeIsPromotedToVoter(t *testing.T) {
	tb := newTestbed(t, 3)
	d := tb.mustDeploy()
	tb.removeNode(d, "node-3")
	tb.sim.promoteAfter = 4 // as autopilot does: a new node is a non-voter for a while
	before := tb.sim.count(isCmd("", "operator", "raft", "list-peers"))

	if _, err := tb.p.Revive(context.Background(), tb.rt, tb.spec, d, "node-3"); err != nil {
		t.Fatalf("Revive: %v", err)
	}
	if polls := tb.sim.count(isCmd("", "operator", "raft", "list-peers")) - before; polls < 5 {
		t.Errorf("list-peers ran %d times, want it to keep asking until the node was a voter", polls)
	}
}

func TestReviveThatNeverGetsAVoterSaysSo(t *testing.T) {
	tb := newTestbed(t, 3)
	d := tb.mustDeploy()
	tb.removeNode(d, "node-3")
	tb.sim.promoteAfter = 1_000_000

	_, err := tb.p.Revive(context.Background(), tb.rt, tb.spec, d, "node-3")
	if err == nil || !strings.Contains(err.Error(), "see node-3 become a voter") || !strings.Contains(err.Error(), "node-3 is a non-voter") {
		t.Errorf("Revive = %v, want it to say that node-3 never became a voter", err)
	}
}

// The scenario that removes two nodes has the instructor bring them back one at a time. Reviving the first must
// not wait for the second, which is still gone.
func TestReviveDoesNotWaitForOtherNodesThatAreStillGone(t *testing.T) {
	tb := newTestbed(t, 3)
	tb.spec.Ports = runtime.NewPortAllocator()
	d := tb.mustDeploy()
	tb.removeNode(d, "node-2")
	tb.removeNode(d, "node-3")
	// Recovery with peers.json left node-1 as the only member, so the removed nodes are not in the list.
	tb.sim.mu.Lock()
	tb.sim.dropVoterNamed("node-2")
	tb.sim.dropVoterNamed("node-3")
	tb.sim.mu.Unlock()
	tb.sim.promoteAfter = 2

	if _, err := tb.p.Revive(context.Background(), tb.rt, tb.spec, d, "node-2"); err != nil {
		t.Fatalf("reviving node-2 while node-3 is gone: %v", err)
	}
	if st := tb.sim.state("node-2"); !st.started || st.sealed {
		t.Errorf("node-2 = %+v, want it back and unsealed", st)
	}
}
