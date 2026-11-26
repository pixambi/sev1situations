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

	"github.com/pixambi/sev1situations/internal/primitive"
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
	spec := product.DeploySpec{
		Run: run, Network: net.ID, Cluster: "primary", Nodes: 3, Storage: "raft",
		Edition: release.Community, Version: version, Binary: binary, Image: AlpineImage,
		Ports:    runtime.NewPortAllocator(),
		Progress: func(s string) { t.Logf("  %s", s) },
	}
	d, err := p.Deploy(ctx, rt, spec)
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

	// The Cluster tab shows what is really there: live state and role per node, the
	// credentials flagged secret, and an address that works from this computer.
	view := p.Describe(ctx, rt, d)
	sec := view.Sections[0]
	byLabel := func(fs []product.Fact) map[string]product.Fact {
		m := map[string]product.Fact{}
		for _, f := range fs {
			m[f.Label] = f
		}
		return m
	}
	cluster := byLabel(sec.Facts)
	if cluster["Root token"].Value != token || !cluster["Root token"].Secret || cluster["Unseal threshold"].Value != "3 of 5" {
		t.Errorf("cluster facts = %+v", cluster)
	}
	roles := map[string]int{}
	for _, n := range sec.Nodes {
		f := byLabel(n.Details)
		if f["Vault"].Value != "unsealed" || f["Version"].Value != version.Core+" community" {
			t.Errorf("Describe says %s is %+v", n.Name, f)
		}
		roles[f["HA role"].Value]++
	}
	if roles["active"] != 1 || roles["standby"] != 2 {
		t.Errorf("Describe roles = %v, want one active and two standby", roles)
	}

	// The primitives work on the real cluster.
	realPrimitives(t, ctx, rt, d)

	// A node that is removed can be revived: a fresh container takes its place, rejoins the
	// cluster, is unsealed and catches up, so it can read what was written before.
	reviveRealNode(t, ctx, rt, p, spec, d)

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

// realPrimitives runs the Vault primitives against the real cluster, including a
// genuine seal and unseal of a node.
func realPrimitives(t *testing.T, ctx context.Context, rt runtime.Runtime, d product.Deployment) {
	t.Helper()
	reg := primitive.NewRegistry()
	for _, prim := range New(Options{}).Primitives() {
		if err := reg.Register(prim); err != nil {
			t.Fatal(err)
		}
	}
	nodes := map[string]primitive.Node{}
	for _, n := range d.Nodes {
		nodes[n.Name] = n
	}
	env := primitive.Env{
		Runtime: rt, Nodes: nodes, Creds: map[string]map[string]string{d.Cluster: d.Creds},
		Redactor: primitive.NewRedactor(d.Creds["rootToken"]),
	}
	type m = map[string]any
	run := func(name string, args m) primitive.Result {
		res, err := reg.Run(ctx, name, env, args)
		if err != nil {
			t.Fatalf("%s %v: %v", name, args, err)
		}
		return res
	}
	want := func(name string, args m, status primitive.Status) primitive.Result {
		t.Helper()
		res := run(name, args)
		if res.Status != status {
			t.Errorf("%s %v = %s %q, want %s", name, args, res.Status, res.Message, status)
		}
		return res
	}

	want("vault.status", m{"node": "node-1", "initialized": true, "sealed": false}, primitive.Pass)
	want("vault.raft-peers", m{"node": "node-1", "servers": 3, "voters": 3}, primitive.Pass)
	want("vault.expect-no-leader", m{"node": "node-1"}, primitive.Fail) // a healthy cluster has a leader
	want("vault.init", m{"node": "node-1"}, primitive.Fail)             // it is initialised already

	want("vault.kv-put", m{"node": "node-1", "path": "app/db", "data": m{"user": "ada", "pass": "hunter2"}}, primitive.Pass)
	want("vault.kv-get", m{"node": "node-2", "path": "app/db", "field": "user", "equals": "ada"}, primitive.Pass)
	want("vault.kv-get", m{"node": "node-1", "path": "app/db", "field": "pass", "equals": "wrong"}, primitive.Fail)
	want("vault.cli", m{"node": "node-1", "command": []any{"status"}}, primitive.Pass)

	// Vault only seals the active node, so seal that one for real. The others elect a new
	// leader, the status primitive sees the node sealed, and the unseal primitive brings it
	// back as a standby.
	want("vault.cli", m{"node": "node-1", "command": []any{"operator", "seal"}}, primitive.Pass)
	want("vault.status", m{"node": "node-1", "sealed": true}, primitive.Pass)
	want("vault.unseal", m{"node": "node-1"}, primitive.Pass)
	want("vault.status", m{"node": "node-1", "sealed": false}, primitive.Pass)
	want("vault.unseal", m{"node": "node-1"}, primitive.Pass) // already unsealed

	// The token never appears in anything the primitives reported.
	res := run("vault.cli", m{"node": "node-1", "command": []any{"token", "lookup"}})
	if strings.Contains(res.Evidence, d.Creds["rootToken"]) {
		t.Error("the root token is in the evidence")
	}
}

// reviveRealNode removes node-3 and revives it.
func reviveRealNode(t *testing.T, ctx context.Context, rt runtime.Runtime, p *Product, spec product.DeploySpec, d product.Deployment) {
	t.Helper()
	victim := d.Nodes[2]
	if err := rt.Remove(ctx, victim.ContainerID); err != nil {
		t.Fatalf("remove %s: %v", victim.Name, err)
	}

	start := time.Now()
	fresh, err := p.Revive(ctx, rt, spec, d, victim.Name)
	if err != nil {
		t.Fatalf("Revive: %v", err)
	}
	t.Logf("revived %s in %s: new container %s at %s", fresh.Name, time.Since(start).Round(time.Second), fresh.ContainerID[:12], fresh.IP)
	if fresh.ContainerID == victim.ContainerID {
		t.Error("the revived node reuses the removed container")
	}

	nodes := map[string]primitive.Node{}
	for _, n := range d.Nodes {
		nodes[n.Name] = n
	}
	nodes[fresh.Name] = fresh
	reg := primitive.NewRegistry()
	for _, prim := range p.Primitives() {
		if err := reg.Register(prim); err != nil {
			t.Fatal(err)
		}
	}
	env := primitive.Env{Runtime: rt, Nodes: nodes, Creds: map[string]map[string]string{d.Cluster: d.Creds}}
	for name, args := range map[string]map[string]any{
		"vault.raft-peers": {"node": "node-1", "servers": 3, "voters": 3},
		"vault.status":     {"node": "node-3", "sealed": false, "initialized": true},
		// Written before the node was removed: the replacement caught up from the leader.
		"vault.kv-get": {"node": "node-3", "path": "app/db", "field": "user", "equals": "ada"},
	} {
		res, err := reg.Run(ctx, name, env, args)
		if err != nil || res.Status != primitive.Pass {
			t.Errorf("after the revive, %s %v = %+v, %v", name, args, res, err)
		}
	}
}
