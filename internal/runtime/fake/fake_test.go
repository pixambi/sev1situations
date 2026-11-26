package fake_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/runtime"
	"github.com/pixambi/sev1situations/internal/runtime/fake"
	"github.com/pixambi/sev1situations/internal/runtime/runtimetest"
)

func TestContract(t *testing.T) {
	runtimetest.Run(t, func(*testing.T) runtime.Runtime { return fake.New() }, runtimetest.Options{
		Image:   "docker.io/library/alpine:3",
		Command: []string{"sleep", "300"},
	})
}

var testRun = runtime.Run{ID: "r1", Scenario: "vault-sandbox"}

func startOne(t *testing.T, f *fake.Runtime, name string, ports ...runtime.PortMapping) runtime.Container {
	t.Helper()
	c, err := f.StartContainer(context.Background(), runtime.ContainerOptions{
		Run: testRun, Name: name, Image: "alpine:3", Ports: ports,
	})
	if err != nil {
		t.Fatalf("StartContainer: %v", err)
	}
	return c
}

func TestScriptedExec(t *testing.T) {
	f := fake.New()
	c := startOne(t, f, "vault")
	f.SetExec(func(c runtime.Container, cmd []string, _ runtime.ExecOptions) (runtime.ExecResult, error) {
		if len(cmd) > 0 && cmd[0] == "vault" {
			return runtime.ExecResult{ExitCode: 2, Stderr: "sealed"}, nil
		}
		return runtime.ExecResult{Stdout: c.Name}, nil
	})

	tests := []struct {
		name string
		cmd  []string
		want runtime.ExecResult
	}{
		{"scripted failure", []string{"vault", "status"}, runtime.ExecResult{ExitCode: 2, Stderr: "sealed"}},
		{"scripted output", []string{"hostname"}, runtime.ExecResult{Stdout: "s1s-r1-vault"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.Exec(context.Background(), c.ID, tt.cmd, runtime.ExecOptions{})
			if err != nil || got != tt.want {
				t.Errorf("Exec = %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}

	got, err := f.Exec(context.Background(), c.ID, []string{"vault", "status"}, runtime.ExecOptions{Detach: true})
	if err != nil || got != (runtime.ExecResult{}) {
		t.Errorf("detached Exec = %+v, %v; want an empty result without running the script", got, err)
	}
}

func TestFailNext(t *testing.T) {
	f := fake.New()
	boom := errors.New("boom")
	f.FailNext(fake.OpStartContainer, boom)

	_, err := f.StartContainer(context.Background(), runtime.ContainerOptions{Run: testRun, Name: "a", Image: "alpine:3"})
	if !errors.Is(err, boom) {
		t.Fatalf("first StartContainer = %v, want the injected error", err)
	}
	if _, err := f.StartContainer(context.Background(), runtime.ContainerOptions{Run: testRun, Name: "a", Image: "alpine:3"}); err != nil {
		t.Errorf("second StartContainer = %v, want the failure to be one-shot", err)
	}
}

func TestUnavailable(t *testing.T) {
	f := fake.New()
	c := startOne(t, f, "a")
	f.SetUnavailable(errors.New("machine stopped"))

	ctx := context.Background()
	_, listErr := f.List(ctx, "r1")
	_, startErr := f.StartContainer(ctx, runtime.ContainerOptions{Run: testRun, Name: "b", Image: "alpine:3"})
	for name, err := range map[string]error{
		"Ping": f.Ping(ctx), "List": listErr, "StartContainer": startErr, "Remove": f.Remove(ctx, c.ID),
	} {
		if !errors.Is(err, runtime.ErrUnavailable) {
			t.Errorf("%s = %v, want ErrUnavailable", name, err)
		}
	}

	f.SetUnavailable(nil)
	if err := f.Ping(ctx); err != nil {
		t.Errorf("Ping after recovery = %v", err)
	}
}

func TestPortConflict(t *testing.T) {
	f := fake.New()
	startOne(t, f, "a", runtime.PortMapping{Host: 19000, Container: 8200})

	_, err := f.StartContainer(context.Background(), runtime.ContainerOptions{
		Run: testRun, Name: "b", Image: "alpine:3", Ports: []runtime.PortMapping{{Host: 19000, Container: 8200}},
	})
	if err == nil || !strings.Contains(err.Error(), "19000") {
		t.Errorf("second container on host port 19000 = %v, want a port error", err)
	}
}

func TestLogsTailAndFollow(t *testing.T) {
	f := fake.New()
	c := startOne(t, f, "a")
	for _, l := range []string{"one", "two", "three"} {
		if err := f.Feed(c.ID, l); err != nil {
			t.Fatal(err)
		}
	}

	r, err := f.Logs(context.Background(), c.ID, runtime.LogOptions{Tail: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := io.ReadAll(r); string(got) != "two\nthree\n" {
		t.Errorf("tail 2 = %q, want %q", got, "two\nthree\n")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	follow, err := f.Logs(ctx, c.ID, runtime.LogOptions{Follow: true, Tail: 1})
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(follow)
	next := func() string {
		t.Helper()
		done := make(chan string, 1)
		go func() {
			if sc.Scan() {
				done <- sc.Text()
			} else {
				done <- "<closed>"
			}
		}()
		select {
		case s := <-done:
			return s
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for a log line")
			return ""
		}
	}
	if got := next(); got != "three" {
		t.Errorf("first followed line = %q, want the tail line three", got)
	}
	if err := f.Feed(c.ID, "four"); err != nil {
		t.Fatal(err)
	}
	if got := next(); got != "four" {
		t.Errorf("followed line = %q, want four", got)
	}

	if err := f.Remove(context.Background(), c.ID); err != nil {
		t.Fatal(err)
	}
	if got := next(); got != "<closed>" {
		t.Errorf("after Remove the stream gave %q, want it to end", got)
	}
}

func TestLogsFollowEndsWithContext(t *testing.T) {
	f := fake.New()
	c := startOne(t, f, "a")
	ctx, cancel := context.WithCancel(context.Background())
	r, err := f.Logs(ctx, c.ID, runtime.LogOptions{Follow: true})
	if err != nil {
		t.Fatal(err)
	}
	cancel()

	done := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, r); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the followed stream did not end after the context was cancelled")
	}
}

func TestCopyStoresMode(t *testing.T) {
	f := fake.New()
	c := startOne(t, f, "a")
	body := "#!/bin/sh\n"
	err := f.Copy(context.Background(), c.ID, runtime.File{
		Path: "/usr/local/bin/vault", Mode: 0o755, Size: int64(len(body)), Content: strings.NewReader(body),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := f.File(c.ID, "/usr/local/bin/vault")
	if !ok || got.Mode != 0o755 || string(got.Content) != body {
		t.Errorf("File = %+v, %v; want the content with mode 0755", got, ok)
	}

	err = f.Copy(context.Background(), c.ID, runtime.File{Path: "relative", Size: 0, Content: strings.NewReader("")})
	if err == nil {
		t.Error("Copy to a relative path succeeded, want an error")
	}
}

func TestTerminalEchoesAndResizes(t *testing.T) {
	f := fake.New()
	c := startOne(t, f, "a")
	term, err := f.Attach(context.Background(), c.ID, []string{"sh"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = term.Close() }()

	if _, err := term.Write([]byte("ls\n")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 16)
	n, err := term.Read(buf)
	if err != nil || string(buf[:n]) != "ls\n" {
		t.Errorf("Read = %q, %v; want the echoed input", buf[:n], err)
	}

	if err := term.Resize(context.Background(), 120, 40); err != nil {
		t.Fatal(err)
	}
	if cols, rows := term.(*fake.Terminal).Size(); cols != 120 || rows != 40 {
		t.Errorf("size = %dx%d, want 120x40", cols, rows)
	}

	_ = term.Close()
	if _, err := term.Read(buf); !errors.Is(err, io.EOF) {
		t.Errorf("Read after Close = %v, want EOF", err)
	}
}

func TestRemoveRunContinuesPastFailure(t *testing.T) {
	f := fake.New()
	ctx := context.Background()
	n, err := f.CreateNetwork(ctx, runtime.NetworkOptions{Run: testRun, Name: "net"})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, name := range []string{"a", "b", "c"} {
		c, err := f.StartContainer(ctx, runtime.ContainerOptions{Run: testRun, Name: name, Image: "alpine:3", Network: n.ID})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, c.ID)
	}

	stuck := errors.New("device busy")
	f.FailNext(fake.OpRemove, stuck)
	err = f.RemoveRun(ctx, testRun.ID)
	if !errors.Is(err, stuck) {
		t.Fatalf("RemoveRun = %v, want it to report the failed removal", err)
	}

	left, _ := f.List(ctx, testRun.ID)
	if len(left) != 1 || left[0].ID != ids[0] {
		t.Errorf("after one failed removal List = %+v, want only %s left", left, ids[0])
	}
	if runs, _ := f.Runs(ctx); len(runs) != 1 {
		t.Errorf("Runs = %v, want the run to remain while a container is stuck", runs)
	}

	if err := f.RemoveRun(ctx, testRun.ID); err != nil {
		t.Fatalf("retry RemoveRun = %v", err)
	}
	if runs, _ := f.Runs(ctx); len(runs) != 0 {
		t.Errorf("Runs = %v, want none after the retry", runs)
	}
}

func TestDuplicateNames(t *testing.T) {
	f := fake.New()
	startOne(t, f, "a")
	if _, err := f.StartContainer(context.Background(), runtime.ContainerOptions{Run: testRun, Name: "a", Image: "alpine:3"}); err == nil {
		t.Error("starting a second container with the same name succeeded, want an error")
	}
}
