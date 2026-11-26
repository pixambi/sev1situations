package gui

import (
	"context"
	"time"

	"github.com/pixambi/sev1situations/internal/engine"
	"github.com/pixambi/sev1situations/internal/product"
)

// describeTimeout bounds how long the Cluster tab waits for a product to describe
// its nodes, so one slow node cannot freeze the tab.
const describeTimeout = 15 * time.Second

// ClusterService feeds the Cluster tab (3.4). The product describes what it
// deployed, the engine reports each node's state and what can be done to it, and
// this joins the two. The frontend knows nothing about any product (R-VIEW-4).
type ClusterService struct {
	link *link
	ctx  context.Context
}

// NewClusterService returns a service over the connection.
func NewClusterService(ctx context.Context, l *link) *ClusterService {
	return &ClusterService{link: l, ctx: ctx}
}

// FactView is one labelled value. A secret is shown to the instructor on this tab,
// masked until revealed, and is never written to a report (R-VIEW-5).
type FactView struct {
	Label  string
	Value  string
	Secret bool
}

// NodeCard is one node as the Cluster tab shows it.
type NodeCard struct {
	Name string
	// IP is empty for a removed node.
	IP string
	// State is running, stopped or removed.
	State string
	// Details are the product's own facts about the node.
	Details []FactView
	// Actions are what can be done to the node right now.
	Actions []string
}

// SectionView is one group on the tab.
type SectionView struct {
	Title string
	Facts []FactView
	Nodes []NodeCard
}

// ClusterState is the Cluster tab's content.
type ClusterState struct {
	Sections []SectionView
}

// Get describes the current run's deployment. With no run it returns no sections.
func (s *ClusterService) Get() (ClusterState, error) {
	conn := s.link.connection()
	if conn == nil {
		return ClusterState{Sections: []SectionView{}}, notice("The container runtime is not ready. Check Podman, then press Retry.")
	}
	snap := conn.Engine.Snapshot()
	if snap.Status != engine.StatusActive && snap.Status != engine.StatusFinished {
		return ClusterState{Sections: []SectionView{}}, nil
	}
	if conn.View == nil {
		return ClusterState{Sections: []SectionView{}}, notice("This build cannot describe a deployment.")
	}

	ctx, cancel := context.WithTimeout(s.ctx, describeTimeout)
	defer cancel()
	return join(conn.View(ctx, snap.RunID), snap.Nodes), nil
}

// join merges a product's description with the engine's view of the nodes. A node
// the engine knows and no section describes is kept, in a last section, so a node
// never disappears from the tab.
func join(view product.ClusterView, nodes []engine.NodeView) ClusterState {
	byName := map[string]engine.NodeView{}
	for _, n := range nodes {
		byName[n.Name] = n
	}
	shown := map[string]bool{}

	out := ClusterState{Sections: []SectionView{}}
	for _, sec := range view.Sections {
		sv := SectionView{Title: sec.Title, Facts: facts(sec.Facts), Nodes: []NodeCard{}}
		for _, ni := range sec.Nodes {
			shown[ni.Name] = true
			sv.Nodes = append(sv.Nodes, card(ni.Name, facts(ni.Details), byName))
		}
		out.Sections = append(out.Sections, sv)
	}

	var rest []NodeCard
	for _, n := range nodes {
		if !shown[n.Name] {
			rest = append(rest, card(n.Name, []FactView{}, byName))
		}
	}
	if len(rest) > 0 {
		out.Sections = append(out.Sections, SectionView{Title: "Other nodes", Facts: []FactView{}, Nodes: rest})
	}
	return out
}

func card(name string, details []FactView, byName map[string]engine.NodeView) NodeCard {
	c := NodeCard{Name: name, Details: details, State: string(engine.NodeRemoved), Actions: []string{}}
	if n, ok := byName[name]; ok {
		c.IP, c.State = n.IP, string(n.State)
		for _, a := range n.Actions {
			c.Actions = append(c.Actions, string(a))
		}
	}
	return c
}

func facts(in []product.Fact) []FactView {
	out := make([]FactView, 0, len(in))
	for _, f := range in {
		out = append(out, FactView{Label: f.Label, Value: f.Value, Secret: f.Secret})
	}
	return out
}
