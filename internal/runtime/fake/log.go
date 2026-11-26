package fake

import (
	"context"
	"io"
	"strings"
	"sync"

	"github.com/pixambi/sev1situations/internal/runtime"
)

// logBuffer holds a container's log lines and fans new ones out to followers.
type logBuffer struct {
	mu     sync.Mutex
	lines  []string
	subs   map[*follower]struct{}
	closed bool
}

type follower struct {
	lines chan string
	done  chan struct{}
}

func newLogBuffer() *logBuffer { return &logBuffer{subs: map[*follower]struct{}{}} }

func (b *logBuffer) append(line string) {
	b.mu.Lock()
	b.lines = append(b.lines, line)
	subs := make([]*follower, 0, len(b.subs))
	for s := range b.subs {
		subs = append(subs, s)
	}
	b.mu.Unlock()

	for _, s := range subs {
		select {
		case s.lines <- line:
		case <-s.done:
		}
	}
}

// close ends every follower, as when the container is removed.
func (b *logBuffer) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for s := range b.subs {
		close(s.done)
	}
	b.subs = map[*follower]struct{}{}
}

// reader returns the existing lines and, with Follow, the lines that arrive
// until the context ends, the reader is closed or the container is removed.
func (b *logBuffer) reader(ctx context.Context, o runtime.LogOptions) io.ReadCloser {
	b.mu.Lock()
	existing := append([]string(nil), b.lines...)
	if o.Tail > 0 && len(existing) > o.Tail {
		existing = existing[len(existing)-o.Tail:]
	}
	if !o.Follow || b.closed {
		b.mu.Unlock()
		return io.NopCloser(strings.NewReader(join(existing)))
	}
	f := &follower{lines: make(chan string, 64), done: make(chan struct{})}
	b.subs[f] = struct{}{}
	b.mu.Unlock()

	pr, pw := io.Pipe()
	go func() {
		defer func() { _ = pw.Close() }()
		if _, err := io.WriteString(pw, join(existing)); err != nil {
			b.unsubscribe(f)
			return
		}
		for {
			select {
			case line := <-f.lines:
				if _, err := io.WriteString(pw, line+"\n"); err != nil {
					b.unsubscribe(f)
					return
				}
			case <-f.done:
				return
			case <-ctx.Done():
				b.unsubscribe(f)
				return
			}
		}
	}()
	return pr
}

func (b *logBuffer) unsubscribe(f *follower) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.subs[f]; ok {
		delete(b.subs, f)
		close(f.done)
	}
}

func join(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}
