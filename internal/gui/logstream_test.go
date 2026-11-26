package gui

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func collect(t *testing.T, in io.Reader) []string {
	t.Helper()
	var got []string
	if err := readLines(in, func(l string) { got = append(got, l) }); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestReadLinesSplitsOnLineEndingsAndKeepsAFinalLineWithNoEnding(t *testing.T) {
	got := collect(t, strings.NewReader("one\ntwo\r\nthree\n\nlast"))
	want := []string{"one", "two", "three", "", "last"}
	if strings.Join(got, "|") != strings.Join(want, "|") || len(got) != len(want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReadLinesCutsAHugeLineAndCarriesOn(t *testing.T) {
	huge := strings.Repeat("x", 5*maxLineBytes)
	got := collect(t, strings.NewReader("before\n"+huge+"\nafter\n"))
	if len(got) != 3 || got[0] != "before" || got[2] != "after" {
		t.Fatalf("got %d lines: %.40q", len(got), got)
	}
	if want := maxLineBytes + len(truncatedMark); len(got[1]) != want || !strings.HasSuffix(got[1], truncatedMark) {
		t.Errorf("the long line is %d bytes, want %d ending in the mark", len(got[1]), want)
	}
}

func TestReadLinesReplacesInvalidUTF8(t *testing.T) {
	got := collect(t, strings.NewReader("ok \xff\xfe bad\n"))
	if len(got) != 1 || !strings.Contains(got[0], "�") || strings.Contains(got[0], "\xff") {
		t.Errorf("got %q", got)
	}
}

func TestReadLinesReportsAReadFailureAfterTheLinesBeforeIt(t *testing.T) {
	boom := errors.New("connection reset")
	r := io.MultiReader(strings.NewReader("a\nb"), errReader{boom})
	var got []string
	err := readLines(r, func(l string) { got = append(got, l) })
	if !errors.Is(err, boom) || strings.Join(got, ",") != "a,b" {
		t.Errorf("err = %v, got %q", err, got)
	}
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

// pipe is a reader the test feeds, and that unblocks when closed, like a log stream.
func pipe() (*io.PipeWriter, io.ReadCloser) {
	pr, pw := io.Pipe()
	return pw, pr
}

type batches struct {
	mu sync.Mutex
	b  []LogBatch
}

func (b *batches) add(x LogBatch) { b.mu.Lock(); b.b = append(b.b, x); b.mu.Unlock() }
func (b *batches) all() []LogBatch {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]LogBatch(nil), b.b...)
}

func TestStreamLinesBatchesLinesAndMarksTheEnd(t *testing.T) {
	pw, pr := pipe()
	var got batches
	done := make(chan struct{})
	go func() { streamLines(context.Background(), pr, got.add); close(done) }()

	_, _ = io.WriteString(pw, "one\ntwo\nthree\n")
	eventually(t, "the first batch", func() bool { return len(got.all()) > 0 })
	_ = pw.Close()
	<-done

	all := got.all()
	var lines []string
	for _, b := range all {
		lines = append(lines, b.Lines...)
	}
	if strings.Join(lines, ",") != "one,two,three" {
		t.Errorf("lines = %v", lines)
	}
	if last := all[len(all)-1]; !last.Ended || last.Reason != "" {
		t.Errorf("last batch = %+v, want a clean end", last)
	}
	if len(all) > 2 {
		t.Errorf("%d batches for three lines written together", len(all))
	}
}

func TestStreamLinesSendsAFullBatchWithoutWaitingForTheTimer(t *testing.T) {
	pw, pr := pipe()
	var got batches
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go streamLines(ctx, pr, got.add)

	go func() {
		for i := 0; i < 2*batchMaxLines; i++ {
			_, _ = io.WriteString(pw, "line\n")
		}
	}()
	eventually(t, "two full batches", func() bool {
		n := 0
		for _, b := range got.all() {
			n += len(b.Lines)
		}
		return n >= 2*batchMaxLines
	})
	for _, b := range got.all() {
		if len(b.Lines) > batchMaxLines {
			t.Errorf("a batch of %d lines exceeds the %d cap", len(b.Lines), batchMaxLines)
		}
	}
}

func TestStreamLinesStopsAndStaysSilentWhenTheInstructorCloses(t *testing.T) {
	pw, pr := pipe()
	var got batches
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { streamLines(ctx, pr, got.add); close(done) }()

	_, _ = io.WriteString(pw, "a\n")
	eventually(t, "a batch", func() bool { return len(got.all()) > 0 })
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream kept running after it was closed")
	}
	for _, b := range got.all() {
		if b.Ended {
			t.Errorf("a closed stream announced its own end: %+v", b)
		}
	}
}

func TestStreamLinesSaysWhyAStreamFailed(t *testing.T) {
	var got batches
	r := io.NopCloser(io.MultiReader(strings.NewReader("a\n"), errReader{errors.New("the node stopped")}))
	streamLines(context.Background(), r, got.add)

	all := got.all()
	if last := all[len(all)-1]; !last.Ended || last.Reason != "the node stopped" {
		t.Errorf("last batch = %+v", last)
	}
}
