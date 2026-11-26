package gui

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/pixambi/sev1situations/internal/engine"
	"github.com/pixambi/sev1situations/internal/product"
)

// describe is a product's description of a two-node cluster.
func describe(context.Context, string) product.ClusterView {
	return product.ClusterView{Sections: []product.Section{{
		Title: "Primary cluster",
		Facts: []product.Fact{{Label: "API address", Value: "http://127.0.0.1:8200"}, {Label: "Root token", Value: "hvs.secret", Secret: true}},
		Nodes: []product.NodeInfo{
			{Name: "node-1", Details: []product.Fact{{Label: "Sealed", Value: "no"}}},
			{Name: "node-2", Details: []product.Fact{{Label: "Sealed", Value: "no"}}},
		},
	}}}
}

func clusterOf(t *testing.T, b *backend, view func(context.Context, string) product.ClusterView) (*ClusterService, *RunService) {
	t.Helper()
	l := newLink(context.Background(), func(context.Context) (*Connection, error) {
		return &Connection{Engine: b.eng, View: view}, nil
	}, b.out.emit)
	l.ensure()
	return NewClusterService(context.Background(), l), NewRunService(context.Background(), l.source, b.catalogue, b.out.emit)
}

func TestClusterJoinsTheProductsDescriptionWithTheEnginesNodes(t *testing.T) {
	b := newBackend(t, map[string]string{"s": scenarioYAML("demo", "Demo", "vault", true)})
	cluster, runs := clusterOf(t, b, describe)
	f := form()
	f.Params = map[string]any{"nodes": float64(2)}
	if _, err := runs.Start(f); err != nil {
		t.Fatal(err)
	}

	got, err := cluster.Get()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sections) != 1 || got.Sections[0].Title != "Primary cluster" {
		t.Fatalf("sections = %+v", got.Sections)
	}
	sec := got.Sections[0]
	if !reflect.DeepEqual(sec.Facts, []FactView{{"API address", "http://127.0.0.1:8200", false}, {"Root token", "hvs.secret", true}}) {
		t.Errorf("facts = %+v", sec.Facts)
	}
	n := sec.Nodes[0]
	if n.Name != "node-1" || n.IP != "10.89.1.2" || n.State != "running" || n.Details[0].Label != "Sealed" {
		t.Errorf("node = %+v", n)
	}
	if !reflect.DeepEqual(n.Actions, []string{"stop", "remove"}) {
		t.Errorf("actions = %v, want only what fits a running node", n.Actions)
	}
}

func TestARemovedNodeStaysAsAPlaceholderThatCanBeRevived(t *testing.T) {
	b := newBackend(t, map[string]string{"s": scenarioYAML("demo", "Demo", "vault", true)})
	cluster, runs := clusterOf(t, b, describe)
	f := form()
	f.Params = map[string]any{"nodes": float64(2)}
	if _, err := runs.Start(f); err != nil {
		t.Fatal(err)
	}
	if _, err := runs.NodeAction("node-2", "remove"); err != nil {
		t.Fatal(err)
	}

	got, _ := cluster.Get()
	n := got.Sections[0].Nodes[1]
	if n.Name != "node-2" || n.State != "removed" || n.IP != "" {
		t.Fatalf("node = %+v", n)
	}
	if !strings.Contains(strings.Join(n.Actions, ","), "revive") {
		t.Errorf("actions = %v, a removed node can be revived", n.Actions)
	}
}

func TestANodeNoSectionDescribesIsStillShown(t *testing.T) {
	got := join(product.ClusterView{}, []engine.NodeView{{Name: "node-1", IP: "10.0.0.1", State: engine.NodeRunning}})
	if len(got.Sections) != 1 || got.Sections[0].Title != "Other nodes" || got.Sections[0].Nodes[0].Name != "node-1" {
		t.Errorf("got %+v", got)
	}
	if got.Sections[0].Facts == nil || got.Sections[0].Nodes[0].Details == nil || got.Sections[0].Nodes[0].Actions == nil {
		t.Error("an empty list became null, which the frontend would trip over")
	}
}

func TestClusterIsEmptyWithoutARunAndExplainsWithoutAConnection(t *testing.T) {
	b := newBackend(t, nil)
	cluster, _ := clusterOf(t, b, describe)
	got, err := cluster.Get()
	if err != nil || got.Sections == nil || len(got.Sections) != 0 {
		t.Errorf("no run: %+v, %v", got, err)
	}

	down := newLink(context.Background(), func(context.Context) (*Connection, error) { return nil, context.Canceled }, b.out.emit)
	if _, err := NewClusterService(context.Background(), down).Get(); err == nil || !strings.Contains(err.Error(), "not ready") {
		t.Errorf("no connection: %v", err)
	}
}
