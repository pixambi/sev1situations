package primitive_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/primitive/primitivetest"
)

// stub is a primitive whose behaviour a test sets.
type stub struct {
	spec primitive.Spec
	run  func(ctx context.Context, env primitive.Env, args primitive.Args) (primitive.Result, error)
}

func (s stub) Spec() primitive.Spec { return s.spec }
func (s stub) Run(ctx context.Context, env primitive.Env, a primitive.Args) (primitive.Result, error) {
	return s.run(ctx, env, a)
}

func spec(name string, args ...primitive.Arg) primitive.Spec {
	return primitive.Spec{Name: name, Summary: "A test primitive.", Args: args, Timeout: 2 * time.Second}
}

func ok(context.Context, primitive.Env, primitive.Args) (primitive.Result, error) {
	return primitive.Result{Status: primitive.Pass, Message: "fine"}, nil
}

var (
	argNode   = primitive.Arg{Name: "node", Type: primitive.String, Required: true, Help: "The node."}
	argVoters = primitive.Arg{Name: "voters", Type: primitive.Int, Default: 1, Help: "Voter count."}
)

func register(t *testing.T, ps ...primitive.Primitive) *primitive.Registry {
	t.Helper()
	r := primitive.NewRegistry()
	for _, p := range ps {
		if err := r.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func TestRegisterRejectsUnsoundDeclarations(t *testing.T) {
	tests := []struct {
		name string
		spec primitive.Spec
		want string
	}{
		{"name without a dot", spec("exec"), "dotted"},
		{"upper-case name", spec("Container.exec"), "dotted"},
		{"empty summary", primitive.Spec{Name: "a.b", Timeout: time.Second}, "summary"},
		{"no timeout", primitive.Spec{Name: "a.b", Summary: "x"}, "blocks forever"},
		{"huge timeout", primitive.Spec{Name: "a.b", Summary: "x", Timeout: 24 * time.Hour}, "blocks forever"},
		{"duplicate argument", spec("a.b", argNode, argNode), "twice"},
		{"reserved argument name", spec("a.b", primitive.Arg{Name: "timeout", Type: primitive.Int, Help: "x"}), "reserved"},
		{"argument without help", spec("a.b", primitive.Arg{Name: "x", Type: primitive.String}), "help"},
		{"required with a default", spec("a.b", primitive.Arg{Name: "x", Type: primitive.String, Required: true, Default: "d", Help: "x"}), "cannot have a default"},
		{"default of the wrong type", spec("a.b", primitive.Arg{Name: "x", Type: primitive.Int, Default: "nope", Help: "x"}), "default"},
		{"unknown argument type", spec("a.b", primitive.Arg{Name: "x", Type: "colour", Help: "x"}), "unknown type"},
		{"argument name not camel case", spec("a.b", primitive.Arg{Name: "Bad_Name", Type: primitive.String, Help: "x"}), "camel case"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := primitive.NewRegistry().Register(stub{spec: tt.spec, run: ok})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Register = %v, want an error containing %q", err, tt.want)
			}
		})
	}

	r := register(t, stub{spec: spec("a.b"), run: ok})
	if err := r.Register(stub{spec: spec("a.b"), run: ok}); err == nil {
		t.Error("registering a name twice succeeded")
	}
}

func TestHasAndNames(t *testing.T) {
	r := register(t, stub{spec: spec("vault.status"), run: ok}, stub{spec: spec("container.exec"), run: ok})
	if !r.Has("vault.status") || r.Has("vault.nope") {
		t.Error("Has gave the wrong answer")
	}
	if got := strings.Join(r.Names(), ","); got != "container.exec,vault.status" {
		t.Errorf("Names = %s, want them sorted", got)
	}
}

func TestCheckArgsAtLoadTime(t *testing.T) {
	r := register(t, stub{spec: spec("vault.raft-peers", argNode, argVoters), run: ok})

	tests := []struct {
		name string
		args map[string]any
		want string // "" means no problems
	}{
		{"valid", map[string]any{"node": "node-1", "voters": 2}, ""},
		{"default left out", map[string]any{"node": "node-1"}, ""},
		{"a typo in an argument name", map[string]any{"node": "node-1", "voterz": 2}, `unknown argument "voterz"`},
		{"a required argument missing", map[string]any{"voters": 2}, `missing required argument "node"`},
		{"wrong type", map[string]any{"node": "node-1", "voters": "two"}, `argument "voters"`},
		{"a template is accepted for any type", map[string]any{"node": "{{ .Nodes.node_1.Name }}", "voters": "{{ .Params.voters }}"}, ""},
		{"timeout in range", map[string]any{"node": "n", "timeout": "10s"}, ""},
		{"timeout not a duration", map[string]any{"node": "n", "timeout": "soon"}, `argument "timeout"`},
		{"timeout too long", map[string]any{"node": "n", "timeout": "5h"}, "must be between"},
		{"no arguments at all", nil, `missing required argument "node"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problems := r.CheckArgs("vault.raft-peers", tt.args)
			got := strings.Join(problems, "; ")
			if tt.want == "" && got != "" || !strings.Contains(got, tt.want) {
				t.Errorf("CheckArgs = %q, want %q", got, tt.want)
			}
		})
	}
	if problems := r.CheckArgs("does.not-exist", nil); problems != nil {
		t.Errorf("CheckArgs for an unknown primitive = %v, want nothing (Has reports it)", problems)
	}
}

func TestPrepareTypesAndFillsDefaults(t *testing.T) {
	all := primitive.Spec{Name: "all.types", Summary: "x", Timeout: time.Second, Args: []primitive.Arg{
		{Name: "text", Type: primitive.String, Default: "d", Help: "x"},
		{Name: "count", Type: primitive.Int, Default: 7, Help: "x"},
		{Name: "flag", Type: primitive.Bool, Default: false, Help: "x"},
		{Name: "wait", Type: primitive.Duration, Default: "5s", Help: "x"},
		{Name: "items", Type: primitive.StringList, Help: "x"},
		{Name: "headers", Type: primitive.Map, Help: "x"},
	}}
	r := register(t, stub{spec: all, run: ok})

	// Rendered templates arrive as text and become typed values.
	args, err := r.Prepare("all.types", map[string]any{
		"count": "42", "flag": "true", "wait": "2m",
		"items": []any{"a", "b"}, "headers": map[string]any{"X": "y"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if args.String("text") != "d" || args.Int("count") != 42 || !args.Bool("flag") ||
		args.Duration("wait") != 2*time.Minute || len(args.List("items")) != 2 || args.Map("headers")["X"] != "y" {
		t.Errorf("args = %+v", args)
	}
	if args.Duration("timeout") != time.Second {
		t.Errorf("timeout = %s, want the primitive's default", args.Duration("timeout"))
	}

	if _, err := r.Prepare("all.types", map[string]any{"count": "{{ .Params.x }}"}); err == nil {
		t.Error("an unresolved template was accepted at run time")
	}
	if _, err := r.Prepare("all.types", map[string]any{"items": []any{"a", 5}}); err == nil {
		t.Error("a list with a number in it was accepted as a list of text")
	}
	var ae *primitive.ArgsError
	_, err = r.Prepare("all.types", map[string]any{"nope": 1})
	if !errors.As(err, &ae) || ae.Primitive != "all.types" {
		t.Errorf("Prepare = %v, want an *ArgsError naming the primitive", err)
	}
	if _, err := r.Prepare("missing.one", nil); !errors.Is(err, primitive.ErrUnknown) {
		t.Errorf("Prepare of an unknown primitive = %v, want ErrUnknown", err)
	}
}

func TestPassFailAndErrorLookDifferent(t *testing.T) {
	r := register(t,
		stub{spec: spec("t.pass"), run: ok},
		stub{spec: spec("t.fail"), run: func(context.Context, primitive.Env, primitive.Args) (primitive.Result, error) {
			return primitive.Result{Status: primitive.Fail, Message: "still broken", Evidence: "no leader"}, nil
		}},
		stub{spec: spec("t.error"), run: func(context.Context, primitive.Env, primitive.Args) (primitive.Result, error) {
			return primitive.Result{}, errors.New("container is gone")
		}},
	)
	ctx := context.Background()

	if res, err := r.Run(ctx, "t.pass", primitive.Env{}, nil); err != nil || res.Status != primitive.Pass {
		t.Errorf("pass = %+v, %v", res, err)
	}
	res, err := r.Run(ctx, "t.fail", primitive.Env{}, nil)
	if err != nil || res.Status != primitive.Fail || res.Message != "still broken" || res.Evidence != "no leader" {
		t.Errorf("fail = %+v, %v; a failed condition is a result, not an error", res, err)
	}
	var pe *primitive.Error
	if _, err := r.Run(ctx, "t.error", primitive.Env{}, nil); !errors.As(err, &pe) || pe.Primitive != "t.error" {
		t.Errorf("error = %v, want a *primitive.Error", err)
	}
}

func TestRunRejectsBadArgumentsBeforeRunning(t *testing.T) {
	ran := false
	r := register(t, stub{spec: spec("t.args", argNode), run: func(context.Context, primitive.Env, primitive.Args) (primitive.Result, error) {
		ran = true
		return primitive.Result{Status: primitive.Pass}, nil
	}})
	var ae *primitive.ArgsError
	if _, err := r.Run(context.Background(), "t.args", primitive.Env{}, map[string]any{}); !errors.As(err, &ae) {
		t.Fatalf("Run = %v, want an *ArgsError", err)
	}
	if ran {
		t.Error("the primitive ran with invalid arguments")
	}
}

func TestRunStopsAtTheTimeout(t *testing.T) {
	block := func(ctx context.Context, _ primitive.Env, _ primitive.Args) (primitive.Result, error) {
		<-ctx.Done()
		return primitive.Result{}, ctx.Err()
	}
	r := register(t, stub{spec: spec("t.block"), run: block})

	start := time.Now()
	_, err := r.Run(context.Background(), "t.block", primitive.Env{}, map[string]any{"timeout": "50ms"})
	var te *primitive.TimeoutError
	if !errors.As(err, &te) || te.After != 50*time.Millisecond {
		t.Fatalf("Run = %v, want a *TimeoutError for 50ms", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Error("it blocked far past its limit")
	}
}

func TestRunStopsWhenTheRunEnds(t *testing.T) {
	block := func(ctx context.Context, _ primitive.Env, _ primitive.Args) (primitive.Result, error) {
		<-ctx.Done()
		return primitive.Result{}, ctx.Err()
	}
	r := register(t, stub{spec: spec("t.block"), run: block})

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	_, err := r.Run(ctx, "t.block", primitive.Env{}, nil)
	var te *primitive.TimeoutError
	if !errors.Is(err, context.Canceled) || errors.As(err, &te) {
		t.Errorf("Run = %v, want context.Canceled and not a timeout: ending the scenario is not the primitive's failure", err)
	}
}

func TestRunRecoversFromAPanic(t *testing.T) {
	r := register(t, stub{spec: spec("t.panic"), run: func(context.Context, primitive.Env, primitive.Args) (primitive.Result, error) {
		panic("boom")
	}})
	_, err := r.Run(context.Background(), "t.panic", primitive.Env{}, nil)
	if err == nil || !strings.Contains(err.Error(), "panicked: boom") {
		t.Errorf("Run = %v, want the panic reported as an error and not a crash", err)
	}
}

func TestRunRejectsAnInvalidStatus(t *testing.T) {
	r := register(t, stub{spec: spec("t.odd"), run: func(context.Context, primitive.Env, primitive.Args) (primitive.Result, error) {
		return primitive.Result{Status: "maybe"}, nil
	}})
	if _, err := r.Run(context.Background(), "t.odd", primitive.Env{}, nil); err == nil {
		t.Error("a primitive that returned neither pass nor fail was accepted")
	}
}

func TestRunRedactsCredentials(t *testing.T) {
	const token = "hvs.CAESIJexampleRootToken"
	leak := func(context.Context, primitive.Env, primitive.Args) (primitive.Result, error) {
		return primitive.Result{
			Status:   primitive.Fail,
			Message:  "login with " + token + " failed",
			Evidence: "VAULT_TOKEN=" + token + "\nstatus: sealed",
			Outputs:  map[string]string{"token": token},
		}, nil
	}
	fail := func(context.Context, primitive.Env, primitive.Args) (primitive.Result, error) {
		return primitive.Result{}, errors.New("request with " + token + " refused")
	}
	r := register(t, stub{spec: spec("t.leak"), run: leak}, stub{spec: spec("t.leakerr"), run: fail})
	env := primitive.Env{Redactor: primitive.NewRedactor(token)}

	res, err := r.Run(context.Background(), "t.leak", env, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Message, token) || strings.Contains(res.Evidence, token) {
		t.Errorf("a credential reached the result: %+v (R-QUAL-3)", res)
	}
	if !strings.Contains(res.Evidence, "status: sealed") || !strings.Contains(res.Message, primitive.Redacted) {
		t.Errorf("redaction removed too much or marked nothing: %+v", res)
	}
	if res.Outputs["token"] != token {
		t.Error("an output was redacted, but later steps need its real value")
	}

	_, err = r.Run(context.Background(), "t.leakerr", env, nil)
	if err == nil || strings.Contains(err.Error(), token) {
		t.Errorf("error text = %v, want the credential removed", err)
	}
}

func TestRedactor(t *testing.T) {
	r := primitive.NewRedactor("abcd", "abcdefgh", "", "xy", "abcd")
	tests := []struct{ in, want string }{
		{"abcdefgh and abcd", "[redacted] and [redacted]"}, // the longer secret goes first
		{"xy is too short to hide", "xy is too short to hide"},
		{"nothing here", "nothing here"},
	}
	for _, tt := range tests {
		if got := r.Redact(tt.in); got != tt.want {
			t.Errorf("Redact(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	var none *primitive.Redactor
	if got := none.Redact("abc"); got != "abc" {
		t.Errorf("a nil Redactor changed the text: %q", got)
	}
}

// TestTheContractHelperAcceptsAGoodPrimitive proves the shared contract passes
// for a well-behaved primitive, so primitives can rely on it.
func TestTheContractHelperAcceptsAGoodPrimitive(t *testing.T) {
	good := stub{spec: spec("t.good", argNode), run: func(ctx context.Context, _ primitive.Env, _ primitive.Args) (primitive.Result, error) {
		select {
		case <-ctx.Done():
			return primitive.Result{}, ctx.Err()
		case <-time.After(time.Minute):
			return primitive.Result{Status: primitive.Pass}, nil
		}
	}}
	primitivetest.Contract(t, good, primitivetest.Options{ValidArgs: map[string]any{"node": "node-1"}})
}
