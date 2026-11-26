package engine

import (
	"testing"
	"time"
)

func recv(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("the channel was closed")
		}
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("no event arrived")
		return Event{}
	}
}

func TestSubscribersReceiveEventsInOrder(t *testing.T) {
	r := newRig(t, testScenario)
	a, cancelA := r.e.Subscribe(0)
	b, cancelB := r.e.Subscribe(0)
	defer cancelA()
	defer cancelB()

	r.start()

	want := []EventKind{EventRunStarted, EventDeployProgress, EventDeployProgress, EventDeployComplete, EventStepActive}
	for i, k := range want {
		for name, ch := range map[string]<-chan Event{"a": a, "b": b} {
			ev := recv(t, ch)
			if ev.Kind != k || ev.Seq != i+1 || ev.RunID != "run1" {
				t.Errorf("subscriber %s event %d = %+v, want %s numbered %d for run1", name, i, ev, k, i+1)
			}
		}
	}
}

func TestEventsWithNoSubscribersAndNoSink(t *testing.T) {
	// With nobody subscribed, and no OnEvent sink, nothing may go wrong.
	r := newRig(t, testScenario)
	r.e.deps.OnEvent = nil
	r.start()
	r.ok(r.e.Apply(ctx))
}

func TestASlowSubscriberNeverBlocksTheEngine(t *testing.T) {
	r := newRig(t, testScenario)
	slow, cancel := r.e.Subscribe(1) // a buffer of one that nobody reads
	defer cancel()
	fast, cancelFast := r.e.Subscribe(1000)
	defer cancelFast()

	done := make(chan struct{})
	go func() {
		defer close(done)
		r.start()
		r.ok(r.e.Apply(ctx))
		r.ok(r.e.Continue(ctx))
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the engine stalled behind a subscriber that was not reading")
	}

	if got := len(slow); got != 1 {
		t.Errorf("the slow subscriber holds %d events, want its one-event buffer", got)
	}
	// A subscriber that reads keeps up, and the gap in the slow one is visible by Seq.
	first := recv(t, fast)
	if first.Seq != 1 {
		t.Errorf("the fast subscriber's first event has Seq %d", first.Seq)
	}
	snap := r.e.Snapshot()
	if len(fast) != len(snap.Events)-1 {
		t.Errorf("the fast subscriber has %d more events, the run recorded %d in all", len(fast), len(snap.Events))
	}
}

func TestCancelClosesTheChannelAndStopsDelivery(t *testing.T) {
	r := newRig(t, testScenario)
	ch, cancel := r.e.Subscribe(0)
	r.start()
	cancel()
	cancel() // cancelling twice is harmless

	for range ch { // drain what arrived before the cancel; the loop ends because the channel is closed
	}
	r.ok(r.e.Apply(ctx)) // events after the cancel must not panic on a closed channel
}

func TestASubscriberMayReadTheSnapshotWhileEventsFlow(t *testing.T) {
	r := newRig(t, testScenario)
	ch, cancel := r.e.Subscribe(0)
	defer cancel()

	stop := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for {
			select {
			case ev := <-ch:
				_ = r.e.Snapshot() // calling back in must not deadlock
				_ = ev
			case <-stop:
				return
			}
		}
	}()

	r.start()
	r.ok(r.e.Apply(ctx))
	r.ok(r.e.Continue(ctx))
	close(stop)
	<-finished
}

func TestEventsCarryTheirRunAcrossRuns(t *testing.T) {
	r := newRig(t, testScenario)
	ch, cancel := r.e.Subscribe(0)
	defer cancel()

	r.start()
	if _, err := r.e.End(ctx); err != nil {
		t.Fatal(err)
	}
	r.start()

	seen := map[string]int{}
	for i := 0; i < 2; i++ { // the first event of each run
		for ev := range ch {
			if ev.Seq == 1 {
				seen[ev.RunID]++
				break
			}
		}
	}
	if seen["run1"] != 1 || seen["run2"] != 1 {
		t.Errorf("first events seen per run = %v, want a Seq 1 for run1 and for run2 on the same subscription", seen)
	}
}
