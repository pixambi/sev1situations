package gui

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"time"
)

// Limits that keep a busy or hostile log from overwhelming the window.
const (
	// batchInterval is how long lines are gathered before they are sent.
	batchInterval = 100 * time.Millisecond
	// batchMaxLines sends a batch early when this many lines have gathered.
	batchMaxLines = 200
	// maxLineBytes is the longest line sent. The rest of a longer line is dropped.
	maxLineBytes = 8 * 1024
)

const truncatedMark = " [line truncated]"

// LogBatch is what a log event carries: lines, in order, and, on the last one,
// that the stream has ended and why. Lines are batched so a busy log costs few
// events (3.4).
type LogBatch struct {
	Lines []string
	// Ended is true on the final batch. The stream is not read after it.
	Ended bool
	// Reason says why it ended when that was not the instructor closing it. It is
	// empty when the log simply ran out, as when the node was removed.
	Reason string
}

// readLines calls emit for each line of r, without its line ending. A line longer
// than maxLineBytes is cut and marked, and the rest of it is skipped, so one huge
// line cannot use unbounded memory. Invalid UTF-8 is replaced, so the frontend
// always receives text it can show. It returns when r ends or fails.
func readLines(r io.Reader, emit func(string)) error {
	br := bufio.NewReaderSize(r, 4096)
	var line []byte
	skipping := false
	for {
		part, isPrefix, err := br.ReadLine()
		if len(part) > 0 && !skipping {
			if room := maxLineBytes - len(line); len(part) > room {
				line = append(line, part[:room]...)
				line = append(line, truncatedMark...)
				skipping = true
			} else {
				line = append(line, part...)
			}
		}
		if err != nil {
			if len(line) > 0 {
				emit(clean(line))
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if !isPrefix {
			emit(clean(line))
			line, skipping = line[:0], false
		}
	}
}

// clean makes a line safe to send: valid UTF-8, with a stray carriage return
// from a terminal removed.
func clean(b []byte) string {
	return strings.TrimRight(strings.ToValidUTF8(string(b), "�"), "\r")
}

// streamLines reads r until it ends or ctx is done and sends its lines in
// batches. The last batch says the stream ended. If ctx ends first, r is closed to
// unblock the read, and nothing more is sent: the instructor closed the stream.
func streamLines(ctx context.Context, r io.ReadCloser, flush func(LogBatch)) {
	lines := make(chan string, 4*batchMaxLines)
	readErr := make(chan error, 1)

	go func() {
		<-ctx.Done()
		_ = r.Close()
	}()
	go func() {
		err := readLines(r, func(l string) {
			select {
			case lines <- l:
			case <-ctx.Done():
			}
		})
		readErr <- err
		close(lines)
	}()

	tick := time.NewTicker(batchInterval)
	defer tick.Stop()
	var pending []string
	send := func() {
		if len(pending) > 0 {
			flush(LogBatch{Lines: pending})
			pending = nil
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case l, ok := <-lines:
			if !ok {
				send()
				if ctx.Err() != nil {
					return
				}
				end := LogBatch{Lines: []string{}, Ended: true}
				if err := <-readErr; err != nil {
					end.Reason = err.Error()
				}
				flush(end)
				return
			}
			pending = append(pending, l)
			if len(pending) >= batchMaxLines {
				send()
			}
		case <-tick.C:
			send()
		}
	}
}
