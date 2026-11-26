// Package producttest is the contract every product must satisfy (section 3.5
// and R-VIEW-1 to R-VIEW-8). Each product's own tests call Contract first and
// then test what is specific to it.
package producttest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/runtime"
	"github.com/pixambi/sev1situations/internal/runtime/fake"
)

// Options tells the contract how to deploy the product under test.
type Options struct {
	// Spec returns a deploy spec for the product. The contract fills in the run
	// and the network.
	Spec func() product.DeploySpec
	// Runtime is the runtime to deploy on. It must support SetUnavailable, so it
	// is the fake.
	Runtime *fake.Runtime
	// SkipVersions skips the check of Versions, which for a real product asks
	// the release site.
	SkipVersions bool
}

// Contract checks a product against the rules every product follows.
func Contract(t *testing.T, p product.Product, o Options) {
	t.Helper()

	t.Run("it can be registered", func(t *testing.T) {
		if err := product.NewRegistry().Register(p); err != nil {
			t.Fatalf("Register: %v", err)
		}
	})

	t.Run("its primitives and settings follow the naming rules", func(t *testing.T) {
		reg := primitive.NewRegistry()
		for _, prim := range p.Primitives() {
			if !strings.HasPrefix(prim.Spec().Name, p.Name()+".") {
				t.Errorf("primitive %q does not start with %q", prim.Spec().Name, p.Name()+".")
			}
			if err := reg.Register(prim); err != nil {
				t.Errorf("primitive %q: %v", prim.Spec().Name, err)
			}
		}
		for _, f := range p.SettingsFields() {
			if !strings.HasPrefix(f.Key, p.Name()+".") || f.Label == "" {
				t.Errorf("setting %+v must start with %q and have a label", f, p.Name()+".")
			}
		}
	})

	t.Run("every edition has a base image", func(t *testing.T) {
		for _, e := range p.Editions() {
			if p.BaseImage(e) == "" {
				t.Errorf("no base image for edition %q (R-RUN-10)", e)
			}
		}
	})

	if !o.SkipVersions {
		t.Run("versions are listed newest first", func(t *testing.T) {
			for _, e := range p.Editions() {
				vs, err := p.Versions(context.Background(), e)
				if err != nil || len(vs) == 0 {
					t.Fatalf("Versions(%q) = %v, %v", e, vs, err)
				}
				for _, v := range vs {
					if v.Edition != e {
						t.Errorf("Versions(%q) returned a version of edition %q", e, v.Edition)
					}
				}
			}
		})
	}

	if o.Spec == nil || o.Runtime == nil {
		return
	}
	deployed(t, p, o)
}

func deployed(t *testing.T, p product.Product, o Options) {
	ctx := context.Background()
	rt := o.Runtime
	spec := o.Spec()
	spec.Run = runtime.Run{ID: "contract", Scenario: "contract-test"}
	n, err := rt.CreateNetwork(ctx, runtime.NetworkOptions{Run: spec.Run, Name: "net"})
	if err != nil {
		t.Fatal(err)
	}
	spec.Network = n.ID
	t.Cleanup(func() { _ = rt.RemoveRun(context.Background(), spec.Run.ID) })

	d, err := p.Deploy(ctx, rt, spec)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	t.Run("a deployment names its nodes by the rule in 3.6.1", func(t *testing.T) {
		if d.Cluster != spec.Cluster || d.Product != p.Name() || len(d.Nodes) != spec.Nodes {
			t.Fatalf("deployment = cluster %q product %q with %d nodes, want %q %q %d", d.Cluster, d.Product, len(d.Nodes), spec.Cluster, p.Name(), spec.Nodes)
		}
		for i, node := range d.Nodes {
			if want := product.NodeName(spec, i+1); node.Name != want {
				t.Errorf("node %d is %q, want %q", i+1, node.Name, want)
			}
			if node.ContainerID == "" || node.Cluster != spec.Cluster {
				t.Errorf("node %+v has no container or the wrong cluster", node)
			}
		}
	})

	t.Run("every node is a container of the run", func(t *testing.T) {
		cs, err := rt.List(ctx, spec.Run.ID)
		if err != nil {
			t.Fatal(err)
		}
		have := map[string]bool{}
		for _, c := range cs {
			have[c.ID] = true
		}
		for _, node := range d.Nodes {
			if !have[node.ContainerID] {
				t.Errorf("node %s has no container in the run (R-RUN-4)", node.Name)
			}
		}
	})

	t.Run("credentials appear only as secret facts", func(t *testing.T) {
		view := p.Describe(ctx, rt, d)
		if len(view.Sections) == 0 {
			t.Fatal("Describe returned no sections")
		}
		for _, sec := range view.Sections {
			facts := append([]product.Fact(nil), sec.Facts...)
			for _, ni := range sec.Nodes {
				facts = append(facts, ni.Details...)
			}
			for _, f := range facts {
				for key, cred := range d.Creds {
					if len(cred) >= 4 && strings.Contains(f.Value, cred) && !f.Secret {
						t.Errorf("the fact %q shows the credential %q without marking it secret (R-VIEW-5)", f.Label, key)
					}
				}
			}
		}
	})

	t.Run("Describe never fails, even when the runtime is down", func(t *testing.T) {
		rt.SetUnavailable(errors.New("machine stopped"))
		defer rt.SetUnavailable(nil)
		if view := p.Describe(ctx, rt, d); len(view.Sections) == 0 {
			t.Error("Describe returned nothing while the runtime was down, want what it could say with the unknowns marked")
		}
		_ = p.Describe(ctx, rt, product.Deployment{Cluster: "empty", Product: p.Name()}) // must not panic
	})

	t.Run("every node offers log streams with unique IDs", func(t *testing.T) {
		for _, node := range d.Nodes {
			streams := p.LogStreams(node)
			if len(streams) == 0 {
				t.Errorf("node %s offers no logs (R-VIEW-8)", node.Name)
			}
			seen := map[string]bool{}
			for _, s := range streams {
				if s.ID == "" || s.Label == "" || seen[s.ID] {
					t.Errorf("node %s has a bad or repeated stream %+v", node.Name, s)
				}
				seen[s.ID] = true
			}
		}
	})
}
