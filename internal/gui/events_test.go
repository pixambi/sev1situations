package gui

import (
	"context"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/engine"
)

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestTheRunsEventsReachTheFrontend(t *testing.T) {
	b, svc := twoStep(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { pumpEvents(ctx, b.eng, b.out.emit, nil); close(done) }()
	// Wait until the pump is subscribed, or the first events are missed.
	eventually(t, "the pump to subscribe", func() bool { _, stop := b.eng.Subscribe(1); stop(); return true })
	time.Sleep(20 * time.Millisecond)

	if _, err := svc.Start(form()); err != nil {
		t.Fatal(err)
	}

	// Progress lines arrive as they are produced.
	eventually(t, "the deployment progress", func() bool {
		for _, d := range b.out.named(EventRunProgress) {
			if ev, ok := d.(engine.Event); ok && ev.Message == "the cluster is healthy" {
				return true
			}
		}
		return false
	})
	// The last state published is the current one, so a missed event cannot leave the page behind.
	eventually(t, "the final state", func() bool {
		states := b.out.named(EventRunState)
		if len(states) == 0 {
			return false
		}
		last, ok := states[len(states)-1].(engine.Snapshot)
		return ok && last.Status == engine.StatusActive && len(last.Events) == len(b.eng.Snapshot().Events)
	})

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the pump kept running after its context ended")
	}
}

func TestOnlyProgressEventsAreSentAsProgress(t *testing.T) {
	b, svc := twoStep(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go pumpEvents(ctx, b.eng, b.out.emit, nil)
	time.Sleep(20 * time.Millisecond)

	if _, err := svc.Start(form()); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the run to start", func() bool { return len(b.out.named(EventRunState)) > 0 })
	for _, d := range b.out.named(EventRunProgress) {
		if ev := d.(engine.Event); !progressKinds[ev.Kind] {
			t.Errorf("%s was sent as progress", ev.Kind)
		}
	}
}
