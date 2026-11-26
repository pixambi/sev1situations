package fake

import (
	"context"
	"io"
	"sync"
)

// Terminal is the fake's interactive session. It echoes input back as output
// and remembers the last size it was resized to.
type Terminal struct {
	mu         sync.Mutex
	cond       *sync.Cond
	buf        []byte
	closed     bool
	cols, rows uint16
}

func newTerminal() *Terminal {
	t := &Terminal{}
	t.cond = sync.NewCond(&t.mu)
	return t
}

// Write sends input, which the terminal echoes back.
func (t *Terminal) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return 0, io.ErrClosedPipe
	}
	t.buf = append(t.buf, p...)
	t.cond.Broadcast()
	return len(p), nil
}

// Read returns echoed output, blocking until some is available or the
// terminal is closed.
func (t *Terminal) Read(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for len(t.buf) == 0 {
		if t.closed {
			return 0, io.EOF
		}
		t.cond.Wait()
	}
	n := copy(p, t.buf)
	t.buf = t.buf[n:]
	return n, nil
}

// Close ends the session and wakes any blocked reader.
func (t *Terminal) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	t.cond.Broadcast()
	return nil
}

// Resize records the new size.
func (t *Terminal) Resize(_ context.Context, cols, rows uint16) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return io.ErrClosedPipe
	}
	t.cols, t.rows = cols, rows
	return nil
}

// Size returns the last size set by Resize.
func (t *Terminal) Size() (cols, rows uint16) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cols, t.rows
}
