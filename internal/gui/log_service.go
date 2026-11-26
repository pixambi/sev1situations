package gui

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"sync"

	"github.com/pixambi/sev1situations/internal/runtime"
)

// maxOpenStreams limits how many logs can be followed at once.
const maxOpenStreams = 8

// initialTail is how many of the most recent lines a stream starts with.
const initialTail = 200

// streamIDPattern is what a stream ID may look like. The frontend chooses it, so
// that it can listen for the stream's events before the first line is sent.
var streamIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// StreamInfo is one log a node offers.
type StreamInfo struct {
	ID    string
	Label string
}

// LogService follows a node's logs (3.4). Each open stream sends its lines as
// batched log:<id> events until it ends or is closed.
type LogService struct {
	link *link
	ctx  context.Context
	emit Emitter

	mu      sync.Mutex
	streams map[string]context.CancelFunc
}

// NewLogService returns a service over the connection. ctx ends when the application does.
func NewLogService(ctx context.Context, l *link, emit Emitter) *LogService {
	return &LogService{link: l, ctx: ctx, emit: emit, streams: map[string]context.CancelFunc{}}
}

// Streams lists the logs a node of the current run offers, from its product.
func (s *LogService) Streams(node string) ([]StreamInfo, error) {
	conn, runID, err := s.current()
	if err != nil {
		return nil, err
	}
	if conn.LogStreams == nil {
		return []StreamInfo{}, nil
	}
	out := []StreamInfo{}
	for _, st := range conn.LogStreams(runID, node) {
		out = append(out, StreamInfo{ID: st.ID, Label: st.Label})
	}
	return out, nil
}

// Open starts following one of a node's logs under the given stream ID.
func (s *LogService) Open(streamID, node, stream string) error {
	if !streamIDPattern.MatchString(streamID) {
		return notice("That is not a valid stream ID.")
	}
	conn, runID, err := s.current()
	if err != nil {
		return err
	}
	if conn.Runtime == nil || conn.LogStreams == nil {
		return notice("This build cannot read logs.")
	}
	containerID, err := conn.Engine.ContainerOf(node)
	if err != nil {
		return nodeError(node, "log", err)
	}
	var def *StreamInfo
	var file string
	for _, st := range conn.LogStreams(runID, node) {
		if st.ID == stream {
			def, file = &StreamInfo{ID: st.ID, Label: st.Label}, st.File
		}
	}
	if def == nil {
		return notice(fmt.Sprintf("%s has no log called %q.", node, stream))
	}

	s.mu.Lock()
	if _, taken := s.streams[streamID]; taken {
		s.mu.Unlock()
		return notice("That stream is already open.")
	}
	if len(s.streams) >= maxOpenStreams {
		s.mu.Unlock()
		return notice(fmt.Sprintf("At most %d logs can be open at once. Close one first.", maxOpenStreams))
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.streams[streamID] = cancel
	s.mu.Unlock()

	r, err := openReader(ctx, conn.Runtime, containerID, file)
	if err != nil {
		s.forget(streamID)
		return fmt.Errorf("open the %s log of %s: %w", def.Label, node, err)
	}
	go func() {
		defer s.forget(streamID)
		streamLines(ctx, r, func(b LogBatch) { s.emit("log:"+streamID, b) })
	}()
	return nil
}

// Close stops following a stream. Closing one that is already gone is fine.
func (s *LogService) Close(streamID string) {
	s.mu.Lock()
	cancel := s.streams[streamID]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// CloseAll stops every stream. It runs when the run ends.
func (s *LogService) CloseAll() {
	s.mu.Lock()
	all := make([]context.CancelFunc, 0, len(s.streams))
	for _, cancel := range s.streams {
		all = append(all, cancel)
	}
	s.mu.Unlock()
	for _, cancel := range all {
		cancel()
	}
}

func (s *LogService) forget(streamID string) {
	s.mu.Lock()
	if cancel, ok := s.streams[streamID]; ok {
		cancel()
		delete(s.streams, streamID)
	}
	s.mu.Unlock()
}

func (s *LogService) current() (*Connection, string, error) {
	conn := s.link.connection()
	if conn == nil {
		return nil, "", notice("The container runtime is not ready. Check Podman, then press Retry.")
	}
	snap := conn.Engine.Snapshot()
	if snap.RunID == "" {
		return nil, "", plain(errNoRun)
	}
	return conn, snap.RunID, nil
}

// openReader follows a container's own output, or, for a log file inside the
// container, follows the file the way tail -F does, which waits for it to appear.
func openReader(ctx context.Context, rt runtime.Runtime, container, file string) (io.ReadCloser, error) {
	if file == "" {
		return rt.Logs(ctx, container, runtime.LogOptions{Follow: true, Tail: initialTail})
	}
	t, err := rt.Attach(ctx, container, []string{"tail", "-n", strconv.Itoa(initialTail), "-F", file})
	if err != nil {
		return nil, err
	}
	return t, nil
}
