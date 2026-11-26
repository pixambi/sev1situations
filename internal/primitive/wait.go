package primitive

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// maxSleep is the longest wait.sleep allows. Longer waits belong in wait.until.
const maxSleep = 30 * time.Second

type waitUntil struct{}

func (waitUntil) Spec() Spec {
	return Spec{
		Name: "wait.until",
		Summary: "Runs a check again and again until it passes or the time runs out. " +
			"If it never passes the result is a fail that shows the last result of the check, not an error. " +
			"Whether it is safe to run again depends on the check.",
		Timeout: 60 * time.Second,
		Nested:  []string{"check"},
		Args: []Arg{
			{Name: "check", Type: Map, Required: true, Help: "The check to retry, written as {primitive: name, args: {...}}."},
			{Name: "interval", Type: Duration, Default: "1s", Help: "How long to wait between attempts."},
		},
	}
}

func (waitUntil) Run(ctx context.Context, env Env, a Args) (Result, error) {
	check := a.Map("check")
	name, _ := check["primitive"].(string)
	inner, _ := check["args"].(map[string]any)
	if name == "" {
		return Result{}, errors.New("the check needs a primitive")
	}
	if env.Call == nil {
		return Result{}, errors.New("this run cannot call another primitive")
	}
	interval := a.Duration("interval")
	if interval < 10*time.Millisecond {
		return Result{}, fmt.Errorf("interval %s is too short: use at least 10ms", interval)
	}

	start := time.Now()
	var last string
	for attempt := 1; ; attempt++ {
		res, err := env.Call(ctx, name, inner)
		if ctx.Err() != nil {
			return untilEnded(ctx, name, attempt, time.Since(start), last)
		}
		var badArgs *ArgsError
		if errors.Is(err, ErrUnknown) || errors.As(err, &badArgs) {
			return Result{}, err // a check that is set up wrong can never pass, so do not wait for it
		}
		switch {
		case err != nil:
			// The environment may still be settling, so an error is retried too.
			last = "error: " + err.Error()
		case res.Status == Pass:
			res.Message = fmt.Sprintf("passed on attempt %d after %s: %s", attempt, time.Since(start).Round(time.Millisecond), res.Message)
			return res, nil
		default:
			last = res.Message
		}

		select {
		case <-ctx.Done():
			return untilEnded(ctx, name, attempt, time.Since(start), last)
		case <-time.After(interval):
		}
	}
}

// untilEnded reports how wait.until stopped. Running out of time is a Fail. The
// run ending is neither, so the context's error is returned.
func untilEnded(ctx context.Context, name string, attempts int, took time.Duration, last string) (Result, error) {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return Result{Status: Fail, Message: fmt.Sprintf("%s did not pass in %s after %d attempts. Last result: %s", name, took.Round(time.Millisecond), attempts, last)}, nil
	}
	return Result{}, ctx.Err()
}

type waitSleep struct{}

func (waitSleep) Spec() Spec {
	return Spec{
		Name:    "wait.sleep",
		Summary: "Pauses for a fixed time, at most 30 seconds. Use it only when there is nothing to check, and prefer wait.until. Safe to run again.",
		Timeout: maxSleep + 5*time.Second,
		Args:    []Arg{{Name: "duration", Type: Duration, Required: true, Help: "How long to pause, for example 2s."}},
	}
}

func (waitSleep) Run(ctx context.Context, _ Env, a Args) (Result, error) {
	d := a.Duration("duration")
	if d <= 0 || d > maxSleep {
		return Result{}, fmt.Errorf("duration %s must be between 1ns and %s: use wait.until for longer waits", d, maxSleep)
	}
	select {
	case <-time.After(d):
		return passed("waited %s", d), nil
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}
