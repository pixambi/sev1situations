package deployutil

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/runtime"
	"github.com/pixambi/sev1situations/internal/runtime/fake"
)

func fast() Poller { return Poller{Interval: time.Millisecond} }

func TestWaitReturnsAsSoonAsTheConditionHolds(t *testing.T) {
	calls := 0
	err := fast().Wait(context.Background(), "node-1", time.Second, "answer", func() (bool, string, error) {
		calls++
		return calls == 3, "not yet", nil
	})
	if err != nil || calls != 3 {
		t.Errorf("err = %v after %d checks, want success on the third", err, calls)
	}
}

func TestWaitThatRunsOutSaysWhoWhatForHowLongAndWhatWasLastSeen(t *testing.T) {
	p := fast()
	p.Tail = func(_ context.Context, name string) string { return "log of " + name }
	err := p.Wait(context.Background(), "db-node-1", 20*time.Millisecond, "accept connections", func() (bool, string, error) {
		return false, "pg_isready said no response", nil
	})
	if err == nil {
		t.Fatal("the wait never ended")
	}
	for _, want := range []string{"db-node-1 did not accept connections within 20ms", "Last seen: pg_isready said no response", "log of db-node-1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err, want)
		}
	}
}

func TestACheckThatCannotRunEndsTheWaitAtOnce(t *testing.T) {
	boom := errors.New("container gone")
	start := time.Now()
	err := fast().Wait(context.Background(), "node-1", time.Minute, "answer", func() (bool, string, error) { return false, "", boom })
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "node-1: could not answer") || time.Since(start) > time.Second {
		t.Errorf("err = %v after %v", err, time.Since(start))
	}
}

func TestWaitStopsWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := fast().Wait(ctx, "n", time.Minute, "x", func() (bool, string, error) { return false, "", nil }); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
}

func TestExecProblemShowsTheFirstLineOfStderrAndNeverTheOutput(t *testing.T) {
	err := ExecProblem(runtime.ExecResult{ExitCode: 2, Stderr: "boom\nmore", Stdout: "hvs.SECRET"}, nil)
	if err == nil || err.Error() != "exit code 2: boom" || strings.Contains(err.Error(), "SECRET") {
		t.Errorf("err = %v", err)
	}
	if err := ExecProblem(runtime.ExecResult{ExitCode: 3}, nil); err == nil || err.Error() != "exit code 3" {
		t.Errorf("no stderr: %v", err)
	}
	cause := errors.New("exec failed")
	if err := ExecProblem(runtime.ExecResult{}, cause); !errors.Is(err, cause) {
		t.Errorf("a command that could not run: %v", err)
	}
}

func TestFirstLineAndOrNone(t *testing.T) {
	if FirstLine("  a\nb ") != "a" || FirstLine("") != "" || OrNone("") != "nothing" || OrNone("x") != "x" {
		t.Error("helpers misbehave")
	}
}

func TestFileCarriesItsContentAndSize(t *testing.T) {
	f := File("/etc/x", 0o600, "hello")
	b, _ := io.ReadAll(f.Content)
	if f.Path != "/etc/x" || f.Mode != 0o600 || f.Size != 5 || string(b) != "hello" {
		t.Errorf("file = %+v %q", f, b)
	}
}

func TestTailLogReadsTheEndOfALogOrSaysItCould(t *testing.T) {
	rt := fake.New()
	rt.SetExec(func(runtime.Container, []string, runtime.ExecOptions) (runtime.ExecResult, error) {
		return runtime.ExecResult{Stdout: "line one\nline two"}, nil
	})
	run := runtime.Run{ID: "r1"}
	c, err := rt.StartContainer(context.Background(), runtime.ContainerOptions{Run: run, Name: "node-1", Image: "alpine:3"})
	if err != nil {
		t.Fatal(err)
	}
	got := TailLog(context.Background(), rt, c.ID, "node-1", "/var/log/x")
	if !strings.Contains(got, "Last lines of the log of node-1") || !strings.Contains(got, "line two") {
		t.Errorf("tail = %q", got)
	}
	if got := TailLog(context.Background(), rt, "no-such-container", "node-9", "/var/log/x"); !strings.Contains(got, "no log lines could be read from node-9") {
		t.Errorf("a missing container: %q", got)
	}
}
