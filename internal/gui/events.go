package gui

import (
	"context"

	"github.com/pixambi/sev1situations/internal/engine"
)

// Event names the frontend listens for (section 3.4). More arrive with the
// features that need them: logs, terminals and the Podman state.
const (
	// EventRunState carries an engine.Snapshot after every change to the run.
	EventRunState = "run:state"
	// EventRunProgress carries an engine.Event for each line of deployment and action progress.
	EventRunProgress = "run:progress"
)

// Emitter sends an event to the frontend. The application supplies it, which
// keeps the services free of Wails and testable without a window.
type Emitter func(name string, data any)

// progressKinds are the engine events that are also sent as progress lines.
var progressKinds = map[engine.EventKind]bool{
	engine.EventDeployProgress: true,
	engine.EventDeployFailed:   true,
	engine.EventActionFailed:   true,
	engine.EventNodeAction:     true,
	engine.EventTeardown:       true,
}

// pumpEvents forwards the engine's events to the frontend until ctx ends.
//
// Every change is announced with a fresh snapshot, never a delta, so the frontend
// holds no state of its own (R-UI-5) and a missed event does no harm. A burst of
// events is answered with one snapshot, taken after the burst.
func pumpEvents(ctx context.Context, eng *engine.Engine, emit Emitter, onEnded func()) {
	events, cancel := eng.Subscribe(engine.DefaultSubscriberBuffer)
	defer cancel()

	for {
		var ev engine.Event
		var ok bool
		select {
		case <-ctx.Done():
			return
		case ev, ok = <-events:
			if !ok {
				return
			}
		}
		for {
			if progressKinds[ev.Kind] {
				emit(EventRunProgress, ev)
			}
			if ev.Kind == engine.EventRunEnded && onEnded != nil {
				onEnded()
			}
			select {
			case ev, ok = <-events:
				if ok {
					continue
				}
			default:
			}
			break
		}
		emit(EventRunState, eng.Snapshot())
	}
}
