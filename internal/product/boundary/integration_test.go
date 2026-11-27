//go:build integration

package boundary

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/release"
	"github.com/pixambi/sev1situations/internal/runtime"
	"github.com/pixambi/sev1situations/internal/runtime/podman"
)

// These tests need a running Podman machine and network access the first time, to download Boundary and the
// images. The binary is cached in the user cache directory so later runs need no download.

func realBinary(t *testing.T) (string, release.Version) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	base, err := os.UserCacheDir()
	if err != nil {
		t.Skipf("no user cache directory: %v", err)
	}
	client := release.NewClient()
	versions, err := client.Versions(ctx, release.Boundary, release.Community)
	if err != nil || len(versions) == 0 {
		t.Skipf("cannot list Boundary versions (offline?): %v", err)
	}
	v := versions[0]
	path, err := release.NewCache(filepath.Join(base, "sev1situations-integration"), client).Ensure(ctx, v, goruntime.GOARCH, func(done, total int64) {})
	if err != nil {
		t.Skipf("cannot fetch Boundary %s: %v", v.Full, err)
	}
	return path, v
}

// real is a deployed Boundary on a real Podman machine.
type real struct {
	t    *testing.T
	ctx  context.Context
	rt   *podman.Runtime
	p    *Product
	spec product.DeploySpec
	d    product.Deployment
}

func deployReal(t *testing.T, timeout time.Duration) *real {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	rt, err := podman.New(ctx)
	if err != nil {
		t.Skipf("Podman is not available: %v", err)
	}
	if err := rt.Ping(ctx); err != nil {
		t.Skipf("Podman is not reachable: %v", err)
	}
	binary, version := realBinary(t)
	t.Logf("deploying Boundary %s", version.Full)

	run := runtime.Run{ID: fmt.Sprintf("b%d", time.Now().UnixNano()%1_000_000_000), Scenario: "boundary-integration"}
	t.Cleanup(func() { _ = rt.RemoveRun(context.Background(), run.ID) })
	if err := rt.EnsureImage(ctx, AlpineImage, nil); err != nil {
		t.Fatal(err)
	}
	net, err := rt.CreateNetwork(ctx, runtime.NetworkOptions{Run: run, Name: "net"})
	if err != nil {
		t.Fatal(err)
	}
	r := &real{t: t, ctx: ctx, rt: rt, p: New(Options{})}
	r.spec = product.DeploySpec{
		Run: run, Network: net.ID, Cluster: "boundary", Nodes: 3, Edition: release.Community, Version: version,
		Binary: binary, Image: AlpineImage, Ports: runtime.NewPortAllocator(),
		Progress: func(s string) { t.Logf("  %s", s) },
	}
	start := time.Now()
	r.d, err = r.p.Deploy(ctx, rt, r.spec)
	t.Logf("deploy took %s", time.Since(start).Round(time.Second))
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	return r
}

func (r *real) node(name string) primitive.Node {
	r.t.Helper()
	for _, n := range r.d.Nodes {
		if n.Name == name {
			return n
		}
	}
	r.t.Fatalf("no node %q", name)
	return primitive.Node{}
}

// banner opens a real session to the target through the load balancer and the worker and returns what the
// target's SSH server says, which only a deployment whose parts all agree can do.
func (r *real) banner() string {
	r.t.Helper()
	first := r.node("node-1")
	c := client{r.rt}
	token, note, err := c.signIn(r.ctx, first, localAddr(), adminOf(r.d))
	if err != nil {
		r.t.Fatalf("sign in: %v %s", err, note)
	}
	env := map[string]string{"BOUNDARY_ADDR": localAddr(), "BOUNDARY_TOKEN": token}
	script := binaryPath + " connect -token=env://BOUNDARY_TOKEN -listen-port=2222 -target-id=" + r.d.State["targetId"] +
		" >/tmp/connect.log 2>&1 & sleep 3; (sleep 3) | nc -w 5 127.0.0.1 2222; kill %1 2>/dev/null; cat /tmp/connect.log"
	res, err := r.rt.Exec(r.ctx, first.ContainerID, []string{"sh", "-c", script}, runtime.ExecOptions{Env: env})
	if err != nil {
		r.t.Fatal(err)
	}
	return res.Stdout
}

func TestDeployARealBoundary(t *testing.T) {
	r := deployReal(t, 12*time.Minute)
	if out := r.banner(); !strings.Contains(out, "SSH-") {
		t.Errorf("no SSH banner came back through the worker: %q", out)
	}
}

// Every part that can be removed can be brought back, and a session still works afterwards.
func TestReviveEveryPartOfARealBoundary(t *testing.T) {
	r := deployReal(t, 20*time.Minute)
	for _, name := range []string{"node-2", "worker-node-1", "lb-node-1", "target-node-1", "node-1"} {
		old := r.node(name)
		if err := r.rt.Remove(r.ctx, old.ContainerID); err != nil {
			t.Fatalf("remove %s: %v", name, err)
		}
		start := time.Now()
		fresh, err := r.p.Revive(r.ctx, r.rt, r.spec, r.d, name)
		if err != nil {
			t.Fatalf("revive %s: %v", name, err)
		}
		t.Logf("revived %s in %s (address %s, was %s)", name, time.Since(start).Round(time.Second), fresh.IP, old.IP)
		for i, n := range r.d.Nodes {
			if n.Name == name {
				r.d.Nodes[i] = fresh
			}
		}
		began := time.Now()
		var out string
		for !strings.Contains(out, "SSH-") && time.Since(began) < 90*time.Second {
			out = r.banner()
			if !strings.Contains(out, "SSH-") {
				time.Sleep(3 * time.Second)
			}
		}
		if !strings.Contains(out, "SSH-") {
			t.Fatalf("after reviving %s no SSH banner came back in 90s: %q", name, out)
		}
		t.Logf("a session worked %s after reviving %s", time.Since(began).Round(time.Second), name)
	}

	// The database cannot be.
	db := r.node("db-node-1")
	if err := r.rt.Remove(r.ctx, db.ContainerID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.p.Revive(r.ctx, r.rt, r.spec, r.d, "db-node-1"); err == nil || !strings.Contains(err.Error(), "data is gone") {
		t.Errorf("reviving the database: %v", err)
	}
}

// The primitives work against a real Boundary: signing in as the admin and as a user who has no grants, which Boundary refuses
// with a 403 that must be told apart from any other failure.
func TestBoundaryPrimitivesOnARealBoundary(t *testing.T) {
	r := deployReal(t, 12*time.Minute)
	reg := primitive.NewRegistry()
	for _, p := range r.p.Primitives() {
		if err := reg.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	nodes := map[string]primitive.Node{}
	for _, n := range r.d.Nodes {
		nodes[n.Name] = n
	}
	env := primitive.Env{
		Runtime: r.rt, Run: r.spec.Run, Nodes: nodes, Creds: map[string]map[string]string{"boundary": r.d.Creds},
		Redactor: primitive.NewRedactor(r.d.Creds["adminPassword"]),
	}
	run := func(name string, args map[string]any) primitive.Result {
		t.Helper()
		args["node"] = "node-1"
		res, err := reg.Run(r.ctx, name, env, args)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		t.Logf("%s -> %s: %s", name, res.Status, res.Message)
		return res
	}

	if res := run("boundary.cli", map[string]any{"command": []string{"scopes", "list"}, "contains": "Generated org scope"}); res.Status != primitive.Pass {
		t.Errorf("cli: %+v", res)
	}
	if res := run("boundary.authorize-session", map[string]any{}); res.Status != primitive.Pass {
		t.Errorf("the admin is not allowed a session: %+v", res)
	}
	if res := run("boundary.target-info", map[string]any{"field": "session_max_seconds", "equals": "28800"}); res.Status != primitive.Pass {
		t.Errorf("target-info: %+v", res)
	}

	// A user with a password and no grants.
	run("boundary.cli", map[string]any{"command": []string{"users", "create", "-name=nobody", "-scope-id=global", "-format=json"}})
	var users struct {
		Items []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
	}
	raw, _ := reg.Run(r.ctx, "boundary.cli", env, map[string]any{"node": "node-1", "command": []string{"users", "list", "-scope-id=global", "-format=json"}, "save": "out"})
	if err := json.Unmarshal([]byte(raw.Outputs["out"]), &users); err != nil {
		t.Fatalf("users: %v", err)
	}
	var nobody string
	for _, u := range users.Items {
		if u.Name == "nobody" {
			nobody = u.ID
		}
	}
	if nobody == "" {
		t.Fatal("the user was not created")
	}
	acct, _ := reg.Run(r.ctx, "boundary.cli", env, map[string]any{"node": "node-1", "save": "out",
		"command": []string{"accounts", "create", "password", "-auth-method-id=" + r.d.State["authMethodId"], "-login-name=nobody", "-password=env://NEW_PASSWORD", "-format=json"},
		"env":     map[string]any{"NEW_PASSWORD": "nobody-password-1"}})
	var created struct {
		Item struct {
			ID string `json:"id"`
		} `json:"item"`
	}
	if err := json.Unmarshal([]byte(acct.Outputs["out"]), &created); err != nil || created.Item.ID == "" {
		t.Fatalf("the account was not created: %v %s", err, acct.Evidence)
	}
	run("boundary.cli", map[string]any{"command": []string{"users", "add-accounts", "-id=" + nobody, "-account=" + created.Item.ID}})

	if res := run("boundary.authorize-session", map[string]any{"as": "nobody", "password": "nobody-password-1", "target": r.d.State["targetId"], "expect": "denied"}); res.Status != primitive.Pass {
		t.Errorf("a user with no grants was not recognised as denied: %+v", res)
	}
	if res := run("boundary.authorize-session", map[string]any{"as": "nobody", "password": "nobody-password-1", "target": r.d.State["targetId"]}); res.Status != primitive.Fail {
		t.Errorf("a denied user passed an expectation of allowed: %+v", res)
	}
	// A wrong password is an error, not a failed expectation: it was not possible to sign in at all.
	if _, err := reg.Run(r.ctx, "boundary.cli", env, map[string]any{"node": "node-1", "command": []string{"scopes", "list"}, "as": "nobody", "password": "wrong-password-1"}); err == nil {
		t.Error("a wrong password signed in")
	}
}
