package product_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/product/fake"
	"github.com/pixambi/sev1situations/internal/product/producttest"
	"github.com/pixambi/sev1situations/internal/release"
	"github.com/pixambi/sev1situations/internal/runtime"
	rtfake "github.com/pixambi/sev1situations/internal/runtime/fake"
	"github.com/pixambi/sev1situations/internal/settings"
)

// verb is a minimal primitive for testing registration.
type verb struct{ name string }

func (v verb) Spec() primitive.Spec {
	return primitive.Spec{Name: v.name, Summary: "A test verb.", Timeout: time.Second}
}

func (verb) Run(context.Context, primitive.Env, primitive.Args) (primitive.Result, error) {
	return primitive.Result{Status: primitive.Pass}, nil
}

func TestTheFakeProductMeetsTheContract(t *testing.T) {
	p := fake.New("demo")
	p.Prims = []primitive.Primitive{verb{"demo.ping"}}
	p.Fields = []settings.Field{{Key: "demo.license", Label: "Demo license", Secret: true}}

	producttest.Contract(t, p, producttest.Options{
		Runtime: rtfake.New(),
		Spec: func() product.DeploySpec {
			return product.DeploySpec{
				Cluster: "primary", Nodes: 3, Edition: release.Community,
				Version: release.Version{Product: "demo", Core: "2.0.0", Full: "2.0.0"},
				Image:   "alpine:3",
			}
		},
	})
}

func TestNodeNamesFollowTheTopologyRule(t *testing.T) {
	first := product.DeploySpec{}
	later := product.DeploySpec{NamePrefix: "dr-secondary"}
	if got := product.NodeName(first, 2); got != "node-2" {
		t.Errorf("first cluster node = %q, want node-2", got)
	}
	if got := product.NodeName(later, 1); got != "dr-secondary-node-1" {
		t.Errorf("later cluster node = %q, want dr-secondary-node-1 (section 3.6.1)", got)
	}
}

func TestRegistry(t *testing.T) {
	r := product.NewRegistry()
	if err := r.Register(fake.New("vault")); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(fake.New("boundary")); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.Names(), ","); got != "boundary,vault" {
		t.Errorf("Names = %q, want them sorted", got)
	}
	if p, ok := r.Get("vault"); !ok || p.Name() != "vault" {
		t.Error("Get did not find a registered product")
	}
	if _, ok := r.Get("nomad"); ok {
		t.Error("Get found a product that was never registered")
	}
	if err := r.Register(fake.New("vault")); err == nil {
		t.Error("registering a name twice succeeded")
	}
}

func TestRegisterRejectsProductsThatBreakTheRules(t *testing.T) {
	badPrim := fake.New("demo")
	badPrim.Prims = []primitive.Primitive{verb{"other.ping"}}
	badField := fake.New("demo")
	badField.Fields = []settings.Field{{Key: "license", Label: "License"}}

	tests := []struct {
		name string
		p    product.Product
		want string
	}{
		{"an upper-case name", fake.New("Vault"), "lower-case"},
		{"a name with a dash", fake.New("my-product"), "lower-case"},
		{"a primitive named after another product", badPrim, `must start with "demo."`},
		{"a setting that is not prefixed", badField, `must start with "demo."`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := product.NewRegistry().Register(tt.p)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Register = %v, want an error containing %q", err, tt.want)
			}
		})
	}
}

func TestSettingsAndPrimitivesAreGatheredFromEveryProduct(t *testing.T) {
	v := fake.New("vault")
	v.Fields = []settings.Field{{Key: "vault.license", Label: "Vault license", Secret: true}}
	v.Prims = []primitive.Primitive{verb{"vault.status"}}
	b := fake.New("boundary")
	b.Fields = []settings.Field{{Key: "boundary.license", Label: "Boundary license", Secret: true}}
	b.Prims = []primitive.Primitive{verb{"boundary.cli"}}

	r := product.NewRegistry()
	for _, p := range []product.Product{v, b} {
		if err := r.Register(p); err != nil {
			t.Fatal(err)
		}
	}

	// Adding a product's settings means registering it, never editing the page (R-SET-4).
	store, err := settings.Open(t.TempDir(), r.SettingsFields()...)
	if err != nil {
		t.Fatal(err)
	}
	view := store.View()
	if len(view.Fields) != 2 || view.Fields[0].Key != "boundary.license" || view.Fields[1].Key != "vault.license" {
		t.Errorf("settings fields = %+v, want both products' licenses", view.Fields)
	}

	prims := primitive.NewRegistry()
	if err := r.RegisterPrimitives(prims); err != nil {
		t.Fatal(err)
	}
	if !prims.Has("vault.status") || !prims.Has("boundary.cli") {
		t.Errorf("primitives = %v, want both products' verbs", prims.Names())
	}
	if err := r.RegisterPrimitives(prims); err == nil {
		t.Error("registering the same primitives twice succeeded")
	}
}

func TestTheDeploySpecHoldsWhatAProductNeeds(t *testing.T) {
	// Deploying through the interface works for a product the engine knows only by its type.
	var p product.Product = fake.New("demo")
	rt := rtfake.New()
	spec := product.DeploySpec{
		Run: runtime.Run{ID: "r1"}, Cluster: "primary", Nodes: 2, Image: "alpine:3",
		Progress: func(string) {},
	}
	d, err := p.Deploy(context.Background(), rt, spec)
	if err != nil || len(d.Nodes) != 2 || d.Nodes[1].Name != "node-2" {
		t.Errorf("Deploy = %+v, %v", d, err)
	}
}
