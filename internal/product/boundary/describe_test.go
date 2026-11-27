package boundary

import (
	"context"
	"testing"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/product"
)

func factsOf(f []product.Fact) map[string]product.Fact {
	m := map[string]product.Fact{}
	for _, x := range f {
		m[x.Label] = x
	}
	return m
}

func nodeDetails(t *testing.T, v product.ClusterView, name string) map[string]product.Fact {
	t.Helper()
	for _, n := range v.Sections[0].Nodes {
		if n.Name == name {
			return factsOf(n.Details)
		}
	}
	t.Fatalf("the view has no node %s", name)
	return nil
}

func TestTheClusterTabShowsWhereToSignInAndWhatWasCreated(t *testing.T) {
	tb := newTestbed(t, 3)
	d := tb.mustDeploy()
	v := tb.p.Describe(context.Background(), tb.rt, d)
	if len(v.Sections) != 1 || v.Sections[0].Title != "Boundary cluster" {
		t.Fatalf("sections = %+v", v.Sections)
	}
	f := factsOf(v.Sections[0].Facts)

	if got := f["Cluster URL"].Value; got != "http://127.0.0.1:"+d.State["port:lb-node-1"] {
		t.Errorf("Cluster URL = %q, want the load balancer's published address", got)
	}
	for _, c := range []string{"node-1", "node-2", "node-3"} {
		if got := f["Controller "+c].Value; got != "http://127.0.0.1:"+d.State["port:"+c] {
			t.Errorf("Controller %s = %q", c, got)
		}
	}
	for label, want := range map[string]string{
		"Admin login name": "admin", "Auth method ID": "ampw_sim", "Org scope ID": "o_sim", "Project scope ID": "p_sim",
		"Host catalog ID": "hcst_sim", "Host set ID": "hsst_sim", "Host ID": "hst_sim", "Target ID": "ttcp_sim", "Version": "0.21.3 community",
	} {
		if f[label].Value != want || f[label].Secret {
			t.Errorf("%s = %+v, want %q shown plainly", label, f[label], want)
		}
	}
	if p := f["Admin password"]; p.Value != simPassword || !p.Secret {
		t.Errorf("Admin password = %+v, want a secret", p)
	}
	for _, shown := range v.Sections[0].Facts {
		for _, secret := range []string{d.Creds["kmsRoot"], d.Creds["kmsWorkerAuth"], d.Creds["kmsRecovery"]} {
			if shown.Value == secret {
				t.Errorf("the tab shows the KMS key under %q", shown.Label)
			}
		}
	}
}

func TestEachPartReportsItsOwnRoleAndLiveState(t *testing.T) {
	tb := newTestbed(t, 2)
	d := tb.mustDeploy()
	v := tb.p.Describe(context.Background(), tb.rt, d)

	want := map[string][2]string{
		"node-1": {RoleController, "healthy"}, "node-2": {RoleController, "healthy"},
		"worker-node-1": {RoleWorker, "registered"}, "lb-node-1": {RoleLB, "running"},
		"db-node-1": {RoleDatabase, "accepting connections"}, "target-node-1": {RoleTarget, "SSH answering"},
	}
	for name, w := range want {
		det := nodeDetails(t, v, name)
		if det["Role"].Value != w[0] || det["State"].Value != w[1] {
			t.Errorf("%s = role %q, state %q; want %q, %q", name, det["Role"].Value, det["State"].Value, w[0], w[1])
		}
	}
	if det := nodeDetails(t, v, "node-1"); det["Version"].Value != "0.21.3 community" {
		t.Errorf("controller version = %q", det["Version"].Value)
	}
	// Credentials are shown where they are used, as secrets.
	if p := nodeDetails(t, v, "db-node-1")["Password"]; p.Value == "" || !p.Secret {
		t.Errorf("database password = %+v", p)
	}
	if p := nodeDetails(t, v, "target-node-1")["SSH password"]; p.Value == "" || !p.Secret {
		t.Errorf("SSH password = %+v", p)
	}
	if nodeDetails(t, v, "target-node-1")["SSH user"].Value != "target" {
		t.Error("the SSH user is not shown")
	}
}

func TestAStoppedOrRemovedPartIsShownAsSuchAndNeverMakesTheTabFail(t *testing.T) {
	tb := newTestbed(t, 2)
	d := tb.mustDeploy()
	tb.sim.mu.Lock()
	tb.sim.started["worker-node-1"] = false // the worker's process is gone
	tb.sim.started["node-2"] = false        // and so is a controller's
	tb.sim.mu.Unlock()
	// A removed node's container no longer exists.
	for _, n := range d.Nodes {
		if n.Name == "lb-node-1" {
			if err := tb.rt.Remove(context.Background(), n.ContainerID); err != nil {
				t.Fatal(err)
			}
		}
	}

	v := tb.p.Describe(context.Background(), tb.rt, d)
	if got := nodeDetails(t, v, "worker-node-1")["State"].Value; got != "not running" {
		t.Errorf("worker = %q", got)
	}
	if got := nodeDetails(t, v, "node-2")["State"].Value; got != "not answering" {
		t.Errorf("controller = %q", got)
	}
	if got := nodeDetails(t, v, "lb-node-1")["State"].Value; got != unknown {
		t.Errorf("a removed load balancer = %q, want %q", got, unknown)
	}
	if got := nodeDetails(t, v, "node-1")["State"].Value; got != "healthy" {
		t.Errorf("the controller that is up = %q", got)
	}
}

func TestAWorkerThatIsRunningButNotRegisteredIsToldApart(t *testing.T) {
	tb := newTestbed(t, 1)
	d := tb.mustDeploy()
	tb.sim.mu.Lock()
	tb.sim.workerNeverShows = true
	tb.sim.mu.Unlock()
	if got := nodeDetails(t, tb.p.Describe(context.Background(), tb.rt, d), "worker-node-1")["State"].Value; got != "running, not registered" {
		t.Errorf("worker = %q", got)
	}

	// With every controller down the registration cannot be read at all.
	tb.sim.mu.Lock()
	tb.sim.workerNeverShows = false
	tb.sim.started["node-1"] = false
	tb.sim.mu.Unlock()
	d.Creds["adminPassword"] = "wrong"
	if got := nodeDetails(t, tb.p.Describe(context.Background(), tb.rt, d), "worker-node-1")["State"].Value; got != unknown {
		t.Errorf("worker with no way to ask = %q", got)
	}
}

func TestEveryNodeNamesItsLogs(t *testing.T) {
	p := New(Options{})
	for name, want := range map[string][2]string{
		"node-1": {"operational", OperationalLog}, "node-3": {"operational", OperationalLog},
		"worker-node-1": {"operational", OperationalLog}, "lb-node-1": {"operational", HAProxyLog},
		"db-node-1": {"database", ""}, "target-node-1": {"ssh", SSHLog},
		"edge-lb-node-1": {"operational", HAProxyLog}, "edge-node-2": {"operational", OperationalLog},
	} {
		streams := p.LogStreams(primitive.Node{Name: name})
		if len(streams) != 1 || streams[0].ID != want[0] || streams[0].File != want[1] || streams[0].Label == "" {
			t.Errorf("%s: streams = %+v, want %v", name, streams, want)
		}
	}
}

func TestNodeActionsOfferEverythingButTheDatabaseCannotBeRemoved(t *testing.T) {
	p := New(Options{})
	d := product.Deployment{State: map[string]string{
		"role:node-1": RoleController, "role:worker-node-1": RoleWorker, "role:lb-node-1": RoleLB,
		"role:db-node-1": RoleDatabase, "role:target-node-1": RoleTarget,
	}}
	has := func(acts []product.NodeAction, a product.NodeAction) bool {
		for _, x := range acts {
			if x == a {
				return true
			}
		}
		return false
	}
	for _, n := range []string{"node-1", "worker-node-1", "lb-node-1", "target-node-1"} {
		acts := p.NodeActions(d, n)
		for _, a := range []product.NodeAction{product.ActionStop, product.ActionStart, product.ActionRemove, product.ActionRevive} {
			if !has(acts, a) {
				t.Errorf("%s does not offer %s", n, a)
			}
		}
	}
	db := p.NodeActions(d, "db-node-1")
	if !has(db, product.ActionStop) || !has(db, product.ActionStart) || has(db, product.ActionRemove) || has(db, product.ActionRevive) {
		t.Errorf("the database offers %v, want only stop and start, because its data cannot come back", db)
	}
	// Where the deployment has no record, the role comes from the name.
	if acts := p.NodeActions(product.Deployment{}, "db-node-1"); has(acts, product.ActionRemove) {
		t.Error("a database found by name can be removed")
	}
}

func TestRoleFromName(t *testing.T) {
	for name, want := range map[string]string{
		"node-1": RoleController, "node-9": RoleController, "worker-node-1": RoleWorker, "lb-node-1": RoleLB, "db-node-1": RoleDatabase,
		"target-node-1": RoleTarget, "edge-node-1": RoleController, "edge-worker-node-1": RoleWorker, "edge-db-node-1": RoleDatabase,
	} {
		if got := roleFromName(name); got != want {
			t.Errorf("roleFromName(%q) = %q, want %q", name, got, want)
		}
	}
}
