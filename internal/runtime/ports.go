package runtime

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
)

// Default range for host ports published by a run (R-APP-4).
const (
	// PortRangeStart is the first port the allocator hands out.
	PortRangeStart = 19000
	// PortRangeEnd is the last port the allocator hands out.
	PortRangeEnd = 29999
)

// ErrNoFreePort means every port in the range is in use.
var ErrNoFreePort = errors.New("no free host port")

// PortAllocator hands out host ports for published services. Each port is
// checked free on the loopback interface before it is returned, and is never
// returned twice until it is released. The check cannot reserve the port: another
// process may take it before the container binds it, in which case starting
// the container fails and the caller can allocate again.
type PortAllocator struct {
	mu       sync.Mutex
	min, max int
	next     int
	used     map[int]bool
	isFree   func(port int) bool
}

// NewPortAllocator returns an allocator over the default range.
func NewPortAllocator() *PortAllocator {
	return newPortAllocator(PortRangeStart, PortRangeEnd, loopbackFree)
}

func newPortAllocator(minPort, maxPort int, isFree func(int) bool) *PortAllocator {
	return &PortAllocator{min: minPort, max: maxPort, next: minPort, used: map[int]bool{}, isFree: isFree}
}

// Allocate returns a free port, trying upward from the last one handed out and
// wrapping around once.
func (a *PortAllocator) Allocate() (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	size := a.max - a.min + 1
	for i := 0; i < size; i++ {
		port := a.min + (a.next-a.min+i)%size
		if a.used[port] || !a.isFree(port) {
			continue
		}
		a.used[port] = true
		a.next = port + 1
		if a.next > a.max {
			a.next = a.min
		}
		return port, nil
	}
	return 0, fmt.Errorf("%w in %d-%d", ErrNoFreePort, a.min, a.max)
}

// Release makes a port available again. Releasing a port that was not
// allocated is not an error.
func (a *PortAllocator) Release(port int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.used, port)
}

// loopbackFree reports whether nothing is listening on the port on 127.0.0.1.
func loopbackFree(port int) bool {
	l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}
