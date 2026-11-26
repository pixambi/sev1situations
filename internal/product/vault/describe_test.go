package vault

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/release"
	"github.com/pixambi/sev1situations/internal/runtime"
)

// facts indexes a section's facts by label.
func facts(fs []product.Fact) map[string]product.Fact {
	m := map[string]product.Fact{}
	for _, f := range fs {
		m[f.Label] = f
	}
	return m
}

func nodeFacts(sec product.Section, name string) map[string]product.Fact {
	for _, n := range sec.Nodes {
		if n.Name == name {
			return facts(n.Details)
		}
	}
	return nil
}

func describedCluster(t *testing.T, nodes int, edit func(*testbed)) (*testbed, product.Deployment, product.Section) {
	t.Helper()
	tb := newTestbed(t, nodes)
	tb.spec.Ports = runtime.NewPortAllocator()
	if edit != nil {
		edit(tb)
	}
	d := tb.mustDeploy()
	view := tb.p.Describe(context.Background(), tb.rt, d)
	if len(view.Sections) != 1 {
		t.Fatalf("sections = %d, want one for the cluster", len(view.Sections))
	}
	return tb, d, view.Sections[0]
}

func TestDescribeShowsTheClusterMinimum(t *testing.T) {
	_, d, sec := describedCluster(t, 3, nil)

	if sec.Title != "Primary cluster" {
		t.Errorf("title = %q", sec.Title)
	}
	f := facts(sec.Facts)
	if f["Role"].Value != "primary" || f["Version"].Value != "2.2.2 community" {
		t.Errorf("role/version = %+v %+v", f["Role"], f["Version"])
	}
	if !strings.HasPrefix(f["API address"].Value, "http://127.0.0.1:") {
		t.Errorf("API address = %q, want a loopback address a browser on this computer can use", f["API address"].Value)
	}
	if f["Root token"].Value != d.Creds["rootToken"] || !f["Root token"].Secret {
		t.Errorf("root token = %+v, want the token, flagged secret (R-VIEW-5)", f["Root token"])
	}
	for i := 1; i <= 5; i++ {
		k := f["Unseal key "+string(rune('0'+i))]
		if k.Value == "" || !k.Secret {
			t.Errorf("unseal key %d = %+v, want it shown and flagged secret", i, k)
		}
	}
	if f["Unseal threshold"].Value != "3 of 5" || f["Unseal threshold"].Secret {
		t.Errorf("threshold = %+v", f["Unseal threshold"])
	}
	if _, ok := f["License"]; ok {
		t.Error("a community cluster shows a license")
	}
}

func TestDescribeReadsEachNodeLive(t *testing.T) {
	_, _, sec := describedCluster(t, 3, nil)

	if len(sec.Nodes) != 3 {
		t.Fatalf("nodes = %d", len(sec.Nodes))
	}
	want := map[string]string{"node-1": "active", "node-2": "standby", "node-3": "standby"}
	for name, role := range want {
		f := nodeFacts(sec, name)
		if f["Node ID"].Value != name || f["Vault"].Value != "unsealed" || f["HA role"].Value != role || f["Version"].Value != "2.2.2 community" {
			t.Errorf("%s = %+v, want unsealed %s on 2.2.2 community", name, f, role)
		}
		if !strings.HasPrefix(f["API address"].Value, "http://127.0.0.1:") {
			t.Errorf("%s API address = %q", name, f["API address"].Value)
		}
	}
	// Each node publishes its own port.
	a, b := nodeFacts(sec, "node-1")["API address"].Value, nodeFacts(sec, "node-2")["API address"].Value
	if a == b {
		t.Errorf("two nodes share the address %s", a)
	}
}

func TestDescribeFollowsAChangeInTheCluster(t *testing.T) {
	tb, d, _ := describedCluster(t, 3, nil)

	// Seal node-3 behind Describe's back, as a scenario step might.
	tb.sim.mu.Lock()
	for id, n := range tb.sim.nodes {
		if n.name == "node-3" {
			tb.sim.nodes[id].sealed = true
		}
	}
	tb.sim.mu.Unlock()

	f := nodeFacts(tb.p.Describe(context.Background(), tb.rt, d).Sections[0], "node-3")
	if f["Vault"].Value != "sealed" || f["HA role"].Value != "-" {
		t.Errorf("node-3 = %+v, want it sealed with no role", f)
	}
}

func TestDescribeNeverFailsForANodeItCannotReach(t *testing.T) {
	tb, d, _ := describedCluster(t, 3, nil)

	// A stopped container makes exec fail, and the runtime being down makes everything fail.
	if err := tb.rt.Stop(context.Background(), d.Nodes[1].ContainerID); err != nil {
		t.Fatal(err)
	}
	sec := tb.p.Describe(context.Background(), tb.rt, d).Sections[0]
	if f := nodeFacts(sec, "node-2"); f["Vault"].Value != "unknown" || f["HA role"].Value != "unknown" || f["Version"].Value != "unknown" {
		t.Errorf("a stopped node = %+v, want unknown", f)
	}
	if f := nodeFacts(sec, "node-1"); f["Vault"].Value != "unsealed" {
		t.Errorf("a reachable node = %+v, want it unaffected by its neighbour", f)
	}

	tb.rt.SetUnavailable(errors.New("machine stopped"))
	defer tb.rt.SetUnavailable(nil)
	sec = tb.p.Describe(context.Background(), tb.rt, d).Sections[0]
	if facts(sec.Facts)["Root token"].Value == "" || len(sec.Nodes) != 3 {
		t.Errorf("with the runtime down, section = %+v, want the cluster facts and every node still listed", sec)
	}
	for _, n := range sec.Nodes {
		if facts(n.Details)["Vault"].Value != "unknown" {
			t.Errorf("%s = %+v, want unknown while the runtime is down", n.Name, n.Details)
		}
	}
}

func TestASlowNodeDoesNotStallTheTab(t *testing.T) {
	tb, d, _ := describedCluster(t, 3, nil)
	tb.p = New(Options{Timing: Timing{Describe: 50 * time.Millisecond}})

	hung := d.Nodes[2].ContainerID
	tb.rt.SetExec(func(c runtime.Container, cmd []string, o runtime.ExecOptions) (runtime.ExecResult, error) {
		if c.ID == hung {
			time.Sleep(2 * time.Second) // a node that does not answer
		}
		return tb.sim.exec(c, cmd, o)
	})

	start := time.Now()
	sec := tb.p.Describe(context.Background(), tb.rt, d).Sections[0]
	if took := time.Since(start); took > 1500*time.Millisecond {
		t.Errorf("Describe took %s, want a slow node to be given up on after its limit", took)
	}
	if got := nodeFacts(sec, "node-3")["Vault"].Value; got != "unknown" {
		t.Errorf("the slow node = %q, want unknown", got)
	}
	if got := nodeFacts(sec, "node-1")["Vault"].Value; got != "unsealed" {
		t.Errorf("a fast node = %q, want it read normally", got)
	}
}

func TestEnterpriseShowsWhereTheLicenseCameFromButNeverTheLicense(t *testing.T) {
	const license = "02MV4UU43BK5HGYYTOJZWFQMTMNNEWU33JJRKFC6SNKVNGSVJRLE6VKUZ5"
	_, _, sec := describedCluster(t, 1, func(tb *testbed) {
		tb.spec.Edition = release.Enterprise
		tb.spec.Version = release.Version{Product: "vault", Edition: release.Enterprise, Core: "2.2.2", Full: "2.2.2+ent"}
		tb.spec.Settings = settingsStub{value: license, ok: true}
	})

	f := facts(sec.Facts)
	if f["License"].Value != "supplied from Settings" || f["Version"].Value != "2.2.2 Enterprise" {
		t.Errorf("facts = %+v", f)
	}
	for _, fact := range sec.Facts {
		if strings.Contains(fact.Value, license) {
			t.Errorf("the license text is on the Cluster tab in %q", fact.Label)
		}
	}
	if nodeFacts(sec, "node-1")["Version"].Value != "2.2.2 Enterprise" {
		t.Errorf("node version = %q", nodeFacts(sec, "node-1")["Version"].Value)
	}
}

func TestADeploymentWithNothingPublishedSaysSo(t *testing.T) {
	tb := newTestbed(t, 1) // no port allocator
	d := tb.mustDeploy()
	sec := tb.p.Describe(context.Background(), tb.rt, d).Sections[0]
	if got := facts(sec.Facts)["API address"].Value; got != "not published to this computer" {
		t.Errorf("API address = %q", got)
	}
}

func TestClusterTitle(t *testing.T) {
	for in, want := range map[string]string{"primary": "Primary cluster", "dr-secondary": "Dr secondary cluster", "": "Cluster"} {
		if got := clusterTitle(in); got != want {
			t.Errorf("clusterTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLogStreams(t *testing.T) {
	streams := New(Options{}).LogStreams(primitive.Node{Name: "node-1"})
	if len(streams) != 2 || streams[0].ID != "operational" || streams[1].ID != "audit" {
		t.Fatalf("streams = %+v, want the operational and audit logs, the minimum for Vault (R-VIEW-8)", streams)
	}
	if streams[0].File != OperationalLog || streams[1].File != AuditLog {
		t.Errorf("files = %q, %q, want the files Vault writes in the container", streams[0].File, streams[1].File)
	}
}
