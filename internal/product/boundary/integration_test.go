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

func TestDeployARealBoundary(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	rt, err := podman.New(ctx)
	if err != nil {
		t.Skipf("Podman is not available: %v", err)
	}
	if err := rt.Ping(ctx); err != nil {
		t.Skipf("Podman is not reachable: %v", err)
	}
	binary, version := realBinary(t)
	t.Logf("deploying Boundary %s", version.Full)

	p := New(Options{})
	run := runtime.Run{ID: fmt.Sprintf("b%d", time.Now().UnixNano()%1_000_000_000), Scenario: "boundary-integration"}
	t.Cleanup(func() { _ = rt.RemoveRun(context.Background(), run.ID) })

	if err := rt.EnsureImage(ctx, AlpineImage, nil); err != nil {
		t.Fatal(err)
	}
	net, err := rt.CreateNetwork(ctx, runtime.NetworkOptions{Run: run, Name: "net"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	d, err := p.Deploy(ctx, rt, product.DeploySpec{
		Run: run, Network: net.ID, Cluster: "boundary", Nodes: 3, Edition: release.Community, Version: version,
		Binary: binary, Image: AlpineImage, Ports: runtime.NewPortAllocator(),
		Progress: func(s string) { t.Logf("  %s", s) },
	})
	t.Logf("deploy took %s, err = %v", time.Since(start).Round(time.Second), err)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	// The real test of a deployment is a session: sign in, ask to connect to the target, and read the SSH banner
	// through the worker, which proves the load balancer, the controllers, the worker and the target host agree.
	var first primitive.Node
	for _, n := range d.Nodes {
		if n.Name == "node-1" {
			first = n
		}
	}
	env := map[string]string{"BOUNDARY_ADDR": "http://127.0.0.1:9200", "BOUNDARY_PASSWORD": d.Creds["adminPassword"]}
	run1 := func(cmd ...string) runtime.ExecResult {
		res, err := rt.Exec(ctx, first.ContainerID, cmd, runtime.ExecOptions{Env: env})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	auth := run1(binaryPath, "authenticate", "password", "-auth-method-id="+d.State["authMethodId"], "-login-name=admin", "-password=env://BOUNDARY_PASSWORD", "-keyring-type=none", "-format=json")
	if auth.ExitCode != 0 {
		t.Fatalf("sign in: %s", auth.Stderr)
	}
	var tok struct {
		Item struct {
			Attributes struct {
				Token string `json:"token"`
			} `json:"attributes"`
		} `json:"item"`
	}
	if err := json.Unmarshal([]byte(auth.Stdout), &tok); err != nil || tok.Item.Attributes.Token == "" {
		t.Fatalf("no token in %q", auth.Stdout)
	}
	env["BOUNDARY_TOKEN"] = tok.Item.Attributes.Token

	authz := run1(binaryPath, "targets", "authorize-session", "-id="+d.State["targetId"], "-format=json", "-token=env://BOUNDARY_TOKEN")
	t.Logf("authorize-session: exit %d, %.300s %.300s", authz.ExitCode, authz.Stdout, authz.Stderr)

	conn := run1("sh", "-c", binaryPath+" connect -token=env://BOUNDARY_TOKEN -listen-port=2222 -target-id="+d.State["targetId"]+
		" >/tmp/connect.log 2>&1 & sleep 3; (sleep 3) | nc -w 5 127.0.0.1 2222; kill %1 2>/dev/null; cat /tmp/connect.log")
	t.Logf("connect: exit %d, stdout %q, stderr %.400q", conn.ExitCode, conn.Stdout, conn.Stderr)
	if !strings.Contains(conn.Stdout, "SSH-") {
		t.Errorf("no SSH banner came back through the worker")
	}
}
