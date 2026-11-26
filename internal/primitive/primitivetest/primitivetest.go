// Package primitivetest is the contract every primitive must satisfy (section
// 4.5). Each primitive's own tests call Contract first and then test what it
// does.
package primitivetest

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/primitive"
)

// Options gives the contract what it needs to exercise Run.
type Options struct {
	// ValidArgs, if set, are arguments the primitive accepts. They let the
	// contract run the primitive, to check that it stops when its context ends.
	ValidArgs map[string]any
	// Env is the environment to run it in, for example one using the fake runtime.
	Env primitive.Env
}

// Contract checks a primitive against the rules every primitive follows.
func Contract(t *testing.T, p primitive.Primitive, o Options) {
	t.Helper()
	spec := p.Spec()

	t.Run("the declaration is sound", func(t *testing.T) {
		if err := primitive.NewRegistry().Register(p); err != nil {
			t.Fatalf("Register: %v", err)
		}
	})

	t.Run("Spec returns the same value every time", func(t *testing.T) {
		if !reflect.DeepEqual(p.Spec(), p.Spec()) {
			t.Error("two calls to Spec differ")
		}
	})

	reg := primitive.NewRegistry()
	if err := reg.Register(p); err != nil {
		return // the first subtest already reported it
	}

	t.Run("an unknown argument is rejected", func(t *testing.T) {
		args := map[string]any{"definitelyNotAnArgument": 1}
		if _, err := reg.Prepare(spec.Name, args); err == nil {
			t.Error("a misspelled argument was accepted, so a typo would reach a live run")
		}
		if problems := reg.CheckArgs(spec.Name, args); len(problems) == 0 {
			t.Error("CheckArgs accepted a misspelled argument")
		}
	})

	t.Run("every required argument is enforced", func(t *testing.T) {
		var required []string
		for _, a := range spec.Args {
			if a.Required {
				required = append(required, a.Name)
			}
		}
		_, err := reg.Prepare(spec.Name, map[string]any{})
		if len(required) == 0 {
			if err != nil {
				t.Errorf("Prepare with no arguments = %v, but none are required", err)
			}
			return
		}
		var ae *primitive.ArgsError
		if !errors.As(err, &ae) || len(ae.Problems) < len(required) {
			t.Errorf("Prepare with no arguments = %v, want a problem for each of %v", err, required)
		}
	})

	if o.ValidArgs == nil {
		return
	}

	t.Run("it stops when its context ends", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		done := make(chan struct{})
		go func() {
			defer close(done)
			// Whether it returns an error or a result does not matter, only that it returns.
			_, _ = reg.Run(ctx, spec.Name, o.Env, o.ValidArgs)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("the primitive kept running after its context was cancelled")
		}
	})
}
