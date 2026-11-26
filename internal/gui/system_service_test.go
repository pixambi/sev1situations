package gui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pixambi/sev1situations/internal/engine"
)

func TestPrereqConnectsAndPublishesTheState(t *testing.T) {
	b := newBackend(t, nil)
	calls := 0
	l := newLink(context.Background(), func(context.Context) (*engine.Engine, []string, error) {
		calls++
		return b.eng, []string{"Removed what run abc left behind when the application last closed."}, nil
	}, b.out.emit)
	svc := NewSystemService(l, b.out.emit)

	got := svc.Prereq()
	if !got.Ready || len(got.Notices) != 1 {
		t.Fatalf("Prereq = %+v", got)
	}
	if again := svc.Prereq(); !again.Ready || calls != 1 {
		t.Errorf("a second check reconnected: ready=%v calls=%d", again.Ready, calls)
	}
	if published := b.out.named(EventSystemPrereq); len(published) != 2 {
		t.Errorf("published %d, want one per check", len(published))
	}
}

func TestPrereqExplainsAFailureAndRetryWorks(t *testing.T) {
	b := newBackend(t, nil)
	down := true
	l := newLink(context.Background(), func(context.Context) (*engine.Engine, []string, error) {
		if down {
			return nil, nil, errors.New("no Podman machine is running")
		}
		return b.eng, nil, nil
	}, b.out.emit)
	svc := NewSystemService(l, b.out.emit)

	got := svc.Prereq()
	if got.Ready || !strings.Contains(got.Message, "no Podman machine is running") || !strings.Contains(got.Message, "Retry") {
		t.Fatalf("Prereq = %+v", got)
	}

	// Until it connects, the run service says why, and Current is idle.
	runs := NewRunService(context.Background(), l.source, b.catalogue, b.out.emit)
	if _, err := runs.Start(form()); err == nil || !strings.Contains(err.Error(), "not ready") {
		t.Errorf("Start before connecting: %v", err)
	}
	if runs.Current().Status != engine.StatusIdle {
		t.Error("Current is not idle before connecting")
	}

	down = false
	if !svc.Prereq().Ready {
		t.Fatal("Retry did not connect")
	}
	if _, err := runs.End(); err == nil || !strings.Contains(err.Error(), "No scenario is running") {
		t.Errorf("after connecting, End says: %v", err)
	}
}

func TestEndRunIsSafeWithNoConnectionAndEndsARunningScenario(t *testing.T) {
	var none *link
	none.endRun() // no panic

	b := newBackend(t, map[string]string{"s": scenarioYAML("demo", "Demo", "vault", true)})
	l := newLink(context.Background(), func(context.Context) (*engine.Engine, []string, error) { return b.eng, nil, nil }, b.out.emit)
	l.endRun() // not connected

	l.ensure()
	if l.runActive() {
		t.Fatal("a run is active before one was started")
	}
	if _, err := b.runService(context.Background()).Start(form()); err != nil {
		t.Fatal(err)
	}
	if !l.runActive() {
		t.Fatal("the run is not reported as active")
	}
	l.endRun()
	if l.runActive() {
		t.Error("the run is still active after endRun")
	}
	if runs, _ := b.rt.Runs(context.Background()); len(runs) != 0 {
		t.Errorf("runs left behind: %v", runs)
	}
}
