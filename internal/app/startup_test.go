package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/pixambi/sev1situations/internal/runtime"
	"github.com/pixambi/sev1situations/internal/runtime/fake"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func leftover(t *testing.T, f *fake.Runtime, runID string, containers ...string) {
	t.Helper()
	ctx := context.Background()
	run := runtime.Run{ID: runID, Scenario: "vault-sandbox"}
	n, err := f.CreateNetwork(ctx, runtime.NetworkOptions{Run: run, Name: "net"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range containers {
		if _, err := f.StartContainer(ctx, runtime.ContainerOptions{Run: run, Name: name, Image: "alpine:3", Network: n.ID}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStartupRemovesLeftovers(t *testing.T) {
	f := fake.New()
	leftover(t, f, "old1", "a", "b")
	leftover(t, f, "old2", "c")

	report, err := Startup(context.Background(), f, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.RemovedRuns) != 2 || report.RemovedRuns[0] != "old1" || report.RemovedRuns[1] != "old2" {
		t.Errorf("RemovedRuns = %v, want old1 and old2", report.RemovedRuns)
	}
	if len(report.Failures) != 0 {
		t.Errorf("Failures = %v", report.Failures)
	}
	if runs, _ := f.Runs(context.Background()); len(runs) != 0 {
		t.Errorf("Runs after start-up = %v, want none", runs)
	}
}

func TestStartupWithNothingLeftOver(t *testing.T) {
	report, err := Startup(context.Background(), fake.New(), quiet)
	if err != nil || len(report.RemovedRuns) != 0 || len(report.Failures) != 0 {
		t.Errorf("Startup = %+v, %v; want an empty report", report, err)
	}
}

func TestStartupReportsWhatItCouldNotRemoveAndGoesOn(t *testing.T) {
	f := fake.New()
	leftover(t, f, "stuck", "a")
	leftover(t, f, "fine", "b")
	busy := errors.New("device busy")
	f.FailNext(fake.OpRemove, busy) // the first removal (run "fine" sorts first) fails

	report, err := Startup(context.Background(), f, quiet)
	if err != nil {
		t.Fatalf("a run that cannot be removed must not stop start-up: %v", err)
	}
	if len(report.Failures) != 1 || !errors.Is(report.Failures[0].Err, busy) {
		t.Fatalf("Failures = %+v, want the one failed run with its cause", report.Failures)
	}
	if len(report.RemovedRuns) != 1 {
		t.Errorf("RemovedRuns = %v, want the other run still cleaned up", report.RemovedRuns)
	}
}

func TestStartupExplainsAnUnavailableRuntime(t *testing.T) {
	f := fake.New()
	f.SetUnavailable(errors.New("machine stopped"))

	_, err := Startup(context.Background(), f, quiet)
	if !errors.Is(err, runtime.ErrUnavailable) {
		t.Fatalf("Startup = %v, want ErrUnavailable so the caller can offer a retry", err)
	}
}

func TestStartupStopsWhenCancelled(t *testing.T) {
	f := fake.New()
	leftover(t, f, "old", "a")
	f.FailNext(fake.OpRemoveRun, context.Canceled)

	if _, err := Startup(context.Background(), f, quiet); !errors.Is(err, context.Canceled) {
		t.Errorf("Startup = %v, want context.Canceled to stop it", err)
	}
}
