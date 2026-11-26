package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/engine"
	"github.com/pixambi/sev1situations/internal/product"
	productfake "github.com/pixambi/sev1situations/internal/product/fake"
)

func TestRenderClusterShowsFactsSecretsAndNodes(t *testing.T) {
	view := product.ClusterView{Sections: []product.Section{{
		Title: "Primary cluster",
		Facts: []product.Fact{
			{Label: "Role", Value: "primary"},
			{Label: "Root token", Value: "hvs.abc", Secret: true},
		},
		Nodes: []product.NodeInfo{
			{Name: "node-1", Details: []product.Fact{{Label: "Node ID", Value: "node-1"}, {Label: "Vault", Value: "unsealed"}, {Label: "HA role", Value: "active"}}},
			{Name: "node-2", Details: []product.Fact{{Label: "Vault", Value: "sealed"}}},
		},
	}}}
	nodes := []engine.NodeView{
		{Name: "node-1", IP: "10.89.1.2", State: engine.NodeRunning},
		{Name: "node-2", IP: "10.89.1.3", State: engine.NodeStopped},
	}

	var b strings.Builder
	renderCluster(&b, view, nodes)
	out := b.String()

	for _, want := range []string{
		"Primary cluster", "Role", "primary",
		"Root token (secret)", "hvs.abc", // the instructor sees credentials here, flagged as secret
		"node-1", "10.89.1.2", "running", "Vault: unsealed", "HA role: active",
		"node-2", "10.89.1.3", "stopped", "Vault: sealed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the Cluster tab is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Node ID") {
		t.Errorf("the node's ID is repeated beside its name:\n%s", out)
	}
}

func TestRenderClusterWithNothingPrintsNothing(t *testing.T) {
	var b strings.Builder
	renderCluster(&b, product.ClusterView{}, nil)
	if b.Len() != 0 {
		t.Errorf("output = %q", b.String())
	}
}

func TestSandboxParams(t *testing.T) {
	reg := product.NewRegistry()
	if err := reg.Register(productfake.New("vault")); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	got, err := sandboxParams(ctx, reg, sandboxChoice{Nodes: 5, Edition: "enterprise", Version: "1.2.3"})
	if err != nil || got["version"] != "1.2.3" || got["edition"] != "enterprise" || got["nodes"] != 5 {
		t.Errorf("params = %v, %v", got, err)
	}
	// No version chosen: the newest is used (D1).
	got, err = sandboxParams(ctx, reg, sandboxChoice{Nodes: 3, Edition: "community"})
	if err != nil || got["version"] != "2.0.0" {
		t.Errorf("params = %v, %v, want the newest version", got, err)
	}

	for _, tt := range []struct {
		name string
		c    sandboxChoice
		want string
	}{
		{"no nodes", sandboxChoice{Nodes: 0, Edition: "community"}, "--nodes must be from 1 to 9, not 0"},
		{"too many nodes", sandboxChoice{Nodes: 10, Edition: "community"}, "--nodes must be from 1 to 9, not 10"},
		{"an unknown edition", sandboxChoice{Nodes: 3, Edition: "gold"}, `--edition must be community or enterprise, not "gold"`},
	} {
		if _, err := sandboxParams(ctx, reg, tt.c); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: error = %v, want %q", tt.name, err, tt.want)
		}
	}
	if _, err := sandboxParams(ctx, product.NewRegistry(), sandboxChoice{Nodes: 3, Edition: "community"}); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Errorf("with no Vault product: %v", err)
	}
}

func TestTheRealSandboxScenarioLoadsAndValidates(t *testing.T) {
	products, err := DefaultProducts()
	if err != nil {
		t.Fatal(err)
	}
	sc, err := loadSandbox(Options{Builtin: os.DirFS("../..")}, products)
	if err != nil {
		t.Fatalf("loadSandbox: %v", err)
	}
	if sc.ID != "vault-sandbox" || len(sc.Steps) != 0 {
		t.Errorf("scenario = %q with %d steps, want the open-ended Vault sandbox (R-SCN-17)", sc.ID, len(sc.Steps))
	}
	if _, err := loadSandbox(Options{}, products); err == nil {
		t.Error("loadSandbox with no built-in scenarios succeeded")
	}
}

func TestSandboxCommandRefusesBadArguments(t *testing.T) {
	for _, args := range [][]string{{"sandbox", "extra"}, {"sandbox", "--nodes", "many"}, {"sandbox", "--colour", "red"}} {
		if code, _, _ := cli(t, Options{}, args...); code != exitUsage {
			t.Errorf("%v: exit %d, want %d", args, code, exitUsage)
		}
	}
}

func TestWaitForEnter(t *testing.T) {
	t.Run("returns when a line arrives", func(t *testing.T) {
		done := make(chan struct{})
		go func() { waitForEnter(context.Background(), strings.NewReader("\n")); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("it did not return after a line was typed")
		}
	})
	t.Run("returns when the context ends, as on Ctrl-C", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		pr, _ := newBlockingReader()
		done := make(chan struct{})
		go func() { waitForEnter(ctx, pr); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("it ignored the cancelled context")
		}
	})
	t.Run("with no input it waits for the context", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		waitForEnter(ctx, nil)
		if ctx.Err() == nil {
			t.Error("it returned before the context ended")
		}
	})
}

// newBlockingReader returns a reader whose Read never returns until it is closed.
func newBlockingReader() (*blockingReader, func()) {
	r := &blockingReader{release: make(chan struct{})}
	return r, func() { close(r.release) }
}

type blockingReader struct{ release chan struct{} }

func (b *blockingReader) Read([]byte) (int, error) { <-b.release; return 0, nil }
