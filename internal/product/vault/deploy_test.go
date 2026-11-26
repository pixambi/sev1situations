package vault

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/product/producttest"
	"github.com/pixambi/sev1situations/internal/release"
	"github.com/pixambi/sev1situations/internal/runtime"
	rtfake "github.com/pixambi/sev1situations/internal/runtime/fake"
)

const fakeBinaryBytes = "#!/bin/sh\necho pretend vault binary\n"

var fastTiming = Timing{Poll: time.Millisecond, NodeReady: 300 * time.Millisecond, Leader: 300 * time.Millisecond, Join: 300 * time.Millisecond}

// testbed is a fake runtime with the simulated Vault behind it and a spec to deploy.
type testbed struct {
	t        *testing.T
	rt       *rtfake.Runtime
	sim      *sim
	p        *Product
	spec     product.DeploySpec
	mu       sync.Mutex
	progress []string
}

func newTestbed(t *testing.T, nodes int) *testbed {
	t.Helper()
	rt := rtfake.New()
	tb := &testbed{t: t, rt: rt, sim: newSim(rt), p: New(Options{Timing: fastTiming})}

	bin := filepath.Join(t.TempDir(), "vault")
	if err := os.WriteFile(bin, []byte(fakeBinaryBytes), 0o755); err != nil {
		t.Fatal(err)
	}
	run := runtime.Run{ID: "r1", Scenario: "test"}
	n, err := rt.CreateNetwork(context.Background(), runtime.NetworkOptions{Run: run, Name: "net"})
	if err != nil {
		t.Fatal(err)
	}
	tb.spec = product.DeploySpec{
		Run: run, Network: n.ID, Cluster: "primary", Nodes: nodes, Storage: "raft",
		Edition: release.Community, Version: release.Version{Product: "vault", Core: "2.2.2", Full: "2.2.2"},
		Binary: bin, Image: AlpineImage,
		Progress: func(s string) { tb.mu.Lock(); tb.progress = append(tb.progress, s); tb.mu.Unlock() },
	}
	return tb
}

func (tb *testbed) deploy() (product.Deployment, error) {
	return tb.p.Deploy(context.Background(), tb.rt, tb.spec)
}

func (tb *testbed) mustDeploy() product.Deployment {
	tb.t.Helper()
	d, err := tb.deploy()
	if err != nil {
		tb.t.Fatalf("Deploy: %v", err)
	}
	return d
}

func (tb *testbed) containers() int {
	tb.t.Helper()
	cs, err := tb.rt.List(context.Background(), "r1")
	if err != nil {
		tb.t.Fatal(err)
	}
	return len(cs)
}

func (tb *testbed) said(sub string) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	for _, p := range tb.progress {
		if strings.Contains(p, sub) {
			return true
		}
	}
	return false
}

func TestDeploysAThreeNodeCluster(t *testing.T) {
	tb := newTestbed(t, 3)
	d := tb.mustDeploy()

	if d.Cluster != "primary" || d.Product != "vault" || len(d.Nodes) != 3 {
		t.Fatalf("deployment = %+v", d)
	}
	for i, want := range []string{"node-1", "node-2", "node-3"} {
		n := d.Nodes[i]
		if n.Name != want || n.Cluster != "primary" || n.ContainerID == "" || n.IP == "" {
			t.Errorf("node %d = %+v, want %s", i, n, want)
		}
	}

	// The credentials the Cluster tab needs: the root token and all five unseal keys, threshold three.
	if d.Creds["rootToken"] != simToken || len(unsealKeys(&d)) != 5 || d.State["unsealThreshold"] != "3" {
		t.Errorf("creds = %v state = %v", d.Creds, d.State)
	}

	for _, name := range []string{"node-1", "node-2", "node-3"} {
		st := tb.sim.state(name)
		if !st.started || !st.initialized || st.sealed {
			t.Errorf("%s = %+v, want it started, in the cluster and unsealed", name, st)
		}
	}
	if tb.sim.voterCount() != 3 {
		t.Errorf("voters = %d, want 3", tb.sim.voterCount())
	}
	if n := tb.sim.count(isCmd("", "operator", "init")); n != 1 {
		t.Errorf("init ran %d times, want once", n)
	}
	if n := tb.sim.count(isCmd("node-1", "operator", "init")); n != 1 {
		t.Error("init did not run on the first node")
	}
	if n := tb.sim.count(isCmd("", "operator", "unseal")); n != 9 {
		t.Errorf("unseal ran %d times, want the threshold (3) on each of 3 nodes", n)
	}
	for _, name := range []string{"node-2", "node-3"} {
		if tb.sim.count(isCmd(name, "operator", "raft", "join", "http://"+d.Nodes[0].IP+":8200")) != 1 {
			t.Errorf("%s did not join the first node's API address once", name)
		}
	}
	if tb.sim.count(isCmd("node-1", "operator", "raft", "join")) != 0 {
		t.Error("the first node was joined to itself")
	}
	if tb.sim.auditEnabled != 1 {
		t.Errorf("audit was enabled %d times, want once (R-VIEW-3)", tb.sim.auditEnabled)
	}
	if tb.containers() != 3 {
		t.Errorf("containers = %d", tb.containers())
	}
	for _, line := range []string{"initialising node-1: 5 key shares, threshold 3", "joining node-2 to the cluster", "joining node-3 to the cluster", "Vault 2.2.2 is up with 3 node(s)"} {
		if !tb.said(line) {
			t.Errorf("progress never said %q", line)
		}
	}
}

func TestEachNodeGetsItsOwnBinaryAndConfig(t *testing.T) {
	tb := newTestbed(t, 3)
	d := tb.mustDeploy()

	for _, n := range d.Nodes {
		bin, ok := tb.rt.File(n.ContainerID, binaryPath)
		if !ok || string(bin.Content) != fakeBinaryBytes || bin.Mode != 0o755 {
			t.Errorf("%s binary = %+v, %v, want the cached binary, executable", n.Name, bin, ok)
		}
		cfg, ok := tb.rt.File(n.ContainerID, configPath)
		if !ok {
			t.Fatalf("%s has no config", n.Name)
		}
		for _, want := range []string{`node_id = "` + n.Name + `"`, `api_addr = "http://` + n.IP + `:8200"`, `cluster_addr = "http://` + n.IP + `:8201"`} {
			if !strings.Contains(string(cfg.Content), want) {
				t.Errorf("%s config is missing %q:\n%s", n.Name, want, cfg.Content)
			}
		}
		if _, ok := tb.rt.File(n.ContainerID, licensePath); ok {
			t.Errorf("%s has a license although the edition is community", n.Name)
		}
	}
}

func TestASingleNodeNeverJoinsAnything(t *testing.T) {
	tb := newTestbed(t, 1)
	d := tb.mustDeploy()
	if len(d.Nodes) != 1 || tb.sim.voterCount() != 1 {
		t.Fatalf("nodes=%d voters=%d", len(d.Nodes), tb.sim.voterCount())
	}
	if n := tb.sim.count(isCmd("", "operator", "raft", "join")); n != 0 {
		t.Errorf("a single node ran %d join commands", n)
	}
	if tb.sim.auditEnabled != 1 {
		t.Error("audit was not enabled")
	}
}

func TestALaterClusterNamesItsNodesAfterItself(t *testing.T) {
	tb := newTestbed(t, 2)
	tb.spec.Cluster, tb.spec.NamePrefix = "dr-secondary", "dr-secondary"
	d := tb.mustDeploy()

	if d.Nodes[0].Name != "dr-secondary-node-1" || d.Nodes[1].Name != "dr-secondary-node-2" {
		t.Errorf("nodes = %+v (section 3.6.1)", d.Nodes)
	}
	cfg, _ := tb.rt.File(d.Nodes[1].ContainerID, configPath)
	if !strings.Contains(string(cfg.Content), `node_id = "dr-secondary-node-2"`) {
		t.Errorf("the raft node ID is not the node's name:\n%s", cfg.Content)
	}
}

func TestSecretsStayOffTheCommandLine(t *testing.T) {
	tb := newTestbed(t, 2)
	tb.mustDeploy()

	tb.sim.mu.Lock()
	defer tb.sim.mu.Unlock()
	for _, c := range tb.sim.calls {
		line := describe(c)
		if strings.Contains(line, simToken) {
			t.Errorf("the root token is on a command line: %s (R-QUAL-3)", line)
		}
		isUnseal := len(c.argv) > 2 && c.argv[1] == "operator" && c.argv[2] == "unseal"
		for _, k := range simKeys {
			if strings.Contains(line, k) && !isUnseal {
				t.Errorf("an unseal key is on a command line that is not unseal: %s", line)
			}
		}
		needsToken := (len(c.argv) > 1 && c.argv[1] == "audit") || (len(c.argv) > 3 && c.argv[3] == "list-peers")
		if got := c.env["VAULT_TOKEN"]; needsToken && got != simToken {
			t.Errorf("%s did not carry the token in its environment", line)
		}
		if got := c.env["VAULT_TOKEN"]; !needsToken && got != "" {
			t.Errorf("%s carried a token it does not need", line)
		}
		if c.env["VAULT_ADDR"] != "http://127.0.0.1:8200" {
			t.Errorf("%s did not set the local address", line)
		}
	}
}

func TestEnterpriseCarriesTheLicenseOnlyWhereItIsNeeded(t *testing.T) {
	tb := newTestbed(t, 2)
	tb.spec.Edition = release.Enterprise
	tb.spec.Version = release.Version{Product: "vault", Core: "2.2.2", Full: "2.2.2+ent"}
	tb.spec.Settings = settingsStub{value: "02MV4UU43BK5HGYYTOJZWFQMTMNNEWU33JJRKFC6SNKVNGSVJRLE6VKUZ5", ok: true}
	d := tb.mustDeploy()

	for _, n := range d.Nodes {
		lic, ok := tb.rt.File(n.ContainerID, licensePath)
		if !ok || lic.Mode != 0o600 || !strings.HasPrefix(string(lic.Content), "02MV4") {
			t.Errorf("%s license = %+v, %v, want it present, owner-only (R-SET-3)", n.Name, lic, ok)
		}
		cfg, _ := tb.rt.File(n.ContainerID, configPath)
		if !strings.Contains(string(cfg.Content), `license_path = "/etc/vault.d/license.hclic"`) || strings.Contains(string(cfg.Content), "02MV4") {
			t.Errorf("%s config = %s, want a path to the license and never the license", n.Name, cfg.Content)
		}
	}
	for _, line := range tb.progress {
		if strings.Contains(line, "02MV4") {
			t.Errorf("the license is in a progress line: %q", line)
		}
	}
}

func TestEnterpriseWithoutALicenseFailsBeforeCreatingAnything(t *testing.T) {
	tb := newTestbed(t, 3)
	tb.spec.Edition = release.Enterprise
	tb.spec.Settings = settingsStub{}

	_, err := tb.deploy()
	if !errors.Is(err, ErrNoLicense) || !strings.Contains(err.Error(), "add it in Settings") {
		t.Fatalf("Deploy = %v, want ErrNoLicense with what to do", err)
	}
	if tb.containers() != 0 {
		t.Error("a deployment that could not start still created containers")
	}

	tb.spec.Settings = nil // no settings given at all
	if _, err := tb.deploy(); !errors.Is(err, ErrNoLicense) {
		t.Errorf("Deploy with no settings = %v, want ErrNoLicense", err)
	}
}

func TestDeployRefusesBadRequestsBeforeCreatingAnything(t *testing.T) {
	tests := []struct {
		name string
		edit func(*product.DeploySpec)
		want string
	}{
		{"no nodes", func(s *product.DeploySpec) { s.Nodes = 0 }, "1 to 9 nodes"},
		{"too many nodes", func(s *product.DeploySpec) { s.Nodes = 10 }, "1 to 9 nodes"},
		{"another storage backend", func(s *product.DeploySpec) { s.Storage = "consul" }, `"consul" storage backend is not supported`},
		{"an edition vault does not have", func(s *product.DeploySpec) { s.Edition = "ent.fips1403" }, "no \"ent.fips1403\" edition"},
		{"the hsm edition, which is not built yet", func(s *product.DeploySpec) { s.Edition = release.EnterpriseHSM }, "HSM edition is not supported yet"},
		{"no image", func(s *product.DeploySpec) { s.Image = "" }, "needs an image"},
		{"no binary", func(s *product.DeploySpec) { s.Binary = "" }, "needs an image"},
		{"a binary that is not there", func(s *product.DeploySpec) { s.Binary = "/no/such/vault" }, "cannot be read"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tb := newTestbed(t, 3)
			tt.edit(&tb.spec)
			_, err := tb.deploy()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Deploy = %v, want an error containing %q", err, tt.want)
			}
			if tb.containers() != 0 {
				t.Error("a refused request still created containers")
			}
		})
	}
}

func TestANodeThatNeverStartsIsNamedWithItsLog(t *testing.T) {
	tb := newTestbed(t, 3)
	tb.sim.neverStart["node-2"] = true

	d, err := tb.deploy()
	if err == nil {
		t.Fatal("Deploy succeeded although node-2 never started")
	}
	for _, want := range []string{"node-2 did not answer within", "Last seen: Error checking seal status: connection refused", "Last lines of the log of node-2", "[ERROR] storage: boom"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error is missing %q:\n%v", want, err)
		}
	}
	// What was built is handed back, so its logs can be read and the engine can clean it up.
	if len(d.Nodes) != 3 || tb.containers() != 3 {
		t.Errorf("nodes=%d containers=%d, want the failed deployment left in place", len(d.Nodes), tb.containers())
	}
}

func TestAFailedInitReportsTheExitCodeButNotTheOutput(t *testing.T) {
	tb := newTestbed(t, 1)
	tb.sim.initFails = true

	_, err := tb.deploy()
	if err == nil || !strings.Contains(err.Error(), "initialise Vault on node-1") || !strings.Contains(err.Error(), "exit code 2") {
		t.Fatalf("Deploy = %v", err)
	}
	if strings.Contains(err.Error(), "SECRET-OUT") || strings.Contains(err.Error(), simToken) {
		t.Errorf("the error holds the command's output, which has the keys: %v (R-QUAL-3)", err)
	}
}

func TestJoiningIsRetriedWhileTheLeaderSettles(t *testing.T) {
	tb := newTestbed(t, 2)
	tb.sim.joinFailures = 5

	tb.mustDeploy()
	if n := tb.sim.count(isCmd("node-2", "operator", "raft", "join")); n != 6 {
		t.Errorf("join ran %d times, want 5 failures and then a success", n)
	}
}

func TestAJoinThatNeverWorksSaysWhy(t *testing.T) {
	tb := newTestbed(t, 2)
	tb.sim.joinFailures = 1_000_000

	_, err := tb.deploy()
	if err == nil || !strings.Contains(err.Error(), "node-2 did not join the cluster within") || !strings.Contains(err.Error(), "Last seen: leader not ready") {
		t.Errorf("Deploy = %v", err)
	}
}

func TestDeployStopsWhenTheContextEnds(t *testing.T) {
	tb := newTestbed(t, 3)
	tb.sim.neverStart["node-1"] = true // it would otherwise wait for the full timeout
	tb.p = New(Options{Timing: Timing{Poll: time.Millisecond, NodeReady: time.Minute, Leader: time.Minute, Join: time.Minute}})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := tb.p.Deploy(ctx, tb.rt, tb.spec)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Deploy = %v, want the context's error", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("Deploy kept waiting after its context ended")
	}
}

func TestAPIPortsArePublishedOnTheLoopbackOnly(t *testing.T) {
	tb := newTestbed(t, 3)
	tb.spec.Ports = runtime.NewPortAllocator()
	d := tb.mustDeploy()

	if len(d.Published) != 3 {
		t.Fatalf("published = %+v, want one port per node", d.Published)
	}
	seen := map[int]bool{}
	for _, pm := range d.Published {
		if pm.Container != 8200 || pm.Host < runtime.PortRangeStart || pm.Host > runtime.PortRangeEnd || seen[pm.Host] {
			t.Errorf("port mapping %+v is wrong or repeated", pm)
		}
		seen[pm.Host] = true
	}
	cs, _ := tb.rt.List(context.Background(), "r1")
	for _, c := range cs {
		if len(c.Ports) != 1 || c.Ports[0].Container != 8200 {
			t.Errorf("container %s ports = %+v", c.Name, c.Ports)
		}
	}
}

func TestTheDeploymentMeetsTheProductContract(t *testing.T) {
	rt := rtfake.New()
	newSim(rt)
	bin := filepath.Join(t.TempDir(), "vault")
	if err := os.WriteFile(bin, []byte(fakeBinaryBytes), 0o755); err != nil {
		t.Fatal(err)
	}
	producttest.Contract(t, New(Options{Timing: fastTiming}), producttest.Options{
		Runtime: rt, SkipVersions: true,
		Spec: func() product.DeploySpec {
			return product.DeploySpec{
				Cluster: "primary", Nodes: 3, Storage: "raft", Edition: release.Community,
				Version: release.Version{Product: "vault", Core: "2.2.2", Full: "2.2.2"},
				Binary:  bin, Image: AlpineImage,
			}
		},
	})
}
