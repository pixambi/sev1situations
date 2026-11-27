// Package deploy turns a scenario's topology into a running environment by
// asking each cluster's product to build it, and gives the engine the result.
// It implements the engine's Deployer and Reviver interfaces and holds no
// product knowledge of its own (R-SCOPE-2).
package deploy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/pixambi/sev1situations/internal/engine"
	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/release"
	"github.com/pixambi/sev1situations/internal/runtime"
	"github.com/pixambi/sev1situations/internal/scenario"
)

// Deployer builds environments. It is safe for use by one engine, which runs one
// scenario at a time (R-SCN-8).
type Deployer struct {
	// Products are the products a scenario may use.
	Products *product.Registry
	// Runtime builds the containers.
	Runtime runtime.Runtime
	// Cache fetches, verifies and keeps the product binaries (R-RUN-6, R-RUN-8).
	Cache *release.Cache
	// Settings gives the products their saved settings, such as a license.
	Settings product.Settings
	// Ports allocates host ports for published services. It may be nil.
	Ports *runtime.PortAllocator
	// Arch is the architecture of the Podman machine, which decides which build
	// to run: arm64 or amd64. The host's is the usual answer.
	Arch string

	mu   sync.Mutex
	runs map[string]*runState
}

var (
	_ engine.Deployer = (*Deployer)(nil)
	_ engine.Reviver  = (*Deployer)(nil)
)

// runState is what the deployer remembers about a run so it can act on it later.
type runState struct {
	clusters []*clusterState
}

// clusterState is one deployed cluster and how it was built.
type clusterState struct {
	name    string
	product product.Product
	spec    product.DeploySpec
	dep     product.Deployment
}

// Deploy implements engine.Deployer.
func (d *Deployer) Deploy(ctx context.Context, run runtime.Run, sc *scenario.Scenario, params map[string]any, progress func(string)) (engine.Deployment, error) {
	say := func(format string, args ...any) {
		if progress != nil {
			progress(fmt.Sprintf(format, args...))
		}
	}
	if len(sc.Topology.Services) > 0 {
		return engine.Deployment{}, errors.New("supporting services are not supported yet")
	}
	edition, err := editionFor(sc, params)
	if err != nil {
		return engine.Deployment{}, err
	}
	coreVersion := versionFor(sc, params)

	state := &runState{}
	d.remember(run.ID, state)

	net, err := d.Runtime.CreateNetwork(ctx, runtime.NetworkOptions{Run: run, Name: "net"})
	if err != nil {
		return engine.Deployment{}, fmt.Errorf("create the network: %w", err)
	}

	out := engine.Deployment{
		Nodes:   map[string]primitive.Node{},
		Creds:   map[string]map[string]string{},
		Actions: map[string][]engine.NodeAction{},
	}
	// A cluster that provides a seal is deployed before the clusters it unseals. Nodes keep the names their
	// position in the topology gives them, whatever order they are deployed in.
	for _, i := range sc.DeployOrder() {
		c := sc.Topology.Clusters[i]
		p, ok := d.Products.Get(c.Product)
		if !ok {
			return engine.Deployment{}, fmt.Errorf("cluster %q uses the product %q, which is not registered", c.Name, c.Product)
		}
		if c.Product != sc.Product {
			return engine.Deployment{}, fmt.Errorf("cluster %q is a %s cluster in a %s scenario: scenarios with more than one product are not supported yet", c.Name, c.Product, sc.Product)
		}

		version, err := resolveVersion(ctx, p, edition, coreVersion)
		if err != nil {
			return engine.Deployment{}, fmt.Errorf("cluster %q: %w", c.Name, err)
		}
		image := p.BaseImage(edition)
		say("making sure the image %s is available", image)
		if err := d.Runtime.EnsureImage(ctx, image, progress); err != nil {
			return engine.Deployment{}, fmt.Errorf("get the image %s: %w", image, err)
		}
		say("making sure %s %s is downloaded and verified", p.Name(), version.Full)
		binary, err := d.Cache.Ensure(ctx, version, d.Arch, downloadProgress(say, p.Name(), version.Full))
		if err != nil {
			return engine.Deployment{}, fmt.Errorf("get %s %s: %w", p.Name(), version.Full, err)
		}

		spec := product.DeploySpec{
			Run: run, Network: net.ID, Cluster: c.Name, Nodes: c.NodeCount(params), Storage: c.Storage,
			Edition: edition, Version: version, Binary: binary, Image: image,
			Settings: d.Settings, Ports: d.Ports, Progress: progress,
		}
		if i > 0 {
			spec.NamePrefix = c.Name // later clusters name their nodes <cluster>-node-N (3.6.1)
		}
		if c.Seal != nil {
			seal, err := d.sealFor(ctx, state, c)
			if err != nil {
				return engine.Deployment{}, fmt.Errorf("cluster %q: %w", c.Name, err)
			}
			spec.Seal = &seal
		}

		dep, err := p.Deploy(ctx, d.Runtime, spec)
		cs := &clusterState{name: c.Name, product: p, spec: spec, dep: dep}
		state.clusters = append(state.clusters, cs)
		if err != nil {
			return engine.Deployment{}, fmt.Errorf("deploy cluster %q: %w", c.Name, err)
		}

		out.Creds[c.Name] = dep.Creds
		for _, n := range dep.Nodes {
			out.Nodes[n.Name] = n
			out.Actions[n.Name] = p.NodeActions(dep, n.Name)
		}
	}
	return out, nil
}

// sealFor asks the cluster that provides a seal to prepare itself, and returns what the cluster it
// unseals needs to reach it. The provider is already deployed, because it comes first in the deploy order.
func (d *Deployer) sealFor(ctx context.Context, state *runState, c scenario.Cluster) (product.SealSpec, error) {
	for _, cs := range state.clusters {
		if cs.name != c.Seal.Provider {
			continue
		}
		provider, ok := cs.product.(product.SealProvider)
		if !ok {
			return product.SealSpec{}, fmt.Errorf("%s clusters cannot provide a seal, so %q cannot unseal %q", cs.product.Name(), cs.name, c.Name)
		}
		seal, err := provider.ProvideSeal(ctx, d.Runtime, cs.dep)
		if err != nil {
			return product.SealSpec{}, fmt.Errorf("prepare %q to provide the seal: %w", cs.name, err)
		}
		return seal, nil
	}
	return product.SealSpec{}, fmt.Errorf("the cluster %q that provides the seal has not been deployed", c.Seal.Provider)
}

// Revive implements engine.Reviver: it asks the node's product to recreate it.
func (d *Deployer) Revive(ctx context.Context, run runtime.Run, _ *scenario.Scenario, _ map[string]any, _ engine.Deployment, node string) (primitive.Node, error) {
	cs, err := d.clusterOf(run.ID, node)
	if err != nil {
		return primitive.Node{}, err
	}
	fresh, err := cs.product.Revive(ctx, d.Runtime, cs.spec, cs.dep, node)
	if err != nil {
		return primitive.Node{}, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	for i, n := range cs.dep.Nodes {
		if n.Name == node {
			cs.dep.Nodes[i] = fresh
		}
	}
	return fresh, nil
}

// View returns what the Cluster tab shows for a run: the sections of every cluster, in order.
func (d *Deployer) View(ctx context.Context, runID string) product.ClusterView {
	var view product.ClusterView
	for _, cs := range d.clustersOf(runID) {
		view.Sections = append(view.Sections, cs.product.Describe(ctx, d.Runtime, cs.dep).Sections...)
	}
	return view
}

// LogStreams returns the logs a node offers, from its product.
func (d *Deployer) LogStreams(runID, node string) []product.LogStream {
	cs, err := d.clusterOf(runID, node)
	if err != nil {
		return nil
	}
	for _, n := range cs.dep.Nodes {
		if n.Name == node {
			return cs.product.LogStreams(n)
		}
	}
	return nil
}

func (d *Deployer) remember(runID string, s *runState) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.runs == nil {
		d.runs = map[string]*runState{}
	}
	d.runs[runID] = s
}

func (d *Deployer) clustersOf(runID string) []*clusterState {
	d.mu.Lock()
	defer d.mu.Unlock()
	if s, ok := d.runs[runID]; ok {
		return append([]*clusterState(nil), s.clusters...)
	}
	return nil
}

func (d *Deployer) clusterOf(runID, node string) (*clusterState, error) {
	for _, cs := range d.clustersOf(runID) {
		for _, n := range cs.dep.Nodes {
			if n.Name == node {
				return cs, nil
			}
		}
	}
	return nil, fmt.Errorf("there is no node %q in run %q", node, runID)
}

// editionFor reads the edition parameter. A scenario with none uses community.
func editionFor(sc *scenario.Scenario, params map[string]any) (release.Edition, error) {
	for _, p := range sc.Parameters {
		if p.Type != scenario.ParamEdition {
			continue
		}
		switch v, _ := params[p.ID].(string); v {
		case "community", "":
			return release.Community, nil
		case "enterprise":
			return release.Enterprise, nil
		default:
			return "", fmt.Errorf("the edition %q is not supported", v)
		}
	}
	return release.Community, nil
}

// versionFor reads the version parameter, the version number without an edition suffix.
func versionFor(sc *scenario.Scenario, params map[string]any) string {
	for _, p := range sc.Parameters {
		if p.Type == scenario.ParamVersion {
			v, _ := params[p.ID].(string)
			return v
		}
	}
	return ""
}

// resolveVersion finds the release for a version number in an edition. An empty
// number means the newest, as decision D1 says.
func resolveVersion(ctx context.Context, p product.Product, edition release.Edition, core string) (release.Version, error) {
	versions, err := p.Versions(ctx, edition)
	if err != nil {
		return release.Version{}, fmt.Errorf("list the %s versions: %w", p.Name(), err)
	}
	if len(versions) == 0 {
		return release.Version{}, fmt.Errorf("no %s versions are available for the %q edition", p.Name(), edition)
	}
	if core == "" {
		return versions[0], nil
	}
	for _, v := range versions {
		if v.Core == core {
			return v, nil
		}
	}
	var have []string
	for i, v := range versions {
		if i == 5 {
			have = append(have, "...")
			break
		}
		have = append(have, v.Core)
	}
	return release.Version{}, fmt.Errorf("%s %s does not exist in the %s edition (newest: %s)", p.Name(), core, editionLabel(edition), strings.Join(have, ", "))
}

// editionLabel names an edition for a message. The community edition has no
// suffix, so its value is empty.
func editionLabel(e release.Edition) string {
	if e == release.Community {
		return "community"
	}
	return string(e)
}

// downloadProgress turns download progress into a status line at every tenth of the
// file, so a large download is visible without flooding the instructor.
func downloadProgress(say func(string, ...any), name, version string) release.Progress {
	lastTenth := -1
	return func(done, total int64) {
		if total <= 0 {
			return
		}
		tenth := int(done * 10 / total)
		if tenth != lastTenth {
			lastTenth = tenth
			say("downloading %s %s: %d%% of %d MB", name, version, tenth*10, total>>20)
		}
	}
}
