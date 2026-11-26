//go:build integration

package podman

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/runtime"
	"github.com/pixambi/sev1situations/internal/runtime/runtimetest"
)

// These tests need a running Podman machine and a local alpine:3 image.
// Run them with: task test:integration

const (
	testImage = "docker.io/library/alpine:3"
)

var sleepCmd = []string{"sleep", "300"}

func newTestRuntime(t *testing.T) *Runtime {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rt, err := New(ctx)
	if err != nil {
		t.Skipf("Podman is not available: %v", err)
	}
	if err := rt.Ping(ctx); err != nil {
		t.Skipf("Podman is not reachable: %v", err)
	}
	return rt
}

// testContainer starts a container of its own run and removes the run afterwards.
func testContainer(t *testing.T, rt *Runtime, name string, cmd []string, ports ...runtime.PortMapping) runtime.Container {
	t.Helper()
	run := runtime.Run{ID: fmt.Sprintf("it%d", time.Now().UnixNano()%1_000_000_000), Scenario: "integration"}
	t.Cleanup(func() { _ = rt.RemoveRun(context.Background(), run.ID) })
	c, err := rt.StartContainer(context.Background(), runtime.ContainerOptions{
		Run: run, Name: name, Image: testImage, Command: cmd, Ports: ports,
	})
	if err != nil {
		t.Fatalf("StartContainer: %v", err)
	}
	return c
}

func TestContract(t *testing.T) {
	rt := newTestRuntime(t)
	runtimetest.Run(t, func(*testing.T) runtime.Runtime { return rt }, runtimetest.Options{
		Image: testImage, Command: sleepCmd,
	})
}

func TestExecOutputAndExitCode(t *testing.T) {
	rt := newTestRuntime(t)
	c := testContainer(t, rt, "exec", sleepCmd)

	res, err := rt.Exec(context.Background(), c.ID,
		[]string{"sh", "-c", "echo to-stdout; echo to-stderr 1>&2; echo $GREETING $(pwd); exit 3"},
		runtime.ExecOptions{Env: map[string]string{"GREETING": "hi"}, WorkDir: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 3 || res.Stdout != "to-stdout\nhi /tmp\n" || res.Stderr != "to-stderr\n" {
		t.Errorf("Exec = %+v", res)
	}
}

func TestCopyKeepsContentAndMode(t *testing.T) {
	rt := newTestRuntime(t)
	c := testContainer(t, rt, "copy", sleepCmd)
	body := "#!/bin/sh\necho copied-ok\n"

	err := rt.Copy(context.Background(), c.ID, runtime.File{
		Path: "/tmp/tool.sh", Mode: 0o755, Size: int64(len(body)), Content: strings.NewReader(body),
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := rt.Exec(context.Background(), c.ID, []string{"sh", "-c", "stat -c %a /tmp/tool.sh && /tmp/tool.sh"}, runtime.ExecOptions{})
	if err != nil || res.ExitCode != 0 || res.Stdout != "755\ncopied-ok\n" {
		t.Errorf("after Copy: %+v, %v; want mode 755 and an executable file", res, err)
	}
}

func TestCopyBeforeStartPoint(t *testing.T) {
	// A container that fails to start must not be left behind.
	rt := newTestRuntime(t)
	run := runtime.Run{ID: fmt.Sprintf("it%d", time.Now().UnixNano()%1_000_000_000)}
	t.Cleanup(func() { _ = rt.RemoveRun(context.Background(), run.ID) })

	_, err := rt.StartContainer(context.Background(), runtime.ContainerOptions{
		Run: run, Name: "bad", Image: testImage, Command: []string{"/no/such/binary"},
	})
	if err == nil || !strings.Contains(err.Error(), "no/such/binary") {
		t.Fatalf("StartContainer = %v, want Podman's reason", err)
	}
	left, err := rt.List(context.Background(), run.ID)
	if err != nil || len(left) != 0 {
		t.Errorf("List after a failed start = %+v, %v; want nothing left behind", left, err)
	}
}

func TestLogsContentAndFollow(t *testing.T) {
	rt := newTestRuntime(t)
	c := testContainer(t, rt, "logs", []string{"sh", "-c", "echo first-line; sleep 300"})

	// Wait until the first line is readable, without sleeping blindly.
	deadline := time.Now().Add(10 * time.Second)
	for {
		r, err := rt.Logs(context.Background(), c.ID, runtime.LogOptions{})
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(r)
		_ = r.Close()
		if strings.Contains(string(b), "first-line") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("logs never contained first-line, got %q", b)
		}
		time.Sleep(100 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	follow, err := rt.Logs(ctx, c.ID, runtime.LogOptions{Follow: true, Tail: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = follow.Close() }()
	lines := make(chan string, 8)
	go func() {
		sc := bufio.NewScanner(follow)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()

	if _, err := rt.Exec(context.Background(), c.ID, []string{"sh", "-c", "echo later-line > /proc/1/fd/1"}, runtime.ExecOptions{}); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatal("the followed stream ended before later-line arrived")
			}
			if strings.Contains(l, "later-line") {
				cancel()
				return
			}
		case <-time.After(15 * time.Second):
			t.Fatal("timed out waiting for the followed line")
		}
	}
}

func TestTerminalRoundTripAndResize(t *testing.T) {
	rt := newTestRuntime(t)
	c := testContainer(t, rt, "term", sleepCmd)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	term, err := rt.Attach(ctx, c.ID, []string{"sh"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = term.Close() }()

	readUntil := func(want string) string {
		t.Helper()
		var got strings.Builder
		buf := make([]byte, 4096)
		done := make(chan struct{})
		go func() {
			defer close(done)
			for !strings.Contains(got.String(), want) {
				n, err := term.Read(buf)
				got.Write(buf[:n])
				if err != nil {
					return
				}
			}
		}()
		select {
		case <-done:
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %q, got %q", want, got.String())
		}
		return got.String()
	}

	if _, err := term.Write([]byte("echo marker$((20+22))\n")); err != nil {
		t.Fatal(err)
	}
	readUntil("marker42")

	if err := term.Resize(ctx, 120, 40); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if _, err := term.Write([]byte("stty size\n")); err != nil {
		t.Fatal(err)
	}
	readUntil("40 120")
}

func TestPublishedPortBindsToLoopbackOnly(t *testing.T) {
	rt := newTestRuntime(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	serve := `while true; do printf 'HTTP/1.1 200 OK\r\nContent-Length: 3\r\nConnection: close\r\n\r\nok\n' | nc -l -p 8080; done`
	c := testContainer(t, rt, "port", []string{"sh", "-c", serve}, runtime.PortMapping{Host: port, Container: 8080})
	if len(c.Ports) != 1 || c.Ports[0].Host != port {
		t.Fatalf("container ports = %+v", c.Ports)
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(url)
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if string(b) != "ok\n" {
				t.Fatalf("GET %s = %q", url, b)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("GET %s: %v", url, err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// The same port must not answer on the machine's other addresses (R-APP-3).
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || !ipn.IP.IsGlobalUnicast() || ipn.IP.To4() == nil {
				continue
			}
			conn, err := net.DialTimeout("tcp", net.JoinHostPort(ipn.IP.String(), fmt.Sprint(port)), time.Second)
			if err == nil {
				_ = conn.Close()
				t.Errorf("port %d is reachable on %s, want loopback only", port, ipn.IP)
			}
		}
	}
}

func TestNeverTouchesAForeignContainer(t *testing.T) {
	rt := newTestRuntime(t)
	ctx := context.Background()

	// A container with none of our labels, created straight through the API.
	name := fmt.Sprintf("s1s-foreign-%d", time.Now().UnixNano()%1_000_000_000)
	var created struct {
		ID string `json:"Id"`
	}
	_, err := rt.c.do(ctx, http.MethodPost, "/containers/create", nil, map[string]any{
		"name": name, "image": testImage, "command": sleepCmd,
	}, &created)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.remove(context.Background(), created.ID) })

	if err := rt.Remove(ctx, created.ID); err == nil || !strings.Contains(err.Error(), "not created by this application") {
		t.Errorf("Remove of a foreign container = %v, want a refusal", err)
	}
	if err := rt.RemoveRun(ctx, "no-such-run"); err != nil {
		t.Errorf("RemoveRun: %v", err)
	}
	if _, err := rt.inspect(ctx, created.ID); err != nil {
		t.Errorf("the foreign container is gone: %v (R-RUN-4)", err)
	}
}

func TestLeftoversFoundAndRemovedByAFreshRuntime(t *testing.T) {
	// Simulates a crash: the process that created the run is gone, only labels remain.
	first := newTestRuntime(t)
	run := runtime.Run{ID: fmt.Sprintf("it%d", time.Now().UnixNano()%1_000_000_000), Scenario: "crash"}
	t.Cleanup(func() { _ = first.RemoveRun(context.Background(), run.ID) })

	n, err := first.CreateNetwork(context.Background(), runtime.NetworkOptions{Run: run, Name: "net"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.StartContainer(context.Background(), runtime.ContainerOptions{
		Run: run, Name: "a", Image: testImage, Command: sleepCmd, Network: n.ID,
	}); err != nil {
		t.Fatal(err)
	}

	second := newTestRuntime(t) // a new process would build a new Runtime
	runs, err := second.Runs(context.Background())
	if err != nil || !contains(runs, run.ID) {
		t.Fatalf("Runs = %v, %v; want it to find the leftover run %s", runs, err, run.ID)
	}
	if err := second.RemoveRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	runs, _ = second.Runs(context.Background())
	if contains(runs, run.ID) {
		t.Errorf("run %s still present after cleanup: %v", run.ID, runs)
	}
	if got, _ := second.listNetworks(context.Background(), run.ID); len(got) != 0 {
		t.Errorf("networks left behind: %+v", got)
	}
}

func TestEnsureImageFailsClearlyForAnUnknownImage(t *testing.T) {
	rt := newTestRuntime(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	err := rt.EnsureImage(ctx, "docker.io/library/s1s-no-such-image:nope", nil)
	if err == nil {
		t.Fatal("EnsureImage of an unknown image succeeded")
	}
	t.Logf("error for an unknown image: %v", err)
}

func TestUnavailableWithBadSocket(t *testing.T) {
	rt := NewWithSocket("/var/empty/no-such.sock")
	if err := rt.Ping(context.Background()); !errors.Is(err, runtime.ErrUnavailable) {
		t.Errorf("Ping = %v, want ErrUnavailable", err)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
