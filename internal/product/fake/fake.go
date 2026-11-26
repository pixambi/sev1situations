// Package fake is a Product for tests (T-050). It builds plain containers on
// the runtime it is given, hands back fixed credentials and offers one log, so
// the engine and the UI can be tested without Vault or Boundary.
package fake

import (
	"context"
	"fmt"
	"strings"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/release"
	"github.com/pixambi/sev1situations/internal/runtime"
	"github.com/pixambi/sev1situations/internal/settings"
)

// RootToken is the credential every fake deployment hands back.
const RootToken = "fake-root-token-0000"

// Product is the fake. Set the exported fields to change what it does.
type Product struct {
	name string

	// Prims are the primitives the product adds. Their names must start with the
	// product's name.
	Prims []primitive.Primitive
	// Fields are the settings the product registers.
	Fields []settings.Field
	// DeployErr, if set, makes Deploy fail after starting its containers, as a
	// real failed deployment leaves them behind.
	DeployErr error
}

var _ product.Product = (*Product)(nil)

// New returns a fake product with the given name.
func New(name string) *Product { return &Product{name: name} }

// Name implements product.Product.
func (p *Product) Name() string { return p.name }

// Editions implements product.Product.
func (p *Product) Editions() []release.Edition {
	return []release.Edition{release.Community, release.Enterprise}
}

// BaseImage implements product.Product.
func (p *Product) BaseImage(release.Edition) string { return "docker.io/library/alpine:3" }

// Versions implements product.Product.
func (p *Product) Versions(_ context.Context, e release.Edition) ([]release.Version, error) {
	full := func(core string) string {
		if e == release.Community {
			return core
		}
		return core + "+" + string(e)
	}
	var out []release.Version
	for _, core := range []string{"2.0.0", "1.9.0", "1.8.0"} {
		out = append(out, release.Version{Product: release.Product(p.name), Edition: e, Core: core, Full: full(core), Arches: []string{"amd64", "arm64"}})
	}
	return out, nil
}

// Deploy implements product.Product.
func (p *Product) Deploy(ctx context.Context, rt runtime.Runtime, spec product.DeploySpec) (product.Deployment, error) {
	d := product.Deployment{
		Cluster: spec.Cluster, Product: p.name, Version: spec.Version,
		Creds: map[string]string{"rootToken": RootToken},
		State: map[string]string{},
	}
	for i := 1; i <= spec.Nodes; i++ {
		name := product.NodeName(spec, i)
		c, err := rt.StartContainer(ctx, runtime.ContainerOptions{
			Run: spec.Run, Name: name, Image: spec.Image, Network: spec.Network, Command: []string{"sleep", "300"},
		})
		if err != nil {
			return d, fmt.Errorf("start %s: %w", name, err)
		}
		d.Nodes = append(d.Nodes, primitive.Node{Name: name, Cluster: spec.Cluster, ContainerID: c.ID, IP: c.IP})
		if spec.Progress != nil {
			spec.Progress("started " + name)
		}
	}
	if p.DeployErr != nil {
		return d, p.DeployErr
	}
	return d, nil
}

// Describe implements product.Product.
func (p *Product) Describe(_ context.Context, _ runtime.Runtime, d product.Deployment) product.ClusterView {
	sec := product.Section{
		Title: strings.ToUpper(d.Cluster[:min(1, len(d.Cluster))]) + d.Cluster[min(1, len(d.Cluster)):] + " cluster",
		Facts: []product.Fact{
			{Label: "Version", Value: d.Version.Full},
			{Label: "Root token", Value: d.Creds["rootToken"], Secret: true},
		},
	}
	for _, n := range d.Nodes {
		sec.Nodes = append(sec.Nodes, product.NodeInfo{Name: n.Name, Details: []product.Fact{{Label: "State", Value: "unsealed"}}})
	}
	return product.ClusterView{Sections: []product.Section{sec}}
}

// LogStreams implements product.Product.
func (p *Product) LogStreams(primitive.Node) []product.LogStream {
	return []product.LogStream{{ID: "operational", Label: "Operational log"}}
}

// NodeActions implements product.Product: every action on every node.
func (p *Product) NodeActions(product.Deployment, string) []product.NodeAction {
	return []product.NodeAction{product.ActionStop, product.ActionStart, product.ActionRemove, product.ActionRevive}
}

// Revive implements product.Product: it starts a new container with the node's name.
func (p *Product) Revive(ctx context.Context, rt runtime.Runtime, spec product.DeploySpec, d product.Deployment, node string) (primitive.Node, error) {
	for _, n := range d.Nodes {
		if n.Name != node {
			continue
		}
		c, err := rt.StartContainer(ctx, runtime.ContainerOptions{
			Run: spec.Run, Name: node, Image: spec.Image, Network: spec.Network, Command: []string{"sleep", "300"},
		})
		if err != nil {
			return primitive.Node{}, fmt.Errorf("revive %s: %w", node, err)
		}
		return primitive.Node{Name: node, Cluster: n.Cluster, ContainerID: c.ID, IP: c.IP}, nil
	}
	return primitive.Node{}, fmt.Errorf("revive: there is no node %q", node)
}

// Primitives implements product.Product.
func (p *Product) Primitives() []primitive.Primitive { return p.Prims }

// SettingsFields implements product.Product.
func (p *Product) SettingsFields() []settings.Field { return p.Fields }
