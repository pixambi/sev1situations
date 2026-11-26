package vault

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/runtime"
)

// NodeActions implements product.Product. Every Vault node offers all four
// actions: which of them apply depends on the node's state (R-VIEW-6).
func (*Product) NodeActions(product.Deployment, string) []product.NodeAction {
	return []product.NodeAction{product.ActionStop, product.ActionStart, product.ActionRemove, product.ActionRevive}
}

// Revive implements product.Product. It recreates a removed node as a fresh
// container with the same name, raft node ID and published port and joins it back
// to the cluster, where it catches up from the leader. Nothing of the old node's
// data is kept, which is what a replacement node would be.
//
// The old node may still be listed as a peer. It is removed from the peer list
// first, so the new node, which has a new address, joins cleanly.
func (p *Product) Revive(ctx context.Context, rt runtime.Runtime, spec product.DeploySpec, d product.Deployment, name string) (primitive.Node, error) {
	idx := -1
	for i, n := range d.Nodes {
		if n.Name == name {
			idx = i
		}
	}
	if idx < 0 {
		return primitive.Node{}, fmt.Errorf("revive: there is no node %q in cluster %q", name, d.Cluster)
	}
	license, err := p.check(spec)
	if err != nil {
		return primitive.Node{}, fmt.Errorf("revive %s: %w", name, err)
	}
	if existing, err := rt.List(ctx, spec.Run.ID); err == nil {
		for _, c := range existing {
			if c.ID == d.Nodes[idx].ContainerID {
				return primitive.Node{}, fmt.Errorf("revive %s: the node still exists. Start it instead", name)
			}
		}
	}

	c := &cluster{p: p, rt: rt, spec: spec, license: license}
	var ports []runtime.PortMapping
	if idx < len(d.Published) {
		ports = []runtime.PortMapping{d.Published[idx]} // the Cluster tab's address stays valid
	}
	ct, err := rt.StartContainer(ctx, runtime.ContainerOptions{
		Run: spec.Run, Name: name, Image: spec.Image, Command: keepAlive,
		Network: spec.Network, Hostname: name, Ports: ports,
	})
	if err != nil {
		return primitive.Node{}, fmt.Errorf("revive %s: %w", name, err)
	}
	node := primitive.Node{Name: name, Cluster: d.Cluster, ContainerID: ct.ID, IP: ct.IP}
	if ct.IP == "" {
		return node, fmt.Errorf("revive %s: it has no address on the run's network", name)
	}

	c.say("reviving %s", name)
	if err := c.install(ctx, []primitive.Node{node}); err != nil {
		return node, err
	}
	if err := c.waitStatus(ctx, node, p.timing.NodeReady, func(status) bool { return true }, "answer"); err != nil {
		return node, err
	}
	if err := c.rejoin(ctx, d, node); err != nil {
		return node, err
	}
	c.say("unsealing %s", name)
	if err := c.unseal(ctx, node, unsealKeys(&d)); err != nil {
		return node, err
	}
	// Until autopilot has promoted it the node is only a non-voting member, so wait for it.
	for _, o := range d.Nodes {
		if o.Name != name {
			if err := c.waitVoters(ctx, o, d.Creds["rootToken"], len(d.Nodes)); err != nil {
				return node, err
			}
			break
		}
	}
	c.say("%s is back in the cluster", name)
	return node, nil
}

// rejoin brings a revived node into the cluster through any other node that
// answers, trying each in turn until one accepts it.
func (c *cluster) rejoin(ctx context.Context, d product.Deployment, node primitive.Node) error {
	token := d.Creds["rootToken"]
	var others []primitive.Node
	for _, n := range d.Nodes {
		if n.Name != node.Name {
			others = append(others, n)
		}
	}
	if len(others) == 0 {
		return errors.New("there is no other node to join: a cluster of one cannot revive its only node")
	}

	// Drop the old membership. If it was never there, or the cluster already forgot it,
	// the command fails and that is fine.
	for _, o := range others {
		res, err := c.vault(ctx, o, token, "operator", "raft", "remove-peer", node.Name)
		if err == nil && res.ExitCode == 0 {
			break
		}
	}

	c.say("joining %s to the cluster", node.Name)
	attempt := 0
	return c.poll(ctx, node, c.p.timing.Join, "join the cluster", func() (bool, string, error) {
		target := others[attempt%len(others)]
		attempt++
		res, err := c.vault(ctx, node, "", "operator", "raft", "join", fmt.Sprintf("http://%s:%d", target.IP, apiPort))
		if err != nil {
			return false, "", err
		}
		return res.ExitCode == 0, strings.TrimSpace(firstLine(res.Stderr + res.Stdout)), nil
	})
}
