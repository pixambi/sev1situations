package gui

import (
	"context"
	"fmt"
	"sync"

	"github.com/pixambi/sev1situations/internal/engine"
)

// Connector joins the core to the container runtime and removes what an earlier
// launch left behind. It returns the engine and sentences describing the cleanup.
// It fails when the runtime cannot be reached.
type Connector func(ctx context.Context) (*engine.Engine, []string, error)

// EngineSource returns the engine, or an error that tells the instructor why there
// is none yet.
type EngineSource func() (*engine.Engine, error)

// link holds the connection to the core. The window opens before Podman is known
// to be ready, so the instructor can see what is wrong and try again (R-APP-2). The
// engine exists once a connection has succeeded, and the event pump starts with it.
type link struct {
	ctx     context.Context
	connect Connector
	emit    Emitter

	mu      sync.Mutex
	eng     *engine.Engine
	notices []string
}

func newLink(ctx context.Context, connect Connector, emit Emitter) *link {
	return &link{ctx: ctx, connect: connect, emit: emit}
}

// current returns the engine, or nil if there is no connection yet.
func (l *link) current() *engine.Engine {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.eng
}

// source is the EngineSource for the services.
func (l *link) source() (*engine.Engine, error) {
	if eng := l.current(); eng != nil {
		return eng, nil
	}
	return nil, notice("The container runtime is not ready. Check Podman, then press Retry.")
}

// ensure connects if there is no connection yet and reports the state.
func (l *link) ensure() PrereqStatus {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.eng != nil {
		return PrereqStatus{Ready: true, Notices: l.notices}
	}
	eng, notices, err := l.connect(l.ctx)
	if err != nil {
		return PrereqStatus{Message: fmt.Sprintf("Podman is not ready: %v. Install Podman, then run \"podman machine init\" and \"podman machine start\", and press Retry.", err)}
	}
	l.eng, l.notices = eng, notices
	go pumpEvents(l.ctx, eng, l.emit)
	return PrereqStatus{Ready: true, Notices: notices}
}

// runActive reports whether a run exists, deploying or not.
func (l *link) runActive() bool {
	eng := l.current()
	return eng != nil && eng.Snapshot().Status != engine.StatusIdle
}

// endRun ends the run, if there is one, and waits for its containers to go. It is
// safe to call with no connection or no run.
func (l *link) endRun() {
	if l == nil || !l.runActive() {
		return
	}
	_, _ = l.current().End(context.Background())
}
