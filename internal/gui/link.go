package gui

import (
	"context"
	"fmt"
	"sync"

	"github.com/pixambi/sev1situations/internal/engine"
	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/runtime"
)

// Connection is the core once it is joined to the container runtime.
type Connection struct {
	// Engine runs the one run.
	Engine *engine.Engine
	// View describes a run's deployment for the Cluster tab. It may be nil.
	View func(ctx context.Context, runID string) product.ClusterView
	// Runtime reads logs and attaches terminals. It may be nil.
	Runtime runtime.Runtime
	// LogStreams lists the logs a node offers. It may be nil.
	LogStreams func(runID, node string) []product.LogStream
	// Notices are sentences describing what start-up cleanup did.
	Notices []string
}

// Connector joins the core to the container runtime and removes what an earlier
// launch left behind. It fails when the runtime cannot be reached.
type Connector func(ctx context.Context) (*Connection, error)

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

	mu    sync.Mutex
	conn  *Connection
	ended []func()
}

// onRunEnded registers something to do when a run ends, such as closing its terminals.
func (l *link) onRunEnded(f func()) {
	l.mu.Lock()
	l.ended = append(l.ended, f)
	l.mu.Unlock()
}

// runEnded runs the registered hooks.
func (l *link) runEnded() {
	l.mu.Lock()
	hooks := append([]func(){}, l.ended...)
	l.mu.Unlock()
	for _, f := range hooks {
		f()
	}
}

func newLink(ctx context.Context, connect Connector, emit Emitter) *link {
	return &link{ctx: ctx, connect: connect, emit: emit}
}

// current returns the engine, or nil if there is no connection yet.
func (l *link) current() *engine.Engine {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conn == nil {
		return nil
	}
	return l.conn.Engine
}

// connection returns the connection, or nil if there is none yet.
func (l *link) connection() *Connection {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.conn
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
	if l.conn != nil {
		return PrereqStatus{Ready: true, Notices: l.conn.Notices}
	}
	conn, err := l.connect(l.ctx)
	if err != nil {
		return PrereqStatus{Message: fmt.Sprintf("Podman is not ready: %v. Install Podman, then run \"podman machine init\" and \"podman machine start\", and press Retry.", err)}
	}
	l.conn = conn
	go pumpEvents(l.ctx, conn.Engine, l.emit, l.runEnded)
	return PrereqStatus{Ready: true, Notices: conn.Notices}
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
