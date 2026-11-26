package primitive

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// A name is dotted and lower-case: an owner, then the verb, for example
// container.exec or vault.raft-peers.
var (
	namePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9-]*)+$`)
	argPattern  = regexp.MustCompile(`^[a-z][A-Za-z0-9]*$`)
)

// ErrUnknown means no primitive has the given name.
var ErrUnknown = errors.New("unknown primitive")

// ArgsError means a primitive's arguments are wrong.
type ArgsError struct {
	// Primitive is the primitive's name.
	Primitive string
	// Problems say what is wrong, one per line.
	Problems []string
}

func (e *ArgsError) Error() string {
	return fmt.Sprintf("%s: %s", e.Primitive, strings.Join(e.Problems, "; "))
}

// TimeoutError means a primitive ran past its time limit. It is an error, not a
// Fail: the condition was never decided.
type TimeoutError struct {
	// Primitive is the primitive's name.
	Primitive string
	// After is the limit that was reached.
	After time.Duration
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("%s did not finish within %s", e.Primitive, e.After)
}

// Error means a primitive could not run, for example the container is gone. It
// is shown differently from a Fail. The text has known credentials removed.
type Error struct {
	// Primitive is the primitive's name.
	Primitive string
	msg       string
	err       error
}

func (e *Error) Error() string { return e.Primitive + ": " + e.msg }

// Unwrap returns the underlying error for errors.Is and errors.As. Display
// Error(), never the unwrapped value, because only Error() is redacted.
func (e *Error) Unwrap() error { return e.err }

// Registry holds the primitives. Register everything at start-up, before
// scenarios are loaded.
type Registry struct {
	mu    sync.RWMutex
	prims map[string]Primitive
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{prims: map[string]Primitive{}}
}

// Register adds a primitive after checking that its declaration is sound: a
// well-formed name, a summary, a positive timeout and well-formed arguments.
func (r *Registry) Register(p Primitive) error {
	spec := p.Spec()
	if err := checkSpec(spec); err != nil {
		return fmt.Errorf("register primitive %q: %w", spec.Name, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.prims[spec.Name]; ok {
		return fmt.Errorf("register primitive %q: already registered", spec.Name)
	}
	r.prims[spec.Name] = p
	return nil
}

func checkSpec(s Spec) error {
	if !namePattern.MatchString(s.Name) {
		return errors.New("the name must be dotted and lower-case, for example container.exec")
	}
	if strings.TrimSpace(s.Summary) == "" {
		return errors.New("a summary is required: every primitive is documented (R-SCN-5)")
	}
	if s.Timeout <= 0 || s.Timeout > maxTimeout {
		return fmt.Errorf("the default timeout %s must be between 1ns and %s: no primitive blocks forever", s.Timeout, maxTimeout)
	}

	seen := map[string]bool{TimeoutArg: true}
	for _, a := range s.Args {
		switch {
		case !argPattern.MatchString(a.Name):
			return fmt.Errorf("argument %q must be lower camel case", a.Name)
		case seen[a.Name]:
			return fmt.Errorf("argument %q is declared twice (or is the reserved %q)", a.Name, TimeoutArg)
		case a.Help == "":
			return fmt.Errorf("argument %q needs help text", a.Name)
		case a.Required && a.Default != nil:
			return fmt.Errorf("argument %q is required, so it cannot have a default", a.Name)
		}
		seen[a.Name] = true
		if a.Default != nil {
			if _, err := convert(a, a.Default, false); err != nil {
				return fmt.Errorf("the default of argument %q: %w", a.Name, err)
			}
		}
		if _, err := convert(a, zeroValue(a.Type), false); err != nil {
			return fmt.Errorf("argument %q has an unknown type %q", a.Name, a.Type)
		}
	}
	return nil
}

// zeroValue returns a value of the declared type, to prove the type is known.
func zeroValue(t ArgType) any {
	switch t {
	case String:
		return ""
	case Int:
		return 0
	case Bool:
		return false
	case Duration:
		return time.Second
	case StringList:
		return []string{}
	case Map:
		return map[string]any{}
	}
	return nil
}

// Has reports whether a primitive is registered. It lets the scenario validator
// check names without importing this package.
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.prims[name]
	return ok
}

// Get returns a registered primitive. The contract tests use it to run the same
// checks over every primitive.
func (r *Registry) Get(name string) (Primitive, bool) { return r.get(name) }

// Names returns every registered name in order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.prims))
	for n := range r.prims {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func (r *Registry) get(name string) (Primitive, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.prims[name]
	return p, ok
}

// CheckArgs validates a scenario's arguments for a primitive when the scenario
// is loaded, so a typo fails validation and not a live run. A text value that
// contains a template is accepted for any argument, because it is resolved
// later. It returns one message per problem, and none for an unknown primitive,
// which Has reports.
func (r *Registry) CheckArgs(name string, args map[string]any) []string {
	p, ok := r.get(name)
	if !ok {
		return nil
	}
	spec := p.Spec()
	_, problems := spec.check(args, true)
	return append(problems, r.checkNested(spec, args)...)
}

// checkNested checks the calls held in a primitive's Nested arguments.
func (r *Registry) checkNested(spec Spec, args map[string]any) []string {
	var problems []string
	for _, arg := range spec.Nested {
		call, ok := args[arg].(map[string]any)
		if !ok {
			continue
		}
		name, _ := call["primitive"].(string)
		switch {
		case name == "":
			problems = append(problems, fmt.Sprintf("argument %q: the nested call needs a primitive", arg))
		case strings.Contains(name, "{{"):
		case !r.Has(name):
			problems = append(problems, fmt.Sprintf("argument %q: %q is not a known primitive", arg, name))
		default:
			inner, _ := call["args"].(map[string]any)
			for _, msg := range r.CheckArgs(name, inner) {
				problems = append(problems, fmt.Sprintf("argument %q, %s: %s", arg, name, msg))
			}
		}
	}
	return problems
}

// Prepare checks arguments that are fully resolved, fills in defaults and
// converts them to their declared types. A rendered template such as "3" becomes
// the number 3 here.
func (r *Registry) Prepare(name string, raw map[string]any) (Args, error) {
	p, ok := r.get(name)
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnknown, name)
	}
	args, problems := p.Spec().check(raw, false)
	if len(problems) > 0 {
		return nil, &ArgsError{Primitive: name, Problems: problems}
	}
	return args, nil
}

// Run prepares the arguments and runs the primitive under its time limit and
// the given context, so ending the scenario cancels it. A panic in a primitive
// becomes an error. The result's message and evidence, and any error text, have
// the run's known credentials removed.
//
// A Result with Status Fail and a nil error means it ran and the condition was
// not met. A non-nil error means it could not run.
func (r *Registry) Run(ctx context.Context, name string, env Env, raw map[string]any) (res Result, err error) {
	p, ok := r.get(name)
	if !ok {
		return Result{}, fmt.Errorf("%w %q", ErrUnknown, name)
	}
	args, problems := p.Spec().check(raw, false)
	if len(problems) > 0 {
		return Result{}, &ArgsError{Primitive: name, Problems: problems}
	}

	env.Call = func(c context.Context, n string, a map[string]any) (Result, error) {
		return r.Run(c, n, env, a)
	}

	limit := args.Duration(TimeoutArg)
	runCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()

	defer func() {
		if rec := recover(); rec != nil {
			res, err = Result{}, r.wrap(env, name, fmt.Errorf("panicked: %v", rec))
		}
	}()

	res, err = p.Run(runCtx, env, args)
	if err != nil {
		switch {
		case ctx.Err() != nil:
			return Result{}, ctx.Err() // the run ended, which is not the primitive's fault
		case errors.Is(runCtx.Err(), context.DeadlineExceeded):
			return Result{}, &TimeoutError{Primitive: name, After: limit}
		}
		return Result{}, r.wrap(env, name, err)
	}
	if res.Status != Pass && res.Status != Fail {
		return Result{}, r.wrap(env, name, fmt.Errorf("returned the invalid status %q", res.Status))
	}

	res.Message = env.Redactor.Redact(res.Message)
	res.Evidence = env.Redactor.Redact(res.Evidence)
	return res, nil
}

func (r *Registry) wrap(env Env, name string, err error) error {
	return &Error{Primitive: name, msg: env.Redactor.Redact(err.Error()), err: err}
}
