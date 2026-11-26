package gui

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/pixambi/sev1situations/internal/runtime"
)

// Limits on terminals.
const (
	// maxTerminals is how many can be open at once.
	maxTerminals = 8
	// maxInput is the most one Input call may send, so a paste cannot be unbounded.
	maxInput = 256 * 1024
	// outputChunk is the most bytes read from a terminal at once, and so the most sent in one event.
	outputChunk = 16 * 1024
	// outputInterval is how long output is gathered before it is sent. A fast stream, such
	// as cat on a big file, costs one event per interval and not one per read (R-TERM-5).
	outputInterval = 16 * time.Millisecond
	// maxCols and maxRows bound a reported size.
	maxCols, maxRows = 1000, 1000
)

// shell is what a terminal runs: bash if the container has it, otherwise sh.
var shell = []string{"/bin/sh", "-c", "command -v bash >/dev/null 2>&1 && exec bash -l || exec sh"}

// TerminalExit is what terminal:exit:<id> carries when a session ends.
type TerminalExit struct {
	// Reason says why, when it was not the shell exiting normally.
	Reason string
}

// TerminalService bridges terminals in the nodes' containers to the window (3.4,
// R-TERM-2). Output travels as base64 in terminal:out:<id> events, because a
// chunk can end in the middle of a UTF-8 character and an event is a JSON string.
// Input and resizes come back through the methods.
//
// A terminal opens only into a container of the current run, because the node
// name is resolved through the engine (R-TERM-4), and ends with the run (R-TERM-3).
type TerminalService struct {
	link *link
	ctx  context.Context
	emit Emitter

	mu   sync.Mutex
	open map[string]*session
}

type session struct {
	term   runtime.Terminal
	cancel context.CancelFunc
}

// NewTerminalService returns a service over the connection. ctx ends when the application does.
func NewTerminalService(ctx context.Context, l *link, emit Emitter) *TerminalService {
	s := &TerminalService{link: l, ctx: ctx, emit: emit, open: map[string]*session{}}
	l.onRunEnded(s.CloseAll)
	return s
}

// Open starts a shell on a node under the given terminal ID, sized as given. The
// frontend chooses the ID so it can listen before the first output.
func (s *TerminalService) Open(id, node string, cols, rows int) error {
	if !streamIDPattern.MatchString(id) {
		return notice("That is not a valid terminal ID.")
	}
	if err := checkSize(cols, rows); err != nil {
		return err
	}
	conn := s.link.connection()
	if conn == nil {
		return notice("The container runtime is not ready. Check Podman, then press Retry.")
	}
	if conn.Runtime == nil {
		return notice("This build cannot open terminals.")
	}
	container, err := conn.Engine.ContainerOf(node)
	if err != nil {
		return nodeError(node, "terminal", err)
	}

	s.mu.Lock()
	if _, taken := s.open[id]; taken {
		s.mu.Unlock()
		return notice("That terminal is already open.")
	}
	if len(s.open) >= maxTerminals {
		s.mu.Unlock()
		return notice(fmt.Sprintf("At most %d terminals can be open at once. Close one first.", maxTerminals))
	}
	ctx, cancel := context.WithCancel(s.ctx)
	sess := &session{cancel: cancel}
	s.open[id] = sess // reserved, so a second Open of the same ID cannot slip in while this one attaches
	s.mu.Unlock()

	term, err := conn.Runtime.Attach(ctx, container, shell)
	if err != nil {
		s.forget(id)
		return fmt.Errorf("open a terminal on %s: %w", node, err)
	}
	s.mu.Lock()
	sess.term = term
	s.mu.Unlock()
	_ = term.Resize(ctx, uint16(cols), uint16(rows)) // a failed first resize is corrected by the next fit

	go s.pump(ctx, id, term)
	return nil
}

// Input sends what the instructor typed, or pasted, to the shell.
func (s *TerminalService) Input(id, data string) error {
	if len(data) > maxInput {
		return notice("That is too much to send at once.")
	}
	term, ok := s.terminal(id)
	if !ok {
		return notice("That terminal is not open.")
	}
	if _, err := io.WriteString(term, data); err != nil {
		return fmt.Errorf("send input to the terminal: %w", err)
	}
	return nil
}

// Resize tells the shell its window's new size, in character cells.
func (s *TerminalService) Resize(id string, cols, rows int) error {
	if err := checkSize(cols, rows); err != nil {
		return err
	}
	term, ok := s.terminal(id)
	if !ok {
		return notice("That terminal is not open.")
	}
	return term.Resize(s.ctx, uint16(cols), uint16(rows))
}

// Close ends a terminal. Closing one that is already gone is fine.
func (s *TerminalService) Close(id string) {
	s.mu.Lock()
	sess := s.open[id]
	s.mu.Unlock()
	if sess != nil {
		sess.cancel()
	}
}

// CloseAll ends every terminal. It runs when the run ends (R-TERM-3).
func (s *TerminalService) CloseAll() {
	s.mu.Lock()
	all := make([]*session, 0, len(s.open))
	for _, sess := range s.open {
		all = append(all, sess)
	}
	s.mu.Unlock()
	for _, sess := range all {
		sess.cancel()
	}
}

func (s *TerminalService) terminal(id string) (runtime.Terminal, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.open[id]
	if !ok || sess.term == nil {
		return nil, false
	}
	return sess.term, true
}

func (s *TerminalService) forget(id string) {
	s.mu.Lock()
	if sess, ok := s.open[id]; ok {
		sess.cancel()
		delete(s.open, id)
	}
	s.mu.Unlock()
}

// pump reads the terminal and sends its output, batched, until it ends. The exit
// event follows the last output, unless the instructor closed the terminal.
func (s *TerminalService) pump(ctx context.Context, id string, term runtime.Terminal) {
	defer s.forget(id)
	go func() {
		<-ctx.Done()
		_ = term.Close()
	}()

	chunks := make(chan []byte, 64)
	readErr := make(chan error, 1)
	go func() {
		defer close(chunks)
		buf := make([]byte, outputChunk)
		for {
			n, err := term.Read(buf)
			if n > 0 {
				select {
				case chunks <- append([]byte(nil), buf[:n]...):
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				readErr <- err
				return
			}
		}
	}()

	tick := time.NewTicker(outputInterval)
	defer tick.Stop()
	var pending []byte
	send := func() {
		if len(pending) > 0 {
			s.emit("terminal:out:"+id, base64.StdEncoding.EncodeToString(pending))
			pending = nil
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case c, ok := <-chunks:
			if !ok {
				send()
				if ctx.Err() != nil {
					return
				}
				exit := TerminalExit{}
				if err := <-readErr; err != nil && !errors.Is(err, io.EOF) {
					exit.Reason = err.Error()
				}
				s.emit("terminal:exit:"+id, exit)
				return
			}
			pending = append(pending, c...)
			if len(pending) >= outputChunk {
				send()
			}
		case <-tick.C:
			send()
		}
	}
}

func checkSize(cols, rows int) error {
	if cols < 1 || rows < 1 || cols > maxCols || rows > maxRows {
		return notice("That terminal size is not usable.")
	}
	return nil
}
