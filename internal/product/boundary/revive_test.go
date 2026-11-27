package boundary

import (
	"context"
	"strings"
	"testing"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/product"
)

func (tb *testbed) remove(d product.Deployment, name string) primitive.Node {
	tb.t.Helper()
	for _, n := range d.Nodes {
		if n.Name == name {
			if err := tb.rt.Remove(context.Background(), n.ContainerID); err != nil {
				tb.t.Fatal(err)
			}
			tb.sim.mu.Lock()
			tb.sim.started[name] = false
			tb.sim.mu.Unlock()
			return n
		}
	}
	tb.t.Fatalf("no node %q", name)
	return primitive.Node{}
}

func (tb *testbed) revive(d product.Deployment, name string) (primitive.Node, error) {
	return tb.p.Revive(context.Background(), tb.rt, tb.spec, d, name)
}

// withNode returns the deployment with a revived node in place of the old one, as the deployer does.
func withNode(d product.Deployment, fresh primitive.Node) product.Deployment {
	d.Nodes = append([]primitive.Node(nil), d.Nodes...)
	for i, n := range d.Nodes {
		if n.Name == fresh.Name {
			d.Nodes[i] = fresh
		}
	}
	return d
}

func TestARevivedControllerIsBackAndTheLoadBalancerKnowsItsNewAddress(t *testing.T) {
	tb := newTestbed(t, 3)
	d := tb.mustDeploy()
	old := tb.remove(d, "node-2")

	fresh, err := tb.revive(d, "node-2")
	if err != nil {
		t.Fatalf("Revive: %v", err)
	}
	if fresh.Name != "node-2" || fresh.ContainerID == old.ContainerID || fresh.IP == "" || fresh.IP == old.IP {
		t.Errorf("revived node = %+v (was %+v), want a new container with the same name", fresh, old)
	}
	if !tb.sim.startedNode("node-2") {
		t.Error("the revived controller was not started")
	}
	cfg := fileContent(t, tb, withNode(d, fresh), "node-2", controllerConfig)
	if !strings.Contains(cfg, `public_cluster_addr = "`+fresh.IP+`:9201"`) || !strings.Contains(cfg, `name                = "node-2"`) {
		t.Errorf("the revived controller has the wrong configuration:\n%s", cfg)
	}
	// The balancer is told the new address, and no longer the old.
	haproxy := fileContent(t, tb, d, "lb-node-1", haproxyConfig)
	if !strings.Contains(haproxy, "server node-2 "+fresh.IP+":9200") || strings.Contains(haproxy, old.IP+":9200") {
		t.Errorf("the load balancer configuration is out of date:\n%s", haproxy)
	}
}

func (s *sim) startedNode(name string) bool { s.mu.Lock(); defer s.mu.Unlock(); return s.started[name] }

func TestARevivedWorkerRegistersAgain(t *testing.T) {
	tb := newTestbed(t, 1)
	d := tb.mustDeploy()
	old := tb.remove(d, "worker-node-1")
	fresh, err := tb.revive(d, "worker-node-1")
	if err != nil {
		t.Fatalf("Revive: %v", err)
	}
	if fresh.ContainerID == old.ContainerID || !tb.sim.startedNode("worker-node-1") {
		t.Errorf("revived worker = %+v", fresh)
	}
	lb := nodeNamed(t, d, "lb-node-1")
	cfg := fileContent(t, tb, withNode(d, fresh), "worker-node-1", workerConfig)
	if !strings.Contains(cfg, `public_addr       = "`+fresh.IP+`:9202"`) || !strings.Contains(cfg, `initial_upstreams = ["`+lb.IP+`:9201"]`) {
		t.Errorf("the revived worker's configuration:\n%s", cfg)
	}
}

func TestARevivedLoadBalancerIsWhereTheWorkerRegistersAndTheWorkerIsToldSo(t *testing.T) {
	tb := newTestbed(t, 2)
	d := tb.mustDeploy()
	tb.remove(d, "lb-node-1")
	before := len(tb.sim.callsOn("worker-node-1", "sh"))

	fresh, err := tb.revive(d, "lb-node-1")
	if err != nil {
		t.Fatalf("Revive: %v", err)
	}
	haproxy := fileContent(t, tb, withNode(d, fresh), "lb-node-1", haproxyConfig)
	for _, c := range []string{"node-1", "node-2"} {
		if !strings.Contains(haproxy, "server "+c+" "+nodeNamed(t, d, c).IP) {
			t.Errorf("the new load balancer does not know controller %s", c)
		}
	}
	// The worker is started again with the new address, as it would otherwise keep registering at the old one.
	if got := len(tb.sim.callsOn("worker-node-1", "sh")) - before; got != 1 {
		t.Errorf("the worker was restarted %d times, want once", got)
	}
	cfg := fileContent(t, tb, d, "worker-node-1", workerConfig)
	if !strings.Contains(cfg, `initial_upstreams = ["`+fresh.IP+`:9201"]`) {
		t.Errorf("the worker still points at the old load balancer:\n%s", cfg)
	}
}

func TestARevivedTargetHostKeepsItsPasswordAndTheHostIsPointedAtItsNewAddress(t *testing.T) {
	tb := newTestbed(t, 1)
	d := tb.mustDeploy()
	old := tb.remove(d, "target-node-1")
	before := len(tb.sim.callsOn("node-1", binaryPath, "hosts", "update"))

	fresh, err := tb.revive(d, "target-node-1")
	if err != nil {
		t.Fatalf("Revive: %v", err)
	}
	updates := tb.sim.callsOn("node-1", binaryPath, "hosts", "update")
	if len(updates) != before+1 {
		t.Fatalf("hosts update ran %d more times, want once", len(updates)-before)
	}
	if args := strings.Join(updates[len(updates)-1].argv, " "); !strings.Contains(args, "-address="+fresh.IP) || strings.Contains(args, old.IP) {
		t.Errorf("hosts update = %s", args)
	}
	// The same password goes in, so what the Cluster tab shows still works.
	var script string
	for _, c := range tb.sim.callsOn("target-node-1", "sh") {
		if last := c.argv[len(c.argv)-1]; strings.Contains(last, "chpasswd") {
			script = last // the install, which is the last one to set a password
		}
	}
	if !strings.Contains(script, "target:"+d.Creds["targetPassword"]) {
		t.Error("the revived target host was given a different password")
	}
}

func TestRevivingThingsThatCannotOrNeedNotBeRevived(t *testing.T) {
	tb := newTestbed(t, 2)
	d := tb.mustDeploy()

	if _, err := tb.revive(d, "node-1"); err == nil || !strings.Contains(err.Error(), "still exists") {
		t.Errorf("reviving a node that is there: %v", err)
	}
	if _, err := tb.revive(d, "node-9"); err == nil || !strings.Contains(err.Error(), "no node") {
		t.Errorf("an unknown node: %v", err)
	}
	tb.remove(d, "db-node-1")
	if _, err := tb.revive(d, "db-node-1"); err == nil || !strings.Contains(err.Error(), "data is gone") {
		t.Errorf("the database: %v", err)
	}
}

func TestAWorkerCannotBeRevivedWhileNoControllerIsRunning(t *testing.T) {
	tb := newTestbed(t, 1)
	d := tb.mustDeploy()
	tb.remove(d, "node-1")
	tb.remove(d, "worker-node-1")
	if _, err := tb.revive(d, "worker-node-1"); err == nil || !strings.Contains(err.Error(), "revive a controller first") {
		t.Errorf("Revive = %v", err)
	}
}
