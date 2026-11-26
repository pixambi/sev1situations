package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/pixambi/sev1situations/internal/release"
	"github.com/pixambi/sev1situations/internal/runtime"
	"github.com/pixambi/sev1situations/internal/runtime/fake"
)

// This is the acceptance test for milestone M2: deploy a labelled container,
// exec in it, copy a cached and verified binary into it, and tear everything
// down, all through the runtime.Runtime interface. It runs against the fake
// here and against real Podman in deploy_flow_integration_test.go.

const flowBinary = "#!/bin/sh\necho vault ok\n"

// flowReleaseSite serves one release of "vault" whose binary is a shell script.
func flowReleaseSite(t *testing.T) *httptest.Server {
	t.Helper()
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, err := zw.Create("vault")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte(flowBinary))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(zbuf.Bytes())
	const name = "vault_1.0.0_linux_arm64.zip"

	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/vault/index.json", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"versions":{"1.0.0":{"shasums":"SHA256SUMS","builds":[{"os":"linux","arch":"arm64","filename":%q,"url":"%s/files/%s"}]}}}`,
			name, srv.URL, name)
	})
	mux.HandleFunc("/vault/1.0.0/SHA256SUMS", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	})
	mux.HandleFunc("/files/"+name, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(zbuf.Bytes()) })
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// deployFlow runs the whole flow against rt using the given image.
func deployFlow(t *testing.T, rt runtime.Runtime, image string, command []string) {
	t.Helper()
	ctx := context.Background()
	site := flowReleaseSite(t)
	cache := release.NewCache(t.TempDir(), release.NewClientFor(site.URL, site.Client()))
	run := runtime.Run{ID: fmt.Sprintf("m2%d", os.Getpid()%100000), Scenario: "m2-acceptance"}
	t.Cleanup(func() { _ = rt.RemoveRun(context.Background(), run.ID) })

	// Start-up: the runtime is reachable and nothing of ours is left over.
	if _, err := Startup(ctx, rt, quiet); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	// The binary is downloaded, verified and cached.
	bin, err := cache.Ensure(ctx, release.Version{Product: release.Vault, Full: "1.0.0", Core: "1.0.0"}, "arm64", nil)
	if err != nil {
		t.Fatalf("cache.Ensure: %v", err)
	}

	// Deploy a labelled container on a run network.
	if err := rt.EnsureImage(ctx, image, nil); err != nil {
		t.Fatalf("EnsureImage: %v", err)
	}
	n, err := rt.CreateNetwork(ctx, runtime.NetworkOptions{Run: run, Name: "net"})
	if err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	c, err := rt.StartContainer(ctx, runtime.ContainerOptions{
		Run: run, Name: "vault", Image: image, Command: command, Network: n.ID,
	})
	if err != nil {
		t.Fatalf("StartContainer: %v", err)
	}
	if c.Run != run {
		t.Errorf("container run = %+v, want %+v", c.Run, run)
	}

	// Copy the cached binary in and run it.
	f, err := os.Open(bin)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	err = rt.Copy(ctx, c.ID, runtime.File{Path: "/usr/local/bin/vault", Mode: 0o755, Size: info.Size(), Content: f})
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	res, err := rt.Exec(ctx, c.ID, []string{"/usr/local/bin/vault"}, runtime.ExecOptions{})
	if err != nil || res.ExitCode != 0 || res.Stdout != "vault ok\n" {
		t.Fatalf("Exec of the copied binary = %+v, %v; want exit 0 and %q", res, err, "vault ok\n")
	}

	// Tear everything down.
	if err := rt.RemoveRun(ctx, run.ID); err != nil {
		t.Fatalf("RemoveRun: %v", err)
	}
	left, err := rt.List(ctx, run.ID)
	if err != nil || len(left) != 0 {
		t.Errorf("List after teardown = %+v, %v; want nothing", left, err)
	}
	runs, err := rt.Runs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range runs {
		if id == run.ID {
			t.Errorf("run %s still present after teardown", id)
		}
	}
}

func TestDeployFlowWithTheFakeRuntime(t *testing.T) {
	f := fake.New()
	f.RequireImages()
	f.AddImage("alpine:3")
	// The fake does not run anything, so make it answer for the binary it was given.
	f.SetExec(func(c runtime.Container, cmd []string, _ runtime.ExecOptions) (runtime.ExecResult, error) {
		file, ok := f.File(c.ID, cmd[0])
		if !ok || file.Mode&0o111 == 0 || string(file.Content) != flowBinary {
			return runtime.ExecResult{ExitCode: 126, Stderr: "not an executable copy of the binary"}, nil
		}
		return runtime.ExecResult{Stdout: "vault ok\n"}, nil
	})

	deployFlow(t, f, "alpine:3", []string{"sleep", "300"})
}
