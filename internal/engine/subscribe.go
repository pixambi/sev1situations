package engine

import "sync"

// DefaultSubscriberBuffer is the channel size Subscribe uses when asked for none.
const DefaultSubscriberBuffer = 256

// Subscribe returns a channel that receives every event from now on, for the
// server to forward to the UI (section 3.4), and a function that ends the
// subscription and closes the channel.
//
// Delivery never blocks the engine. A subscriber that falls behind and fills
// its buffer misses events. Each event carries its run ID and a sequence number
// that counts from 1 within the run, so a consumer sees the gap and reads
// Snapshot to catch up. Subscriptions outlive runs.
func (e *Engine) Subscribe(buffer int) (<-chan Event, func()) {
	if buffer <= 0 {
		buffer = DefaultSubscriberBuffer
	}
	s := &subscriber{ch: make(chan Event, buffer)}

	e.mu.Lock()
	e.subs = append(e.subs, s)
	e.mu.Unlock()

	return s.ch, func() {
		e.mu.Lock()
		for i, have := range e.subs {
			if have == s {
				e.subs = append(e.subs[:i], e.subs[i+1:]...)
				break
			}
		}
		e.mu.Unlock()
		s.close()
	}
}

type subscriber struct {
	mu     sync.Mutex
	ch     chan Event
	closed bool
}

// send delivers an event if there is room. It reports whether it did.
func (s *subscriber) send(ev Event) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return true
	}
	select {
	case s.ch <- ev:
		return true
	default:
		return false
	}
}

func (s *subscriber) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.ch)
	}
}
