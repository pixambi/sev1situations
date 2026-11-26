// Package runtimetest is the contract every runtime.Runtime must satisfy. The
// fake and the Podman runtime run the same suite, so the fake cannot drift from
// the real behaviour that the rest of the application relies on.
package runtimetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/runtime"
)

// Options tells the suite how to start a container on the runtime under test.
type Options struct {
	// Image is an image the runtime can start without a long pull.
	Image string
	// Command keeps the container running, for example sleep 300.
	Command []string
}

// Run executes the contract against runtimes returned by newRuntime. Each
// subtest uses its own run ID and removes what it created.
func Run(t *testing.T, newRuntime func(t *testing.T) runtime.Runtime, o Options) {
	t.Helper()

	tests := []struct {
		name string
		fn   func(t *testing.T, rt runtime.Runtime, o Options)
	}{
		{"Ping succeeds", testPing},
		{"EnsureImage accepts a local image and never needs a network for it", testEnsureImage},
		{"container lifecycle", testLifecycle},
		{"a container joins a network", testNetwork},
		{"exec on a running container", testExec},
		{"exec and attach need a running container", testNeedsRunning},
		{"copy into a container", testCopy},
		{"logs", testLogs},
		{"missing objects match ErrNotFound", testNotFound},
		{"RemoveRun removes only its own run, and is idempotent", testRemoveRun},
		{"Runs lists runs that have objects", testRuns},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := newRuntime(t)
			tt.fn(t, rt, o)
		})
	}
}

func testPing(t *testing.T, rt runtime.Runtime, _ Options) {
	if err := rt.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func testEnsureImage(t *testing.T, rt runtime.Runtime, o Options) {
	var lines int
	if err := rt.EnsureImage(context.Background(), o.Image, func(string) { lines++ }); err != nil {
		t.Fatalf("EnsureImage(%s): %v", o.Image, err)
	}
	if err := rt.EnsureImage(context.Background(), o.Image, nil); err != nil {
		t.Errorf("EnsureImage a second time: %v", err)
	}
	if err := rt.EnsureImage(context.Background(), "", nil); err == nil {
		t.Error("EnsureImage with no reference succeeded, want an error")
	}
}

func testLifecycle(t *testing.T, rt runtime.Runtime, o Options) {
	ctx := context.Background()
	run := newRun(t, rt)

	c := start(t, rt, run, "node", o, "")
	if c.ID == "" || c.State != runtime.StateRunning || c.Run != run {
		t.Fatalf("StartContainer = %+v, want a running container of run %q", c, run.ID)
	}
	if !strings.Contains(c.Name, run.ID) {
		t.Errorf("name %q does not include the run ID %q", c.Name, run.ID)
	}

	got := list(t, rt, run.ID)
	if len(got) != 1 || got[0].ID != c.ID {
		t.Fatalf("List = %+v, want only %s", got, c.ID)
	}

	if err := rt.Stop(ctx, c.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := rt.Stop(ctx, c.ID); err != nil {
		t.Errorf("stopping a stopped container: %v", err)
	}
	if got := list(t, rt, run.ID); len(got) != 1 || got[0].State != runtime.StateStopped {
		t.Errorf("after Stop, List = %+v, want one stopped container", got)
	}

	if err := rt.Start(ctx, c.ID); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := rt.Start(ctx, c.ID); err != nil {
		t.Errorf("starting a running container: %v", err)
	}
	if got := list(t, rt, run.ID); len(got) != 1 || got[0].State != runtime.StateRunning {
		t.Errorf("after Start, List = %+v, want one running container", got)
	}

	if err := rt.Remove(ctx, c.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got := list(t, rt, run.ID); len(got) != 0 {
		t.Errorf("after Remove, List = %+v, want none", got)
	}
}

func testNetwork(t *testing.T, rt runtime.Runtime, o Options) {
	run := newRun(t, rt)
	n, err := rt.CreateNetwork(context.Background(), runtime.NetworkOptions{Run: run, Name: "net"})
	if err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	if n.ID == "" || !strings.Contains(n.Name, run.ID) {
		t.Fatalf("CreateNetwork = %+v, want an ID and a name that includes %q", n, run.ID)
	}

	c := start(t, rt, run, "node", o, n.ID)
	if c.IP == "" {
		t.Errorf("container on a network has no IP: %+v", c)
	}
}

func testExec(t *testing.T, rt runtime.Runtime, o Options) {
	ctx := context.Background()
	c := start(t, rt, newRun(t, rt), "node", o, "")

	res, err := rt.Exec(ctx, c.ID, []string{"true"}, runtime.ExecOptions{})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("Exec true = %+v, %v; want exit code 0 and no error", res, err)
	}
	if _, err := rt.Exec(ctx, c.ID, []string{"true"}, runtime.ExecOptions{Detach: true}); err != nil {
		t.Errorf("detached Exec: %v", err)
	}
}

func testNeedsRunning(t *testing.T, rt runtime.Runtime, o Options) {
	ctx := context.Background()
	c := start(t, rt, newRun(t, rt), "node", o, "")
	if err := rt.Stop(ctx, c.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, err := rt.Exec(ctx, c.ID, []string{"true"}, runtime.ExecOptions{}); err == nil {
		t.Error("Exec on a stopped container succeeded, want an error")
	}
	if term, err := rt.Attach(ctx, c.ID, []string{"sh"}); err == nil {
		_ = term.Close()
		t.Error("Attach on a stopped container succeeded, want an error")
	}
}

func testCopy(t *testing.T, rt runtime.Runtime, o Options) {
	ctx := context.Background()
	c := start(t, rt, newRun(t, rt), "node", o, "")
	body := "hello\n"
	f := runtime.File{Path: "/tmp/hello.txt", Mode: 0o644, Size: int64(len(body)), Content: strings.NewReader(body)}
	if err := rt.Copy(ctx, c.ID, f); err != nil {
		t.Fatalf("Copy into a running container: %v", err)
	}
	bad := runtime.File{Path: "/tmp/bad.txt", Mode: 0o644, Size: 99, Content: strings.NewReader("short")}
	if err := rt.Copy(ctx, c.ID, bad); err == nil {
		t.Error("Copy with a wrong size succeeded, want an error")
	}
}

func testLogs(t *testing.T, rt runtime.Runtime, o Options) {
	c := start(t, rt, newRun(t, rt), "node", o, "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, err := rt.Logs(ctx, c.ID, runtime.LogOptions{})
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if _, err := io.Copy(io.Discard, r); err != nil {
		t.Errorf("reading logs: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Errorf("closing logs: %v", err)
	}
}

func testNotFound(t *testing.T, rt runtime.Runtime, _ Options) {
	ctx := context.Background()
	const id = "does-not-exist"
	checks := map[string]error{
		"Start":  rt.Start(ctx, id),
		"Stop":   rt.Stop(ctx, id),
		"Remove": rt.Remove(ctx, id),
		"Copy":   rt.Copy(ctx, id, runtime.File{Path: "/x", Size: 0, Content: strings.NewReader("")}),
	}
	_, checks["Exec"] = rt.Exec(ctx, id, []string{"true"}, runtime.ExecOptions{})
	_, checks["Logs"] = rt.Logs(ctx, id, runtime.LogOptions{})
	_, checks["Attach"] = rt.Attach(ctx, id, []string{"sh"})
	for op, err := range checks {
		if !errors.Is(err, runtime.ErrNotFound) {
			t.Errorf("%s on a missing container = %v, want ErrNotFound", op, err)
		}
	}
}

func testRemoveRun(t *testing.T, rt runtime.Runtime, o Options) {
	ctx := context.Background()
	a, b := newRun(t, rt), newRun(t, rt)
	na, err := rt.CreateNetwork(ctx, runtime.NetworkOptions{Run: a, Name: "net"})
	if err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	start(t, rt, a, "one", o, na.ID)
	start(t, rt, a, "two", o, na.ID)
	start(t, rt, b, "other", o, "")

	if err := rt.RemoveRun(ctx, a.ID); err != nil {
		t.Fatalf("RemoveRun: %v", err)
	}
	if got := list(t, rt, a.ID); len(got) != 0 {
		t.Errorf("run A still has %d container(s) after RemoveRun", len(got))
	}
	if got := list(t, rt, b.ID); len(got) != 1 {
		t.Errorf("RemoveRun(A) touched run B: List(B) = %+v, want 1 container (R-RUN-4)", got)
	}
	if err := rt.RemoveRun(ctx, a.ID); err != nil {
		t.Errorf("second RemoveRun: %v, want it to be idempotent", err)
	}
	if err := rt.RemoveRun(ctx, "never-existed"); err != nil {
		t.Errorf("RemoveRun of an unknown run: %v", err)
	}
}

func testRuns(t *testing.T, rt runtime.Runtime, o Options) {
	ctx := context.Background()
	run := newRun(t, rt)
	start(t, rt, run, "node", o, "")

	runs, err := rt.Runs(ctx)
	if err != nil {
		t.Fatalf("Runs: %v", err)
	}
	if !contains(runs, run.ID) {
		t.Errorf("Runs = %v, want it to include %q", runs, run.ID)
	}
	if err := rt.RemoveRun(ctx, run.ID); err != nil {
		t.Fatalf("RemoveRun: %v", err)
	}
	runs, err = rt.Runs(ctx)
	if err != nil {
		t.Fatalf("Runs: %v", err)
	}
	if contains(runs, run.ID) {
		t.Errorf("Runs = %v, want it to omit %q after RemoveRun", runs, run.ID)
	}
}

// newRun returns a unique run and removes everything it owns when the test ends.
func newRun(t *testing.T, rt runtime.Runtime) runtime.Run {
	t.Helper()
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random run ID: %v", err)
	}
	run := runtime.Run{ID: "t" + hex.EncodeToString(b), Scenario: "contract-test"}
	t.Cleanup(func() { _ = rt.RemoveRun(context.Background(), run.ID) })
	return run
}

func start(t *testing.T, rt runtime.Runtime, run runtime.Run, name string, o Options, networkID string) runtime.Container {
	t.Helper()
	c, err := rt.StartContainer(context.Background(), runtime.ContainerOptions{
		Run: run, Name: name, Image: o.Image, Command: o.Command, Network: networkID,
	})
	if err != nil {
		t.Fatalf("StartContainer %s: %v", name, err)
	}
	return c
}

func list(t *testing.T, rt runtime.Runtime, runID string) []runtime.Container {
	t.Helper()
	got, err := rt.List(context.Background(), runID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return got
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
