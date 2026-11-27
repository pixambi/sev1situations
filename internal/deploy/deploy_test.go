package deploy_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/pixambi/sev1situations/internal/deploy"
	"github.com/pixambi/sev1situations/internal/engine"
	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/product"
	productfake "github.com/pixambi/sev1situations/internal/product/fake"
	"github.com/pixambi/sev1situations/internal/release"
	"github.com/pixambi/sev1situations/internal/runtime"
	rtfake "github.com/pixambi/sev1situations/internal/runtime/fake"
	"github.com/pixambi/sev1situations/internal/scenario"
)

const sandboxYAML = `id: vault-sandbox
name: Vault Sandbox
summary: A Vault cluster to explore.
product: vault
editions: [community, enterprise]
difficulty: beginner
estimatedMinutes: 60
tags: [vault]
objectives: [Explore Vault]
prerequisites: [None]
parameters:
  - id: version
    type: version
    product: vault
    label: Vault version
  - id: edition
    type: edition
    default: community
    label: Edition
  - id: nodes
    type: int
    label: Node count
    default: 3
    min: 1
    max: 9
topology:
  clusters:
    - name: primary
      product: vault
      nodes: 1
      nodesFrom: nodes
      storage: raft
initialState: A running cluster.
references:
  - title: Vault docs
    url: https://developer.hashicorp.com/vault
`

const twoClusterYAML = `id: two
name: Two
summary: Two clusters.
product: vault
editions: [community]
difficulty: beginner
estimatedMinutes: 5
tags: [vault]
objectives: [x]
prerequisites: [None]
parameters:
  - id: version
    type: version
    product: vault
    label: Vault version
topology:
  clusters:
    - name: primary
      product: vault
      nodes: 2
    - name: dr
      product: vault
      nodes: 1
initialState: Two clusters.
references:
  - title: Docs
    url: https://example.com
`

func load(t *testing.T, yaml string) *scenario.Scenario {
	t.Helper()
	sc, err := scenario.Load(fstest.MapFS{"s/scenario.yaml": {Data: []byte(yaml)}}, "s")
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

// releaseSite serves a fake release site for the product "vault" with versions
// 2.0.0, 1.9.0 and 2.0.0+ent for arm64, and counts the archive downloads.
type releaseSite struct {
	srv       *httptest.Server
	downloads atomic.Int32
}

func newSite(t *testing.T) *releaseSite {
	t.Helper()
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, _ := zw.Create("vault")
	_, _ = w.Write([]byte("#!/bin/sh\necho vault\n"))
	_ = zw.Close()
	sum := sha256.Sum256(zbuf.Bytes())

	s := &releaseSite{}
	fulls := []string{"2.0.0", "1.9.0", "2.0.0+ent"}
	mux := http.NewServeMux()
	mux.HandleFunc("/vault/index.json", func(w http.ResponseWriter, _ *http.Request) {
		var parts []string
		for _, f := range fulls {
			name := fmt.Sprintf("vault_%s_linux_arm64.zip", f)
			parts = append(parts, fmt.Sprintf(`%q:{"shasums":"SUMS","builds":[{"os":"linux","arch":"arm64","filename":%q,"url":"%s/files/%s"}]}`, f, name, s.srv.URL, name))
		}
		fmt.Fprintf(w, `{"versions":{%s}}`, strings.Join(parts, ","))
	})
	for _, f := range fulls {
		f := f
		name := fmt.Sprintf("vault_%s_linux_arm64.zip", f)
		mux.HandleFunc("/vault/"+f+"/SUMS", func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), name)
		})
		mux.HandleFunc("/files/"+name, func(w http.ResponseWriter, _ *http.Request) {
			s.downloads.Add(1)
			_, _ = w.Write(zbuf.Bytes())
		})
	}
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

type settingsStub struct{}

func (settingsStub) Get(string) (string, bool, error) { return "LICENSE", true, nil }

type bed struct {
	t    *testing.T
	d    *deploy.Deployer
	rt   *rtfake.Runtime
	prod *productfake.Product
	site *releaseSite
}

func newBed(t *testing.T) *bed {
	t.Helper()
	site := newSite(t)
	rt := rtfake.New()
	prod := productfake.New("vault")
	products := product.NewRegistry()
	if err := products.Register(prod); err != nil {
		t.Fatal(err)
	}
	return &bed{
		t: t, rt: rt, prod: prod, site: site,
		d: &deploy.Deployer{
			Products: products, Runtime: rt, Arch: "arm64", Settings: settingsStub{},
			Cache: release.NewCache(t.TempDir(), release.NewClientFor(site.srv.URL, site.srv.Client())),
			Ports: runtime.NewPortAllocator(),
		},
	}
}

var run1 = runtime.Run{ID: "r1", Scenario: "vault-sandbox"}

func (b *bed) deploy(sc *scenario.Scenario, params map[string]any) (engine.Deployment, []string, error) {
	var lines []string
	dep, err := b.d.Deploy(context.Background(), run1, sc, params, func(s string) { lines = append(lines, s) })
	return dep, lines, err
}

func TestDeployBuildsTheEnvironmentTheScenarioDescribes(t *testing.T) {
	b := newBed(t)
	dep, lines, err := b.deploy(load(t, sandboxYAML), map[string]any{"version": "2.0.0", "edition": "community", "nodes": 4})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	if len(dep.Nodes) != 4 || dep.Nodes["node-1"].Cluster != "primary" || dep.Nodes["node-4"].ContainerID == "" {
		t.Errorf("nodes = %+v, want four, taking the count from the nodes parameter", dep.Nodes)
	}
	if dep.Creds["primary"]["rootToken"] != productfake.RootToken {
		t.Errorf("creds = %v, want the cluster's credentials keyed by its name", dep.Creds)
	}
	for name := range dep.Nodes {
		if got := dep.Actions[name]; len(got) != 4 {
			t.Errorf("%s offers %v, want the product's declared actions", name, got)
		}
	}

	spec := b.prod.Specs[0]
	if spec.Cluster != "primary" || spec.NamePrefix != "" || spec.Nodes != 4 || spec.Storage != "raft" || spec.Edition != release.Community || spec.Version.Full != "2.0.0" {
		t.Errorf("spec = %+v", spec)
	}
	if spec.Image != "docker.io/library/alpine:3" || spec.Binary == "" || spec.Network == "" || spec.Run != run1 || spec.Ports == nil || spec.Settings == nil {
		t.Errorf("spec = %+v, want the image, the verified binary, the network, the run, ports and settings", spec)
	}
	if got := b.rt.Pulled(); len(got) != 1 || got[0] != "docker.io/library/alpine:3" {
		t.Errorf("pulled = %v, want the base image made available", got)
	}
	if b.site.downloads.Load() != 1 {
		t.Errorf("downloads = %d, want the binary fetched once", b.site.downloads.Load())
	}

	joined := strings.Join(lines, "\n")
	for _, want := range []string{"making sure the image", "making sure vault 2.0.0 is downloaded and verified", "downloading vault 2.0.0: 100%", "started node-4"} {
		if !strings.Contains(joined, want) {
			t.Errorf("progress is missing %q:\n%s", want, joined)
		}
	}
}

func TestADefaultNodeCountAndNewestVersion(t *testing.T) {
	b := newBed(t)
	// The engine resolves defaults first; the deployer also copes with a missing version, which means the newest (D1).
	dep, _, err := b.deploy(load(t, sandboxYAML), map[string]any{"edition": "community", "nodes": 3})
	if err != nil || len(dep.Nodes) != 3 {
		t.Fatalf("Deploy = %d nodes, %v", len(dep.Nodes), err)
	}
	if got := b.prod.Specs[0].Version.Full; got != "2.0.0" {
		t.Errorf("version = %q, want the newest when none is chosen", got)
	}
}

func TestEnterpriseGetsItsEditionAndTheSettings(t *testing.T) {
	b := newBed(t)
	_, _, err := b.deploy(load(t, sandboxYAML), map[string]any{"version": "2.0.0", "edition": "enterprise", "nodes": 1})
	if err != nil {
		t.Fatal(err)
	}
	spec := b.prod.Specs[0]
	if spec.Edition != release.Enterprise || spec.Version.Full != "2.0.0+ent" {
		t.Errorf("edition = %q version = %q, want the Enterprise build", spec.Edition, spec.Version.Full)
	}
	if v, ok, _ := spec.Settings.Get("vault.license"); !ok || v != "LICENSE" {
		t.Error("the product was not given the saved settings, so it cannot find the license")
	}
}

func TestDeployFailsClearlyWhenWhatWasAskedForDoesNotExist(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]any
		want   string
	}{
		{"a version that does not exist", map[string]any{"version": "9.9.9", "edition": "community", "nodes": 1}, "vault 9.9.9 does not exist in the community edition (newest: 2.0.0, 1.9.0, 1.8.0)"},
		{"an edition that is not supported", map[string]any{"version": "2.0.0", "edition": "platinum", "nodes": 1}, `the edition "platinum" is not supported`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newBed(t)
			_, _, err := b.deploy(load(t, sandboxYAML), tt.params)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Deploy = %v, want an error containing %q", err, tt.want)
			}
			if len(b.prod.Specs) != 0 {
				t.Error("the product was asked to deploy something that cannot exist")
			}
		})
	}
}

func TestDeployRefusesWhatItCannotBuildYet(t *testing.T) {
	b := newBed(t)
	withService := strings.Replace(sandboxYAML, "initialState:", "  services:\n    - name: postgres\n      type: postgres\ninitialState:", 1)
	if _, _, err := b.deploy(load(t, withService), map[string]any{"version": "2.0.0", "edition": "community", "nodes": 1}); err == nil || !strings.Contains(err.Error(), "supporting services are not supported yet") {
		t.Errorf("Deploy with a service = %v", err)
	}

	// A cluster of a product that is not registered.
	other := strings.Replace(twoClusterYAML, "- name: dr\n      product: vault", "- name: dr\n      product: boundary", 1)
	if _, _, err := b.deploy(load(t, other), map[string]any{"version": "2.0.0"}); err == nil || !strings.Contains(err.Error(), `"boundary", which is not registered`) {
		t.Errorf("Deploy with an unregistered product = %v", err)
	}
}

func TestLaterClustersNameTheirNodesAfterThemselves(t *testing.T) {
	b := newBed(t)
	dep, _, err := b.deploy(load(t, twoClusterYAML), map[string]any{"version": "2.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"node-1", "node-2", "dr-node-1"} {
		if _, ok := dep.Nodes[name]; !ok {
			t.Errorf("nodes = %v, want %s (section 3.6.1)", dep.Nodes, name)
		}
	}
	if len(dep.Creds) != 2 || dep.Creds["dr"]["rootToken"] == "" || dep.Creds["primary"]["rootToken"] == "" {
		t.Errorf("creds = %v, want each cluster's credentials under its own name", dep.Creds)
	}
	if b.prod.Specs[0].NamePrefix != "" || b.prod.Specs[1].NamePrefix != "dr" {
		t.Errorf("prefixes = %q, %q", b.prod.Specs[0].NamePrefix, b.prod.Specs[1].NamePrefix)
	}
	if b.site.downloads.Load() != 1 {
		t.Errorf("downloads = %d, want one binary shared by both clusters", b.site.downloads.Load())
	}
}

func TestAFailedClusterDeployReportsAndKeepsWhatWasBuilt(t *testing.T) {
	b := newBed(t)
	b.prod.DeployErr = errors.New("vault never became ready")
	_, _, err := b.deploy(load(t, sandboxYAML), map[string]any{"version": "2.0.0", "edition": "community", "nodes": 2})
	if err == nil || !strings.Contains(err.Error(), `deploy cluster "primary": vault never became ready`) {
		t.Fatalf("Deploy = %v", err)
	}
	cs, _ := b.rt.List(context.Background(), "r1")
	if len(cs) != 2 {
		t.Errorf("containers = %d, want the failed deployment left in place so its logs can be read", len(cs))
	}
}

func TestReviveGoesToTheNodesProduct(t *testing.T) {
	b := newBed(t)
	dep, _, err := b.deploy(load(t, sandboxYAML), map[string]any{"version": "2.0.0", "edition": "community", "nodes": 3})
	if err != nil {
		t.Fatal(err)
	}
	victim := dep.Nodes["node-3"]
	if err := b.rt.Remove(context.Background(), victim.ContainerID); err != nil {
		t.Fatal(err)
	}

	fresh, err := b.d.Revive(context.Background(), run1, nil, nil, dep, "node-3")
	if err != nil || fresh.Name != "node-3" || fresh.ContainerID == victim.ContainerID {
		t.Fatalf("Revive = %+v, %v", fresh, err)
	}
	if len(b.prod.Revived) != 1 || b.prod.Revived[0] != "node-3" {
		t.Errorf("revived = %v", b.prod.Revived)
	}

	// The deployer remembers the new container, so the Cluster tab describes it.
	view := b.d.View(context.Background(), "r1")
	if len(view.Sections) != 1 || len(view.Sections[0].Nodes) != 3 {
		t.Errorf("view = %+v", view)
	}

	if _, err := b.d.Revive(context.Background(), run1, nil, nil, dep, "node-9"); err == nil || !strings.Contains(err.Error(), `no node "node-9"`) {
		t.Errorf("Revive of an unknown node = %v", err)
	}
	if _, err := b.d.Revive(context.Background(), runtime.Run{ID: "other"}, nil, nil, dep, "node-1"); err == nil {
		t.Error("Revive for a run the deployer does not know succeeded")
	}
}

func TestViewAndLogStreams(t *testing.T) {
	b := newBed(t)
	if _, _, err := b.deploy(load(t, twoClusterYAML), map[string]any{"version": "2.0.0"}); err != nil {
		t.Fatal(err)
	}
	view := b.d.View(context.Background(), "r1")
	if len(view.Sections) != 2 || view.Sections[0].Title != "Primary cluster" || view.Sections[1].Title != "Dr cluster" {
		t.Errorf("view = %+v, want a section per cluster in order", view)
	}
	if streams := b.d.LogStreams("r1", "dr-node-1"); len(streams) == 0 {
		t.Error("a node offers no log streams")
	}
	if streams := b.d.LogStreams("r1", "nope"); streams != nil {
		t.Errorf("an unknown node offers %v", streams)
	}
	if view := b.d.View(context.Background(), "unknown-run"); len(view.Sections) != 0 {
		t.Errorf("an unknown run has a view: %+v", view)
	}
}

// TestTheEngineRunsTheSandboxThroughTheDeployer is the whole chain with the fake
// product: the engine starts the scenario, the deployer builds it, the instructor
// removes a node and revives it, and ending the run removes everything.
func TestTheEngineRunsTheSandboxThroughTheDeployer(t *testing.T) {
	b := newBed(t)
	eng := engine.New(engine.Deps{Registry: primitive.NewRegistry(), Deployer: b.d, Runtime: b.rt})
	ctx := context.Background()

	err := eng.Start(ctx, load(t, sandboxYAML), engine.StartInput{
		Instructor: "Ada", Trainee: "Grace", Params: map[string]any{"version": "2.0.0", "edition": "community", "nodes": 3},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	snap := eng.Snapshot()
	if snap.Status != engine.StatusActive || len(snap.Nodes) != 3 || len(snap.Steps) != 0 {
		t.Fatalf("snapshot = %+v, want a running sandbox with three nodes and no steps", snap)
	}
	if got := snap.Nodes[0].Actions; len(got) != 2 {
		t.Errorf("node-1 offers %v, want stop and remove while it runs", got)
	}

	if err := eng.NodeAction(ctx, "node-3", engine.ActionRemove); err != nil {
		t.Fatal(err)
	}
	if n := eng.Snapshot().Nodes[2]; n.State != engine.NodeRemoved || len(n.Actions) != 1 || n.Actions[0] != engine.ActionRevive {
		t.Errorf("node-3 = %+v, want a removed placeholder that offers revive", n)
	}
	if err := eng.NodeAction(ctx, "node-3", engine.ActionRevive); err != nil {
		t.Fatalf("revive through the engine: %v", err)
	}
	if n := eng.Snapshot().Nodes[2]; n.State != engine.NodeRunning {
		t.Errorf("node-3 = %+v, want it running again", n)
	}

	final, err := eng.End(ctx)
	if err != nil || final.Result != "completed" {
		t.Fatalf("End = %q, %v", final.Result, err)
	}
	if cs, _ := b.rt.List(ctx, final.RunID); len(cs) != 0 {
		t.Errorf("%d containers left after End", len(cs))
	}
}

// sealedYAML is the two-cluster scenario with the first cluster unsealed by the second, which is listed last.
var sealedYAML = strings.Replace(twoClusterYAML, "    - name: primary\n      product: vault\n      nodes: 2\n",
	"    - name: primary\n      product: vault\n      nodes: 2\n      seal:\n        type: transit\n        provider: dr\n", 1)

func TestAProviderIsDeployedFirstAndGivesTheClusterItUnsealsItsSeal(t *testing.T) {
	b := newBed(t)
	dep, lines, err := b.deploy(load(t, sealedYAML), map[string]any{"version": "2.0.0"})
	if err != nil {
		t.Fatal(err)
	}

	// The provider, listed last, was deployed first, and prepared before the other began.
	if len(b.prod.Specs) != 2 || b.prod.Specs[0].Cluster != "dr" || b.prod.Specs[1].Cluster != "primary" {
		t.Fatalf("deploy order = %+v, want dr then primary", clusterNames(b.prod.Specs))
	}
	if len(b.prod.SealFor) != 1 || b.prod.SealFor[0].Cluster != "dr" {
		t.Fatalf("ProvideSeal was asked about %+v", b.prod.SealFor)
	}
	if b.prod.Specs[0].Seal != nil {
		t.Error("the provider was itself given a seal")
	}
	seal := b.prod.Specs[1].Seal
	if seal == nil || seal.Type != "transit" || seal.Transit.Token != "hvs.seal-token" || !strings.HasPrefix(seal.Transit.Address, "http://") {
		t.Errorf("the sealed cluster was given %+v", seal)
	}

	// Nodes are still named by their place in the topology, not by when they were deployed.
	for _, name := range []string{"node-1", "node-2", "dr-node-1"} {
		if _, ok := dep.Nodes[name]; !ok {
			t.Errorf("nodes = %v, want %s", dep.Nodes, name)
		}
	}
	if b.prod.Specs[0].NamePrefix != "dr" || b.prod.Specs[1].NamePrefix != "" {
		t.Errorf("prefixes = %q (dr), %q (primary)", b.prod.Specs[0].NamePrefix, b.prod.Specs[1].NamePrefix)
	}
	if len(lines) == 0 {
		t.Error("no progress was reported")
	}
}

func clusterNames(specs []product.DeploySpec) []string {
	var out []string
	for _, s := range specs {
		out = append(out, s.Cluster)
	}
	return out
}

func TestASealThatCannotBeProvidedStopsTheDeploymentWithAClearReason(t *testing.T) {
	b := newBed(t)
	b.prod.SealErr = errors.New("transit would not enable")
	_, _, err := b.deploy(load(t, sealedYAML), map[string]any{"version": "2.0.0"})
	if err == nil || !strings.Contains(err.Error(), `cluster "primary"`) || !strings.Contains(err.Error(), `prepare "dr" to provide the seal: transit would not enable`) {
		t.Errorf("Deploy = %v", err)
	}
	if len(b.prod.Specs) != 1 {
		t.Errorf("%d clusters were deployed, want only the provider before the failure", len(b.prod.Specs))
	}
}

// noSeal is a product that cannot provide a seal: embedding the interface hides the fake's ProvideSeal.
type noSeal struct{ product.Product }

func TestAProductThatCannotProvideASealIsRefused(t *testing.T) {
	b := newBed(t)
	products := product.NewRegistry()
	if err := products.Register(noSeal{b.prod}); err != nil {
		t.Fatal(err)
	}
	b.d.Products = products
	_, _, err := b.deploy(load(t, sealedYAML), map[string]any{"version": "2.0.0"})
	if err == nil || !strings.Contains(err.Error(), "cannot provide a seal") {
		t.Errorf("Deploy = %v", err)
	}
}
