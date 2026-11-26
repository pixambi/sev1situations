package runtime

import (
	"errors"
	"net"
	"strconv"
	"sync"
	"testing"
)

func TestAllocateSkipsUsedAndBusyPorts(t *testing.T) {
	busy := map[int]bool{100: true, 102: true}
	a := newPortAllocator(100, 105, func(p int) bool { return !busy[p] })

	var got []int
	for i := 0; i < 4; i++ {
		p, err := a.Allocate()
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, p)
	}
	want := []int{101, 103, 104, 105}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("allocated %v, want %v", got, want)
		}
	}
}

func TestAllocateExhaustionAndRelease(t *testing.T) {
	a := newPortAllocator(10, 11, func(int) bool { return true })
	p1, _ := a.Allocate()
	p2, _ := a.Allocate()
	if p1 == p2 {
		t.Fatalf("handed out %d twice", p1)
	}
	if _, err := a.Allocate(); !errors.Is(err, ErrNoFreePort) {
		t.Fatalf("third Allocate = %v, want ErrNoFreePort", err)
	}

	a.Release(p1)
	got, err := a.Allocate()
	if err != nil || got != p1 {
		t.Errorf("after Release(%d), Allocate = %d, %v; want the released port", p1, got, err)
	}
	a.Release(9999) // releasing a port that was never allocated is harmless
}

func TestAllocateWrapsAround(t *testing.T) {
	a := newPortAllocator(10, 12, func(int) bool { return true })
	for range 3 {
		if _, err := a.Allocate(); err != nil {
			t.Fatal(err)
		}
	}
	a.Release(10)
	if p, err := a.Allocate(); err != nil || p != 10 {
		t.Errorf("Allocate = %d, %v; want it to wrap around to 10", p, err)
	}
}

func TestAllocateIsSafeForConcurrentUse(t *testing.T) {
	a := newPortAllocator(1000, 1999, func(int) bool { return true })
	var mu sync.Mutex
	seen := map[int]bool{}
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := a.Allocate()
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if seen[p] {
				t.Errorf("port %d handed out twice", p)
			}
			seen[p] = true
		}()
	}
	wg.Wait()
}

func TestLoopbackFreeSeesARealListener(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if loopbackFree(port) {
		t.Errorf("port %s reported free while a listener holds it", strconv.Itoa(port))
	}
	_ = l.Close()
	if !loopbackFree(port) {
		t.Errorf("port %d reported busy after the listener closed", port)
	}
}

func TestDefaultRange(t *testing.T) {
	p, err := NewPortAllocator().Allocate()
	if err != nil {
		t.Fatal(err)
	}
	if p < PortRangeStart || p > PortRangeEnd {
		t.Errorf("port %d is outside %d-%d", p, PortRangeStart, PortRangeEnd)
	}
}
