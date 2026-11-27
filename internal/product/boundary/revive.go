package boundary

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/product/deployutil"
	"github.com/pixambi/sev1situations/internal/runtime"
)

var errNotRevivable = errors.New("a removed database cannot be revived, because its data is gone: end the scenario and start again")

// Revive implements product.Product. It recreates a removed part with its original configuration. A new container has
// a new address, so what holds the old one is brought up to date: a revived controller is added to the load balancer's
// configuration, a revived load balancer is where the worker registers, and a revived target host is where the generated
// host points.
func (p *Product) Revive(ctx context.Context, rt runtime.Runtime, spec product.DeploySpec, d product.Deployment, name string) (primitive.Node, error) {
	idx := slices.IndexFunc(d.Nodes, func(n primitive.Node) bool { return n.Name == name })
	if idx < 0 {
		return primitive.Node{}, fmt.Errorf("revive: there is no node %q in cluster %q", name, d.Cluster)
	}
	role := roleOf(d, name)
	if role == RoleDatabase {
		return primitive.Node{}, fmt.Errorf("revive %s: %w", name, errNotRevivable)
	}
	if _, err := p.check(spec); err != nil {
		return primitive.Node{}, fmt.Errorf("revive %s: %w", name, err)
	}
	alive := aliveContainers(ctx, rt, spec.Run.ID)
	if alive[d.Nodes[idx].ContainerID] {
		return primitive.Node{}, fmt.Errorf("revive %s: the node still exists. Start it instead", name)
	}

	c := &cluster{p: p, rt: rt, spec: spec, dbPassword: d.Creds["databasePassword"],
		k: keys{Root: d.Creds["kmsRoot"], WorkerAuth: d.Creds["kmsWorkerAuth"], Recovery: d.Creds["kmsRecovery"]}}
	c.poller = deployutil.Poller{Interval: p.timing.Poll, Tail: c.tail}
	c.nodes = map[string]primitive.Node{}
	for _, n := range d.Nodes {
		c.nodes[n.Name] = n
	}

	image := spec.Image
	if role == RoleLB {
		image = HAProxyImage
	}
	var ports []runtime.PortMapping
	if host := d.State["port:"+name]; host != "" && spec.Ports != nil {
		var h int
		if _, err := fmt.Sscan(host, &h); err == nil {
			ports = []runtime.PortMapping{{Host: h, Container: apiPort}} // the Cluster tab's address stays valid
		}
	}
	ct, err := rt.StartContainer(ctx, runtime.ContainerOptions{
		Run: spec.Run, Name: name, Image: image, Command: keepAlive, Network: spec.Network, Hostname: name, Ports: ports,
	})
	if err != nil {
		return primitive.Node{}, fmt.Errorf("revive %s: %w", name, err)
	}
	node := primitive.Node{Name: name, Cluster: d.Cluster, ContainerID: ct.ID, IP: ct.IP}
	if ct.IP == "" {
		return node, fmt.Errorf("revive %s: it has no address on the run's network", name)
	}
	c.nodes[name] = node

	// From here on the deployment is the one with the new node in it.
	nd := d
	nd.Nodes = slices.Clone(d.Nodes)
	nd.Nodes[idx] = node
	alive[node.ContainerID] = true

	c.say("reviving %s", name)
	switch role {
	case RoleController:
		err = c.reviveController(ctx, nd, node, alive)
	case RoleWorker:
		err = c.reviveWorker(ctx, nd, node, alive)
	case RoleLB:
		err = c.reviveLoadBalancer(ctx, nd, node, alive)
	case RoleTarget:
		err = c.reviveTarget(ctx, nd, node, alive)
	}
	if err != nil {
		return node, err
	}
	c.say("%s is back", name)
	return node, nil
}

// aliveContainers is the set of the run's containers that still exist.
func aliveContainers(ctx context.Context, rt runtime.Runtime, runID string) map[string]bool {
	out := map[string]bool{}
	if cs, err := rt.List(ctx, runID); err == nil {
		for _, c := range cs {
			out[c.ID] = true
		}
	}
	return out
}

// liveController returns the first controller that still exists, other than the named one, to run commands from.
func (c *cluster) liveController(d product.Deployment, alive map[string]bool, except string) (primitive.Node, bool) {
	for _, n := range c.controllers(&d) {
		if n.Name != except && alive[n.ContainerID] {
			return n, true
		}
	}
	return primitive.Node{}, false
}

func (c *cluster) reviveController(ctx context.Context, d product.Deployment, n primitive.Node, alive map[string]bool) error {
	db := c.nodes[roleName(c.spec, "db")]
	if err := c.install(ctx, n, controllerConfig, controllerHCL(n.Name, n.IP, dbURL(c.dbPassword, db.IP), c.k)); err != nil {
		return err
	}
	if err := c.serve(ctx, n, controllerConfig); err != nil {
		return err
	}
	if err := c.waitHealthy(ctx, n); err != nil {
		return err
	}
	// The balancer sends traffic to the controllers' addresses, and this one has a new address.
	lb := c.nodes[roleName(c.spec, "lb")]
	if alive[lb.ContainerID] {
		c.say("telling the load balancer about %s", n.Name)
		if err := c.runLoadBalancer(ctx, lb, c.aliveControllers(d, alive)); err != nil {
			return err
		}
		return c.waitThroughLoadBalancer(ctx, d, lb, n)
	}
	return nil
}

// aliveControllers are the controllers that exist, for the balancer's configuration.
func (c *cluster) aliveControllers(d product.Deployment, alive map[string]bool) []primitive.Node {
	var out []primitive.Node
	for _, n := range c.controllers(&d) {
		if alive[n.ContainerID] {
			out = append(out, n)
		}
	}
	return out
}

func (c *cluster) reviveWorker(ctx context.Context, d product.Deployment, w primitive.Node, alive map[string]bool) error {
	via, ok := c.liveController(d, alive, "")
	if !ok {
		return errors.New("no controller is running to register the worker with: revive a controller first")
	}
	return c.runWorker(ctx, d, w, c.nodes[roleName(c.spec, "lb")], via)
}

func (c *cluster) reviveLoadBalancer(ctx context.Context, d product.Deployment, lb primitive.Node, alive map[string]bool) error {
	via, ok := c.liveController(d, alive, "")
	if !ok {
		return errors.New("no controller is running behind the load balancer: revive a controller first")
	}
	if err := c.runLoadBalancer(ctx, lb, c.aliveControllers(d, alive)); err != nil {
		return err
	}
	if err := c.waitThroughLoadBalancer(ctx, d, lb, via); err != nil {
		return err
	}
	// The worker registers through the balancer, whose address has changed, so it is started again with the new one.
	w := c.nodes[roleName(c.spec, "worker")]
	if alive[w.ContainerID] {
		c.say("pointing %s at the new load balancer", w.Name)
		return c.runWorker(ctx, d, w, lb, via)
	}
	return nil
}

func (c *cluster) reviveTarget(ctx context.Context, d product.Deployment, t primitive.Node, alive map[string]bool) error {
	if err := c.runSSH(ctx, t, d.Creds["targetPassword"]); err != nil {
		return err
	}
	via, ok := c.liveController(d, alive, "")
	if !ok {
		return nil // nothing to point yet: a controller that is revived later finds the host as it was
	}
	c.say("pointing the generated host at the new address of %s", t.Name)
	return c.pointHost(ctx, d, via, t)
}
