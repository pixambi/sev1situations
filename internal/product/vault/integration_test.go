//go:build integration

package vault

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/release"
	"github.com/pixambi/sev1situations/internal/runtime"
	"github.com/pixambi/sev1situations/internal/runtime/podman"
)

// These tests need a running Podman machine, a local alpine:3 image and network
// access the first time, to download Vault. The binary is cached in the user
// cache directory so later runs need no download. Run them with: task test:integration

func cacheDir(t *testing.T) string {
	t.Helper()
	base, err := os.UserCacheDir()
	if err != nil {
		t.Skipf("no user cache directory: %v", err)
	}
	return filepath.Join(base, "sev1situations-integration")
}

// realBinary returns the newest community Vault for this machine's architecture,
// downloaded and verified once.
func realBinary(t *testing.T) (string, release.Version) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	client := release.NewClient()
	versions, err := client.Versions(ctx, release.Vault, release.Community)
	if err != nil || len(versions) == 0 {
		t.Skipf("cannot list Vault versions (offline?): %v", err)
	}
	v := versions[0]
	path, err := release.NewCache(cacheDir(t), client).Ensure(ctx, v, goruntime.GOARCH, func(done, total int64) {})
	if err != nil {
		t.Skipf("cannot fetch Vault %s: %v", v.Full, err)
	}
	return path, v
}

func TestDeployARealThreeNodeCluster(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	rt, err := podman.New(ctx)
	if err != nil {
		t.Skipf("Podman is not available: %v", err)
	}
	if err := rt.Ping(ctx); err != nil {
		t.Skipf("Podman is not reachable: %v", err)
	}
	binary, version := realBinary(t)
	t.Logf("deploying Vault %s from %s", version.Full, binary)

	p := New(Options{})
	run := runtime.Run{ID: fmt.Sprintf("v%d", time.Now().UnixNano()%1_000_000_000), Scenario: "vault-integration"}
	t.Cleanup(func() { _ = rt.RemoveRun(context.Background(), run.ID) })

	if err := rt.EnsureImage(ctx, AlpineImage, nil); err != nil {
		t.Fatalf("EnsureImage: %v", err)
	}
	net, err := rt.CreateNetwork(ctx, runtime.NetworkOptions{Run: run, Name: "net"})
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	d, err := p.Deploy(ctx, rt, product.DeploySpec{
		Run: run, Network: net.ID, Cluster: "primary", Nodes: 3, Storage: "raft",
		Edition: release.Community, Version: version, Binary: binary, Image: AlpineImage,
		Ports:    runtime.NewPortAllocator(),
		Progress: func(s string) { t.Logf("  %s", s) },
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	t.Logf("deployed in %s", time.Since(start).Round(time.Second))

	token := d.Creds["rootToken"]
	if token == "" || len(unsealKeys(&d)) != 5 {
		t.Fatalf("creds = %d keys, token set: %v", len(unsealKeys(&d)), token != "")
	}
	env := map[string]string{"VAULT_ADDR": "http://127.0.0.1:8200", "VAULT_TOKEN": token}

	// A leader exists and all three nodes are voters.
	res, err := rt.Exec(ctx, d.Nodes[0].ContainerID, []string{binaryPath, "operator", "raft", "list-peers", "-format=json"}, runtime.ExecOptions{Env: env})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("list-peers: %+v, %v", res, err)
	}
	peers, err := parsePeers(res.Stdout)
	if err != nil {
		t.Fatal(err)
	}
	if n, leader := peers.voters(); n != 3 || leader == "" {
		t.Errorf("voters = %d leader = %q, want 3 and a leader", n, leader)
	}

	// Every node is unsealed, and exactly one is active.
	active := 0
	for _, n := range d.Nodes {
		res, err := rt.Exec(ctx, n.ContainerID, []string{binaryPath, "status", "-format=json"}, runtime.ExecOptions{Env: map[string]string{"VAULT_ADDR": "http://127.0.0.1:8200"}})
		if err != nil {
			t.Fatalf("status on %s: %v", n.Name, err)
		}
		st, err := parseStatus(res.Stdout)
		if err != nil || st.Sealed || !st.Initialized {
			t.Errorf("%s = %+v, %v, want it initialised and unsealed", n.Name, st, err)
		}
		t.Logf("%s: version %s, role %q", n.Name, st.Version, st.role())
		if st.role() == roleActive {
			active++
		}
	}
	if active != 1 {
		t.Errorf("%d nodes are active, want exactly one", active)
	}

	// Audit logging is on by default, and both logs exist for the Logs tab (R-VIEW-3).
	for _, f := range []string{AuditLog, OperationalLog} {
		res, err := rt.Exec(ctx, d.Nodes[0].ContainerID, []string{"sh", "-c", "wc -c < " + f}, runtime.ExecOptions{})
		if err != nil || res.ExitCode != 0 || strings.TrimSpace(res.Stdout) == "0" {
			t.Errorf("%s on the first node: %+v, %v, want a log with content", f, res, err)
		}
	}

	// The API answers on the published loopback port, from this computer.
	if len(d.Published) != 3 {
		t.Fatalf("published = %+v", d.Published)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/sys/health?standbyok=true", d.Published[0].Host)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"sealed":false`) {
		t.Errorf("GET %s = %d %s", url, resp.StatusCode, body)
	}
}
