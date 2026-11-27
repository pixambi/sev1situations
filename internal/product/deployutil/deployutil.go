// Package deployutil holds the small pieces every product's deployment needs: waiting for a
// condition with a limit, describing a command that failed, and putting a file into a container.
// Waiting always has a limit, and one that runs out says which container and shows the end of its
// log (section 7).
package deployutil

import (
	"context"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/pixambi/sev1situations/internal/runtime"
)

// Poller waits for conditions in one deployment.
type Poller struct {
	// Interval is how often a condition is checked.
	Interval time.Duration
	// Tail returns the end of a container's log, for the message of a wait that ran out. It may be nil.
	Tail func(ctx context.Context, name string) string
}

// Check is one look at a condition. done reports it holds, and note says what was seen if it does not,
// for the message of a wait that runs out. An error is a check that could not run at all, such as one in a
// container that is gone, and ends the wait at once because waiting will not mend it.
type Check func() (done bool, note string, err error)

// Wait runs check until it holds, the limit passes or the context ends. name is the node being waited on
// and what describes the wait ("answer", "become ready"), for the messages.
func (p Poller) Wait(ctx context.Context, name string, limit time.Duration, what string, check Check) error {
	deadline := time.NewTimer(limit)
	defer deadline.Stop()
	interval := p.Interval
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var last string
	for {
		done, note, err := check()
		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case err != nil:
			return fmt.Errorf("%s: could not %s: %w", name, what, err)
		case done:
			return nil
		case note != "":
			last = note
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			msg := fmt.Sprintf("%s did not %s within %s. Last seen: %s", name, what, limit, OrNone(last))
			if p.Tail != nil {
				msg += "\n" + p.Tail(ctx, name)
			}
			return fmt.Errorf("%s", msg)
		case <-ticker.C:
		}
	}
}

// TailLog returns the end of a log file in a container for an error message. It uses a fresh short
// deadline, because the context that ran out may be the reason the message is needed.
func TailLog(ctx context.Context, rt runtime.Runtime, containerID, name, path string) string {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	res, err := rt.Exec(ctx, containerID, []string{"tail", "-n", "20", path}, runtime.ExecOptions{})
	if err != nil || strings.TrimSpace(res.Stdout) == "" {
		return "(no log lines could be read from " + name + ")"
	}
	return "Last lines of the log of " + name + ":\n" + res.Stdout
}

// OrNone is "nothing" for an empty string, so a message never ends in a blank.
func OrNone(s string) string {
	if s == "" {
		return "nothing"
	}
	return s
}

// FirstLine is the first line of some output, trimmed.
func FirstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

// ExecProblem describes a command that failed or could not run. It shows only the first line of standard
// error, never the output, which may hold a credential.
func ExecProblem(res runtime.ExecResult, err error) error {
	if err != nil {
		return err
	}
	msg := FirstLine(res.Stderr)
	if msg == "" {
		return fmt.Errorf("exit code %d", res.ExitCode)
	}
	return fmt.Errorf("exit code %d: %s", res.ExitCode, msg)
}

// File is a file to copy into a container, with its content as text.
func File(path string, mode fs.FileMode, content string) runtime.File {
	return runtime.File{Path: path, Mode: mode, Size: int64(len(content)), Content: strings.NewReader(content)}
}
