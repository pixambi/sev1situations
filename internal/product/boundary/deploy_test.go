package boundary

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/release"
	"github.com/pixambi/sev1situations/internal/runtime"
	rtfake "github.com/pixambi/sev1situations/internal/runtime/fake"
)

var fastTiming = Timing{Poll: time.Millisecond, Database: 150 * time.Millisecond, Controller: 150 * time.Millisecond, Worker: 150 * time.Millisecond, Install: 150 * time.Millisecond, Describe: 150 * time.Millisecond}

type testbed struct {
	t        *testing.T
	rt       *rtfake.Runtime
	sim      *sim
	p        *Product
	spec     product.DeploySpec
	mu       sync.Mutex
	progress []string
}

func newTestbed(t *testing.T, controllers int) *testbed {
	t.Helper()
	rt := rtfake.New()
	tb := &testbed{t: t, rt: rt, sim: newSim(rt), p: New(Options{Timing: fastTiming})}

	bin := filepath.Join(t.TempDir(), "boundary")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho boundary\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	run := runtime.Run{ID: "r1", Scenario: "test"}
	n, err := rt.CreateNetwork(context.Background(), runtime.NetworkOptions{Run: run, Name: "net"})
	if err != nil {
		t.Fatal(err)
	}
	tb.spec = product.DeploySpec{
		Run: run, Network: n.ID, Cluster: "boundary", Nodes: controllers,
		Edition: release.Community, Version: release.Version{Product: "boundary", Core: "0.21.3", Full: "0.21.3"},
		Binary: bin, Image: AlpineImage, Ports: runtime.NewPortAllocator(),
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

func (tb *testbed) progressText() string {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return strings.Join(tb.progress, "\n")
}

func nodeNamed(t *testing.T, d product.Deployment, name string) (n struct{ ID, IP string }) {
	t.Helper()
	for _, x := range d.Nodes {
		if x.Name == name {
			return struct{ ID, IP string }{x.ContainerID, x.IP}
		}
	}
	t.Fatalf("no node %q in %v", name, d.Nodes)
	return
}

func fileContent(t *testing.T, tb *testbed, node product.Deployment, name, path string) string {
	t.Helper()
	n := nodeNamed(t, node, name)
	f, ok := tb.rt.File(n.ID, path)
	if !ok {
		t.Fatalf("%s has no file %s", name, path)
	}
	return string(f.Content)
}

func TestDeploysEveryPartOfABoundaryDeployment(t *testing.T) {
	tb := newTestbed(t, 3)
	d := tb.mustDeploy()

	want := map[string]string{
		"node-1": RoleController, "node-2": RoleController, "node-3": RoleController,
		"worker-node-1": RoleWorker, "lb-node-1": RoleLB, "db-node-1": RoleDatabase, "target-node-1": RoleTarget,
	}
	if len(d.Nodes) != len(want) {
		t.Fatalf("%d nodes, want %d: %v", len(d.Nodes), len(want), d.Nodes)
	}
	for _, n := range d.Nodes {
		if want[n.Name] == "" || d.State["role:"+n.Name] != want[n.Name] || n.Cluster != "boundary" || n.ContainerID == "" || n.IP == "" {
			t.Errorf("node %+v with role %q", n, d.State["role:"+n.Name])
		}
	}
	if d.Cluster != "boundary" || d.Product != "boundary" {
		t.Errorf("deployment = %s %s", d.Cluster, d.Product)
	}
	// What the Cluster tab and the primitives need is kept.
	for k, v := range map[string]string{
		"adminLogin": "admin", "authMethodId": "ampw_sim", "orgScopeId": "o_sim", "projectScopeId": "p_sim",
		"hostCatalogId": "hcst_sim", "hostSetId": "hsst_sim", "hostId": "hst_sim", "targetId": "ttcp_sim", "targetUser": "target",
	} {
		if d.State[k] != v {
			t.Errorf("State[%s] = %q, want %q", k, d.State[k], v)
		}
	}
	for _, k := range []string{"adminPassword", "kmsRoot", "kmsWorkerAuth", "kmsRecovery", "databasePassword", "targetPassword"} {
		if d.Creds[k] == "" {
			t.Errorf("credential %s was not kept", k)
		}
	}
	if d.Creds["adminPassword"] != simPassword {
		t.Errorf("adminPassword = %q", d.Creds["adminPassword"])
	}
	// The controllers and the load balancer are reachable from this computer.
	for _, n := range []string{"node-1", "node-2", "node-3", "lb-node-1"} {
		if d.State["port:"+n] == "" {
			t.Errorf("%s has no published port", n)
		}
	}
	for _, n := range []string{"worker-node-1", "db-node-1", "target-node-1"} {
		if d.State["port:"+n] != "" {
			t.Errorf("%s publishes a port, which only the controllers and the load balancer should", n)
		}
	}
}

func TestPartsAreStartedInTheOrderTheyDependOnEachOther(t *testing.T) {
	tb := newTestbed(t, 3)
	tb.sim.dbReady = 2
	tb.mustDeploy()

	ev := strings.Join(tb.sim.eventLog(), " > ")
	order := []string{"db ready", "init node-1", "start node-1", "healthy node-1", "start node-2", "healthy node-2", "start node-3", "healthy node-3",
		"start lb-node-1", "start worker-node-1", "worker registered", "install ssh on target-node-1", "hosts update"}
	last := -1
	for _, e := range order {
		i := strings.Index(ev, e)
		if i < 0 || i < last {
			t.Fatalf("%q is missing or out of order in:\n%s", e, ev)
		}
		last = i
	}
	// Controllers start one after another: the next is not started until the last was healthy.
	for i, pair := range [][2]string{{"healthy node-1", "start node-2"}, {"healthy node-2", "start node-3"}} {
		if strings.Index(ev, pair[0]) > strings.Index(ev, pair[1]) {
			t.Errorf("pair %d: %q came after %q, so controllers raced on a fresh database", i, pair[0], pair[1])
		}
	}
}

func TestEachPartIsConfiguredWithTheAddressesOfTheOthers(t *testing.T) {
	tb := newTestbed(t, 2)
	d := tb.mustDeploy()
	db, lb, target := nodeNamed(t, d, "db-node-1"), nodeNamed(t, d, "lb-node-1"), nodeNamed(t, d, "target-node-1")

	for _, c := range []string{"node-1", "node-2"} {
		cfg := fileContent(t, tb, d, c, controllerConfig)
		n := nodeNamed(t, d, c)
		for _, want := range []string{`name                = "` + c + `"`, `public_cluster_addr = "` + n.IP + `:9201"`, "@" + db.IP + ":5432/boundary?sslmode=disable"} {
			if !strings.Contains(cfg, want) {
				t.Errorf("%s configuration lacks %q", c, want)
			}
		}
		if f, ok := tb.rt.File(n.ID, binaryPath); !ok || f.Mode != 0o755 {
			t.Errorf("%s has no executable Boundary binary", c)
		}
	}
	worker := fileContent(t, tb, d, "worker-node-1", workerConfig)
	if !strings.Contains(worker, `initial_upstreams = ["`+lb.IP+`:9201"]`) {
		t.Errorf("the worker does not register through the load balancer:\n%s", worker)
	}
	haproxy := fileContent(t, tb, d, "lb-node-1", haproxyConfig)
	for _, c := range []string{"node-1", "node-2"} {
		if !strings.Contains(haproxy, "server "+c+" "+nodeNamed(t, d, c).IP+":9200") {
			t.Errorf("the load balancer does not know controller %s", c)
		}
	}

	// The generated host is pointed at the target host, signed in as the admin.
	updates := tb.sim.callsOn("node-1", binaryPath, "hosts", "update")
	if len(updates) != 1 {
		t.Fatalf("hosts update ran %d times", len(updates))
	}
	args := strings.Join(updates[0].argv, " ")
	if !strings.Contains(args, "-id=hst_sim") || !strings.Contains(args, "-address="+target.IP) || !strings.Contains(args, "-token=env://BOUNDARY_TOKEN") {
		t.Errorf("hosts update = %s", args)
	}
	if updates[0].env["BOUNDARY_TOKEN"] != simToken {
		t.Error("the update was not signed in")
	}
	for _, a := range updates[0].argv {
		if strings.Contains(a, simToken) || strings.Contains(a, simPassword) {
			t.Errorf("a token or password is on the command line: %q", a)
		}
	}
}

func TestSignInPassesThePasswordInTheEnvironmentNotOnTheCommandLine(t *testing.T) {
	tb := newTestbed(t, 1)
	tb.mustDeploy()
	for _, c := range tb.sim.callsOn("", binaryPath, "authenticate") {
		for _, a := range c.argv {
			if strings.Contains(a, simPassword) {
				t.Errorf("the password is in %q", c.argv)
			}
		}
		if c.env["BOUNDARY_PASSWORD"] != simPassword {
			t.Error("the password did not reach the command's environment")
		}
	}
}

func TestNothingSecretIsReportedAsProgress(t *testing.T) {
	tb := newTestbed(t, 2)
	d := tb.mustDeploy()
	text := tb.progressText()
	for _, k := range []string{"adminPassword", "kmsRoot", "kmsWorkerAuth", "kmsRecovery", "databasePassword", "targetPassword"} {
		if strings.Contains(text, d.Creds[k]) {
			t.Errorf("progress mentions the secret %s", k)
		}
	}
}

func TestADatabaseThatIsSlowToStartIsWaitedFor(t *testing.T) {
	tb := newTestbed(t, 1)
	tb.sim.dbReady = 5
	tb.mustDeploy()
	if got := len(tb.sim.callsOn("db-node-1", "pg_isready")); got != 6 {
		t.Errorf("pg_isready ran %d times, want 6", got)
	}
}

func TestADatabaseThatNeverComesUpStopsTheDeploymentAndSaysSo(t *testing.T) {
	tb := newTestbed(t, 1)
	tb.sim.dbReady = 1_000_000
	d, err := tb.deploy()
	if err == nil || !strings.Contains(err.Error(), "db-node-1 did not accept connections within") {
		t.Fatalf("Deploy = %v", err)
	}
	if len(d.Nodes) != 5 {
		t.Errorf("%d nodes were returned, want every container left in place to be inspected", len(d.Nodes))
	}
}

func TestAFailedInitDoesNotRevealThePasswordItWouldHaveShown(t *testing.T) {
	tb := newTestbed(t, 1)
	tb.sim.initFails = true
	_, err := tb.deploy()
	if err == nil || !strings.Contains(err.Error(), "initialise the database from node-1") || !strings.Contains(err.Error(), "could not connect to the database") {
		t.Fatalf("Deploy = %v", err)
	}
	if strings.Contains(err.Error(), simPassword) || strings.Contains(err.Error(), "SECRET-INIT-OUTPUT") {
		t.Errorf("the error shows the output of init: %v", err)
	}
}

func TestAControllerThatNeverGetsHealthyIsNamed(t *testing.T) {
	tb := newTestbed(t, 3)
	tb.sim.controllerNever["node-2"] = true
	_, err := tb.deploy()
	if err == nil || !strings.Contains(err.Error(), "node-2 did not report healthy within") {
		t.Fatalf("Deploy = %v", err)
	}
	if got := tb.sim.callsOn("node-3", "sh"); len(got) != 0 {
		t.Error("node-3 was started although node-2 never became healthy")
	}
}

func TestAWorkerThatNeverRegistersIsNamed(t *testing.T) {
	tb := newTestbed(t, 1)
	tb.sim.workerNeverShows = true
	_, err := tb.deploy()
	if err == nil || !strings.Contains(err.Error(), "worker-node-1 did not register with the controllers within") {
		t.Fatalf("Deploy = %v", err)
	}
}

func TestARequestThatCannotBeBuiltIsRefusedBeforeAnythingIsCreated(t *testing.T) {
	for name, edit := range map[string]func(*product.DeploySpec){
		"no controllers":        func(s *product.DeploySpec) { s.Nodes = 0 },
		"too many controllers":  func(s *product.DeploySpec) { s.Nodes = 6 },
		"an Enterprise edition": func(s *product.DeploySpec) { s.Edition = release.Enterprise },
		"an unknown edition":    func(s *product.DeploySpec) { s.Edition = "free" },
		"no binary":             func(s *product.DeploySpec) { s.Binary = "" },
		"a binary that is gone": func(s *product.DeploySpec) { s.Binary = "/no/such/boundary" },
		"no cluster name":       func(s *product.DeploySpec) { s.Cluster = "" },
	} {
		t.Run(name, func(t *testing.T) {
			tb := newTestbed(t, 3)
			edit(&tb.spec)
			if _, err := tb.deploy(); err == nil {
				t.Fatal("the request was accepted")
			}
			if cs, _ := tb.rt.List(context.Background(), "r1"); len(cs) != 0 {
				t.Errorf("%d containers were created for a request that was refused", len(cs))
			}
		})
	}
}

func TestLaterClustersNameTheirSupportingNodesAfterThemselves(t *testing.T) {
	tb := newTestbed(t, 1)
	tb.spec.NamePrefix, tb.spec.Cluster = "edge", "edge"
	d := tb.mustDeploy()
	for _, want := range []string{"edge-node-1", "edge-worker-node-1", "edge-lb-node-1", "edge-db-node-1", "edge-target-node-1"} {
		nodeNamed(t, d, want)
	}
}

func TestTheBinaryIsReadFromTheCache(t *testing.T) {
	tb := newTestbed(t, 1)
	d := tb.mustDeploy()
	n := nodeNamed(t, d, "node-1")
	f, _ := tb.rt.File(n.ID, binaryPath)
	if got, _ := io.ReadAll(strings.NewReader(string(f.Content))); !strings.Contains(string(got), "boundary") {
		t.Error("the installed binary is not the one from the cache")
	}
}
