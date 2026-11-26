package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/runtime"
	"github.com/pixambi/sev1situations/internal/runtime/fake"
	"github.com/pixambi/sev1situations/internal/scenario"
)

// waitFor polls a condition. It exists for the few tests where one goroutine
// must reach a known point before another acts, and it fails the test if the
// point is never reached.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestStartDeploysAndActivatesTheFirstStep(t *testing.T) {
	r := newRig(t, testScenario)
	r.start()

	snap := r.e.Snapshot()
	if snap.Status != StatusActive || snap.ActiveStep != 0 || snap.RunID != "run1" || snap.Instructor != "Ada" || snap.Trainee != "Grace" {
		t.Fatalf("snapshot = %+v", snap)
	}
	if snap.Params["version"] != "1.21.0" || snap.Params["nodes"] != 2 {
		t.Errorf("params = %v, want the chosen version and the default node count", snap.Params)
	}
	if run := r.dep.runs[0]; run != (runtime.Run{ID: "run1", Scenario: "test-scenario"}) {
		t.Errorf("deployed as %+v, want the run carrying the scenario ID", run)
	}
	if r.containers() != 2 {
		t.Errorf("containers = %d, want one per node", r.containers())
	}

	want := map[string]StepState{"break": StepActive, "fix": StepLocked, "break-again": StepLocked, "fix-again": StepLocked}
	for _, s := range snap.Steps {
		if s.State != want[s.ID] {
			t.Errorf("step %s is %s, want %s", s.ID, s.State, want[s.ID])
		}
	}
	if b := snap.Steps[0]; !b.CanApply || b.CanVerify || b.CanContinue || b.CanRollBack {
		t.Errorf("first step buttons = %+v, want only Apply", b)
	}

	got := r.kinds()
	for i, k := range []EventKind{EventRunStarted, EventDeployProgress, EventDeployProgress, EventDeployComplete, EventStepActive} {
		if i >= len(got) || got[i] != k {
			t.Fatalf("events = %v, want them to start with run-started, two progress lines, deploy-complete, step-active", got)
		}
	}
	for i, ev := range snap.Events {
		if ev.Seq != i+1 {
			t.Errorf("event %d has Seq %d, want them numbered from 1", i, ev.Seq)
		}
	}
}

func TestStartChecksItsInput(t *testing.T) {
	r := newRig(t, testScenario)
	good := map[string]any{"version": "1.21.0"}
	tests := []struct {
		name string
		in   StartInput
		want string
	}{
		{"no instructor", StartInput{Trainee: "G", Params: good}, "names are required"},
		{"no trainee", StartInput{Instructor: "A", Params: good}, "names are required"},
		{"blank names", StartInput{Instructor: "  ", Trainee: " ", Params: good}, "names are required"},
		{"an undeclared parameter", StartInput{Instructor: "A", Trainee: "G", Params: map[string]any{"version": "1", "colour": "red"}}, "unknown parameter(s): colour"},
		{"no version", StartInput{Instructor: "A", Trainee: "G"}, `parameter "version"`},
		{"a node count out of range", StartInput{Instructor: "A", Trainee: "G", Params: map[string]any{"version": "1", "nodes": 9}}, "outside 1 to 5"},
		{"a node count that is not a number", StartInput{Instructor: "A", Trainee: "G", Params: map[string]any{"version": "1", "nodes": "many"}}, "not a whole number"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := r.e.Start(ctx, r.sc, tt.in)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Start = %v, want an error containing %q", err, tt.want)
			}
			if r.e.Snapshot().Status != StatusIdle || r.dep.count() != 0 {
				t.Error("a rejected Start left a run behind or deployed something")
			}
		})
	}
}

func TestResolveParams(t *testing.T) {
	sc := loadScenario(t, testScenario)
	tests := []struct {
		name  string
		given map[string]any
		want  map[string]any
		err   string
	}{
		{"defaults fill the gaps", map[string]any{"version": "1.2.3"}, map[string]any{"version": "1.2.3", "nodes": 2}, ""},
		{"a string number is converted", map[string]any{"version": "1", "nodes": "4"}, map[string]any{"version": "1", "nodes": 4}, ""},
		{"a whole float is converted", map[string]any{"version": "1", "nodes": 3.0}, map[string]any{"version": "1", "nodes": 3}, ""},
		{"an empty value takes the default", map[string]any{"version": "1", "nodes": ""}, map[string]any{"version": "1", "nodes": 2}, ""},
		{"a fractional number is refused", map[string]any{"version": "1", "nodes": 2.5}, nil, "not a whole number"},
		{"below the minimum", map[string]any{"version": "1", "nodes": 0}, nil, "outside 1 to 5"},
		{"a version with spaces is trimmed", map[string]any{"version": " 1.0 "}, map[string]any{"version": "1.0", "nodes": 2}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveParams(sc, tt.given)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Errorf("error = %v, want %q", err, tt.err)
				}
				return
			}
			if err != nil || fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("ResolveParams = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

func TestOnlyOneRunAtATime(t *testing.T) {
	r := newRig(t, testScenario)
	r.start()
	err := r.e.Start(ctx, r.sc, StartInput{Instructor: "A", Trainee: "G", Params: map[string]any{"version": "1"}})
	if !errors.Is(err, ErrBusy) {
		t.Errorf("a second Start = %v, want ErrBusy (R-SCN-8)", err)
	}
	if r.dep.count() != 1 {
		t.Error("the second Start deployed something")
	}
}

func TestDeployFailureCanBeRetried(t *testing.T) {
	r := newRig(t, testScenario)
	r.dep.fail = []error{errors.New("image pull failed")}

	err := r.e.Start(ctx, r.sc, StartInput{Instructor: "A", Trainee: "G", Params: map[string]any{"version": "1.21.0"}})
	if err == nil || !strings.Contains(err.Error(), "image pull failed") {
		t.Fatalf("Start = %v, want the deploy error", err)
	}
	snap := r.e.Snapshot()
	if snap.Status != StatusDeployFailed || !strings.Contains(snap.DeployError, "image pull failed") {
		t.Fatalf("snapshot = %+v, want a failed deployment that says why", snap)
	}
	if r.containers() != 2 {
		t.Errorf("containers = %d, want the failed deployment left in place so its logs can be read", r.containers())
	}
	if err := r.e.Apply(ctx); !errors.Is(err, ErrWrongState) {
		t.Errorf("Apply on a failed deployment = %v, want ErrWrongState", err)
	}

	r.ok(r.e.Retry(ctx))
	snap = r.e.Snapshot()
	if snap.Status != StatusActive || snap.DeployError != "" || snap.ActiveStep != 0 {
		t.Errorf("after Retry the snapshot is %+v", snap)
	}
	if r.containers() != 2 || r.dep.count() != 2 {
		t.Errorf("containers=%d deploys=%d, want the old environment replaced, not added to", r.containers(), r.dep.count())
	}
	if !contains(r.kinds(), EventRetry) || !contains(r.kinds(), EventDeployFailed) {
		t.Errorf("events = %v, want the failure and the retry recorded", r.kinds())
	}
	if err := r.e.Retry(ctx); !errors.Is(err, ErrWrongState) {
		t.Errorf("Retry on a healthy run = %v, want ErrWrongState", err)
	}
}

func TestRetryThatFailsAgainStaysFailed(t *testing.T) {
	r := newRig(t, testScenario)
	r.dep.fail = []error{errors.New("first"), errors.New("second")}
	_ = r.e.Start(ctx, r.sc, StartInput{Instructor: "A", Trainee: "G", Params: map[string]any{"version": "1"}})

	if err := r.e.Retry(ctx); err == nil || !strings.Contains(err.Error(), "second") {
		t.Fatalf("Retry = %v, want the second error", err)
	}
	if s := r.e.Snapshot(); s.Status != StatusDeployFailed || !strings.Contains(s.DeployError, "second") {
		t.Errorf("snapshot = %+v", s)
	}
}

func contains(kinds []EventKind, k EventKind) bool {
	for _, x := range kinds {
		if x == k {
			return true
		}
	}
	return false
}

func TestTheStepsRunInOrderAndCannotBeSkipped(t *testing.T) {
	r := newRig(t, testScenario)
	r.start()

	// Nothing can be skipped: Continue and Verify need the right state first.
	if err := r.e.Continue(ctx); !errors.Is(err, ErrWrongState) {
		t.Errorf("Continue before anything = %v, want ErrWrongState", err)
	}
	if err := r.e.Verify(ctx); !errors.Is(err, ErrWrongState) {
		t.Errorf("Verify on a breakage that is not applied = %v, want ErrWrongState", err)
	}

	r.ok(r.e.Apply(ctx))
	b := r.step("break")
	if b.State != StepApplied || !b.Verified || b.Last == nil || b.Last.Status != "pass" || !b.CanContinue || b.CanApply || !b.CanVerify {
		t.Fatalf("after Apply, break = %+v", b)
	}
	if err := r.e.Apply(ctx); !errors.Is(err, ErrWrongState) {
		t.Errorf("a second Apply = %v, want ErrWrongState", err)
	}

	r.ok(r.e.Continue(ctx))
	if r.step("break").State != StepComplete || r.step("fix").State != StepActive || r.step("break-again").State != StepLocked {
		t.Fatal("Continue did not complete one step and activate exactly the next")
	}
	if err := r.e.Apply(ctx); !errors.Is(err, ErrWrongState) {
		t.Errorf("Apply on a verification step = %v, want ErrWrongState", err)
	}

	// Red: the check fails, so Continue stays closed.
	r.ok(r.e.Verify(ctx))
	f := r.step("fix")
	if f.Verified || f.Last.Status != "fail" || f.CanContinue || !f.CanVerify {
		t.Errorf("a failing verification left fix as %+v", f)
	}
	if err := r.e.Continue(ctx); !errors.Is(err, ErrWrongState) {
		t.Errorf("Continue after a failed verification = %v, want ErrWrongState (R-SCN-9)", err)
	}

	// Green: fix it, check again as often as needed, then continue.
	r.world.set("fixed", true)
	r.ok(r.e.Verify(ctx))
	r.ok(r.e.Verify(ctx))
	if f := r.step("fix"); !f.Verified || !f.CanContinue {
		t.Fatalf("after the fix, fix = %+v", f)
	}
	r.ok(r.e.Continue(ctx))

	r.ok(r.e.Apply(ctx))
	r.ok(r.e.Continue(ctx))
	r.world.set("fixed2", true)
	r.ok(r.e.Verify(ctx))
	r.ok(r.e.Continue(ctx))

	snap := r.e.Snapshot()
	if snap.Status != StatusFinished || snap.Result != "completed" || snap.ActiveStep != -1 || snap.EndedAt.IsZero() {
		t.Errorf("after the last step, snapshot = %+v", snap)
	}
	for _, s := range snap.Steps {
		if s.State != StepComplete {
			t.Errorf("step %s is %s", s.ID, s.State)
		}
	}
	if err := r.e.Continue(ctx); !errors.Is(err, ErrWrongState) {
		t.Errorf("Continue after the end = %v, want ErrWrongState", err)
	}
	if !contains(r.kinds(), EventRunFinished) {
		t.Error("run-finished was not recorded")
	}
}

func TestAnActionThatFailsOrErrorsCanBeRetried(t *testing.T) {
	for _, tt := range []struct {
		name, prim, status, message string
	}{
		{"fails", "t.fail", "fail", "it did not work"},
		{"errors", "t.error", "error", "the container is gone"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			yaml := strings.Replace(testScenario, "primitive: t.set\n        args: { key: broken }", "primitive: "+tt.prim, 1)
			r := newRig(t, yaml)
			r.start()
			r.ok(r.e.Apply(ctx))

			b := r.step("break")
			if b.State != StepActive || b.Verified || b.Last == nil || b.Last.Status != tt.status || !strings.Contains(b.Last.Message, tt.message) {
				t.Fatalf("break = %+v, want it still active with the %s recorded", b, tt.status)
			}
			if !b.CanApply || b.CanContinue {
				t.Errorf("buttons = %+v, want Retry (Apply) offered and Continue closed", b)
			}
			if !contains(r.kinds(), EventActionFailed) || contains(r.kinds(), EventRetry) {
				t.Errorf("events = %v", r.kinds())
			}

			r.ok(r.e.Apply(ctx)) // the instructor presses it again
			if !contains(r.kinds(), EventRetry) {
				t.Errorf("events = %v, want the retry recorded (R-REPORT-5)", r.kinds())
			}
		})
	}
}

func TestEvidenceNeverHoldsACredential(t *testing.T) {
	yaml := strings.Replace(testScenario, "primitive: t.set\n        args: { key: broken }", "primitive: t.fail", 1)
	r := newRig(t, yaml)
	r.start()
	r.ok(r.e.Apply(ctx))

	snap := r.e.Snapshot()
	dump := fmt.Sprintf("%+v", snap)
	if strings.Contains(dump, rootToken) {
		t.Errorf("the snapshot holds the root token (R-REPORT-5): %s", dump)
	}
	if ev := snap.Steps[0].Last.Evidence; !strings.Contains(ev, "[redacted]") {
		t.Errorf("evidence = %q, want the credential replaced", ev)
	}
}

func TestVerificationAfterApplyChecksTheBrokenState(t *testing.T) {
	yaml := strings.Replace(testScenario, "verify:\n      - primitive: t.expect\n        args: { key: broken }", "verify:\n      - primitive: t.expect\n        args: { key: confirmed }", 1)
	r := newRig(t, yaml)
	r.start()
	r.ok(r.e.Apply(ctx))

	b := r.step("break")
	if b.State != StepApplied || b.Verified || b.CanContinue || !b.CanVerify || b.Last.Status != "fail" {
		t.Fatalf("break = %+v, want applied but not confirmed broken", b)
	}
	r.world.set("confirmed", true)
	r.ok(r.e.Verify(ctx))
	if b := r.step("break"); !b.Verified || !b.CanContinue {
		t.Errorf("break = %+v, want it confirmed after the check passes", b)
	}
}

func TestNotesAndRatings(t *testing.T) {
	r := newRig(t, testScenario)
	r.start()

	r.ok(r.e.SetNotes("break", "Checked the logs first."))
	r.ok(r.e.SetRating("break", 4))
	if b := r.step("break"); b.Notes != "Checked the logs first." || b.Rating != 4 {
		t.Errorf("break = %+v", b)
	}
	r.ok(r.e.SetRating("break", 0)) // clears it
	if r.step("break").Rating != 0 {
		t.Error("a rating of 0 did not clear it")
	}

	for _, bad := range []int{-1, 6} {
		if err := r.e.SetRating("break", bad); err == nil {
			t.Errorf("rating %d was accepted", bad)
		}
	}
	if err := r.e.SetNotes("fix", "too early"); !errors.Is(err, ErrWrongState) {
		t.Errorf("notes on a step not reached = %v, want ErrWrongState", err)
	}
	if err := r.e.SetNotes("nope", "x"); err == nil {
		t.Error("notes on an unknown step were accepted")
	}

	// Notes stay after the step is complete.
	r.ok(r.e.Apply(ctx))
	r.ok(r.e.Continue(ctx))
	r.ok(r.e.SetNotes("break", "Updated afterwards."))
	if r.step("break").Notes != "Updated afterwards." {
		t.Error("notes on a completed step were not saved")
	}
}

func TestStepTimes(t *testing.T) {
	r := newRig(t, testScenario)
	t0 := r.clock.Now()
	r.start()

	r.clock.Advance(5 * time.Minute)
	r.ok(r.e.Apply(ctx))
	r.ok(r.e.Continue(ctx))
	b := r.step("break")
	if !b.Started.Equal(t0) || !b.Ended.Equal(t0.Add(5*time.Minute)) || b.Elapsed != 5*time.Minute {
		t.Errorf("break times = %v to %v (%v), want 5 minutes from the start", b.Started, b.Ended, b.Elapsed)
	}

	r.clock.Advance(2 * time.Minute)
	f := r.step("fix")
	if !f.Started.Equal(t0.Add(5*time.Minute)) || !f.Ended.IsZero() || f.Elapsed != 2*time.Minute || f.Attempts != 1 {
		t.Errorf("fix = %+v, want a running step with 2 minutes so far", f)
	}
}

func TestAnOperationBlocksOthersAndEndStopsIt(t *testing.T) {
	yaml := strings.Replace(testScenario, "primitive: t.set\n        args: { key: broken }", "primitive: t.block", 1)
	r := newRig(t, yaml)
	r.start()

	applied := make(chan error, 1)
	go func() { applied <- r.e.Apply(ctx) }()
	waitFor(t, "the step to start applying", func() bool { return contains(r.kinds(), EventStepApplying) })

	if err := r.e.Apply(ctx); !errors.Is(err, ErrBusy) {
		t.Errorf("a second operation = %v, want ErrBusy", err)
	}
	if err := r.e.Continue(ctx); !errors.Is(err, ErrBusy) {
		t.Errorf("Continue during an operation = %v, want ErrBusy", err)
	}
	if b := r.step("break"); b.State != StepApplying || b.CanApply || b.CanVerify || b.CanContinue {
		t.Errorf("break = %+v, want it applying with every button closed", b)
	}

	snap, err := r.e.End(ctx)
	if err != nil {
		t.Fatalf("End: %v", err)
	}
	if got := <-applied; !errors.Is(got, context.Canceled) {
		t.Errorf("the operation in flight returned %v, want it cancelled by End", got)
	}
	if snap.Result != "ended early" || r.containers() != 0 {
		t.Errorf("result=%q containers=%d, want ended early and everything removed", snap.Result, r.containers())
	}
}

func TestEnd(t *testing.T) {
	t.Run("ending early removes everything and returns the final state", func(t *testing.T) {
		r := newRig(t, testScenario)
		r.start()
		r.ok(r.e.SetNotes("break", "n"))
		r.clock.Advance(time.Minute)

		snap, err := r.e.End(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if snap.Result != "ended early" || snap.RunID != "run1" || snap.Steps[0].Notes != "n" || !snap.EndedAt.Equal(r.clock.Now()) {
			t.Errorf("final snapshot = %+v", snap)
		}
		if r.containers() != 0 {
			t.Error("containers were left behind (R-SCN-10)")
		}
		kinds := r.kinds()
		if n := len(kinds); n < 2 || kinds[n-2] != EventTeardown || kinds[n-1] != EventRunEnded {
			t.Errorf("events = %v, want teardown then run-ended last", kinds)
		}
		if r.e.Snapshot().Status != StatusIdle {
			t.Error("the engine is not idle after End")
		}
		if _, err := r.e.End(ctx); !errors.Is(err, ErrNoRun) {
			t.Errorf("a second End = %v, want ErrNoRun", err)
		}
	})

	t.Run("a finished run ends as completed", func(t *testing.T) {
		r := newRig(t, `id: one
name: One
summary: s
product: vault
editions: [community]
difficulty: beginner
estimatedMinutes: 1
tags: [t]
objectives: [o]
prerequisites: [p]
parameters:
  - id: version
    type: version
    product: vault
    label: V
topology:
  clusters:
    - name: primary
      product: vault
      nodes: 1
initialState: i
failureCreated: f
successCriteria: s
pitfalls: [p]
references:
  - title: d
    url: https://example.com
steps:
  - id: only
    kind: verification
    title: Only
    guide: steps/break.md
    verify:
      - primitive: t.expect
        args: { key: ok }
`)
		r.start()
		r.world.set("ok", true)
		r.ok(r.e.Verify(ctx))
		r.ok(r.e.Continue(ctx))
		if r.e.Snapshot().Status != StatusFinished {
			t.Fatal("the run did not finish")
		}
		snap, err := r.e.End(ctx)
		if err != nil || snap.Result != "completed" {
			t.Errorf("End = %q, %v, want completed", snap.Result, err)
		}
	})

	t.Run("a sandbox has no steps and ends as completed", func(t *testing.T) {
		r := newRig(t, sandboxScenario)
		r.start()
		if s := r.e.Snapshot(); s.Status != StatusActive || s.ActiveStep != -1 || len(s.Steps) != 0 {
			t.Fatalf("sandbox snapshot = %+v", s)
		}
		snap, err := r.e.End(ctx)
		if err != nil || snap.Result != "completed" {
			t.Errorf("End = %q, %v, want completed (R-SCN-17)", snap.Result, err)
		}
	})

	t.Run("a failed removal is reported and the run is closed anyway", func(t *testing.T) {
		r := newRig(t, testScenario)
		r.start()
		r.rt.FailNext(fake.OpRemove, errors.New("device busy"))

		snap, err := r.e.End(ctx)
		if err == nil || !strings.Contains(err.Error(), "device busy") {
			t.Fatalf("End = %v, want the failed removal reported", err)
		}
		if snap.RunID != "run1" || snap.Result != "ended early" {
			t.Errorf("snapshot = %+v, want it returned even though teardown failed", snap)
		}
		if r.e.Snapshot().Status != StatusIdle {
			t.Error("the run was left open after a failed teardown")
		}
		found := false
		for _, ev := range snap.Events {
			found = found || ev.Kind == EventTeardown && strings.Contains(ev.Message, "did not finish")
		}
		if !found {
			t.Error("the failed teardown was not recorded as an event")
		}
	})

	t.Run("another run can start afterwards", func(t *testing.T) {
		r := newRig(t, testScenario)
		r.start()
		if _, err := r.e.End(ctx); err != nil {
			t.Fatal(err)
		}
		r.start()
		if s := r.e.Snapshot(); s.RunID != "run2" || s.Status != StatusActive {
			t.Errorf("snapshot = %+v", s)
		}
	})
}

func TestArgumentsAreRenderedAndOutputsFlowToLaterCalls(t *testing.T) {
	yaml := strings.Replace(testScenario, "action:\n      - primitive: t.set\n        args: { key: broken }\n    verify:\n      - primitive: t.expect\n        args: { key: broken }",
		`action:
      - primitive: t.echo
        args: { text: "{{ .Nodes.node_1.IP }}/{{ .Params.version }}" }
    verify:
      - primitive: t.echo
        args: { text: "{{ .Outputs.echoed }}-{{ .Creds.primary.rootToken }}" }`, 1)
	r := newRig(t, yaml)
	r.start()
	r.ok(r.e.Apply(ctx))

	if len(r.rec.args) != 2 {
		t.Fatalf("recorded %d calls", len(r.rec.args))
	}
	if got := r.rec.args[0].String("text"); got != "10.89.1.2/1.21.0" {
		t.Errorf("first call got %q, want node, IP and parameter filled in", got)
	}
	if got := r.rec.args[1].String("text"); got != "10.89.1.2/1.21.0-"+rootToken {
		t.Errorf("second call got %q, want the first call's output and the credential filled in", got)
	}
	b := r.step("break")
	if b.Last.Status != "pass" || b.Last.At.IsZero() {
		t.Errorf("break outcome = %+v, want a pass stamped with the time it ended", b.Last)
	}
}

func TestATemplateErrorIsAnErrorOutcome(t *testing.T) {
	yaml := strings.Replace(testScenario, "args: { key: broken }\n    verify", `args: { key: "{{ .Outputs.neverProduced }}" }
    verify`, 1)
	r := newRig(t, yaml)
	r.start()
	r.ok(r.e.Apply(ctx))

	b := r.step("break")
	if b.State != StepActive || b.Last.Status != "error" || !strings.Contains(b.Last.Message, "t.set") {
		t.Errorf("break = %+v, want an error outcome that names the primitive and keeps the step active", b)
	}
}

// Rollback ---------------------------------------------------------------------

// reachBreakAgain plays the run to the point where break-again is active, with
// break and fix complete, a note on break and some time spent.
func reachBreakAgain(r *rig) {
	r.t.Helper()
	r.start()
	r.clock.Advance(4 * time.Minute)
	r.ok(r.e.Apply(ctx))
	r.ok(r.e.SetNotes("break", "Started with the logs."))
	r.ok(r.e.Continue(ctx))
	r.clock.Advance(6 * time.Minute)
	r.world.set("fixed", true)
	r.ok(r.e.Verify(ctx))
	r.ok(r.e.Continue(ctx))
	r.clock.Advance(1 * time.Minute)
}

func TestRollbackRedeploysAndReplaysTheActions(t *testing.T) {
	r := newRig(t, testScenario)
	reachBreakAgain(r)
	if r.step("break-again").State != StepActive {
		t.Fatal("setup did not reach break-again")
	}
	if !r.step("fix").CanRollBack || r.step("break").CanRollBack {
		t.Error("only the completed reset point should offer a roll back")
	}

	r.ok(r.e.Rollback(ctx, "fix"))

	if r.dep.count() != 2 || r.containers() != 2 {
		t.Errorf("deploys=%d containers=%d, want one redeploy that replaced the environment", r.dep.count(), r.containers())
	}
	if !r.world.get("broken") || r.world.get("fixed") {
		t.Errorf("world broken=%v fixed=%v, want the breakage replayed and the trainee's fix gone", r.world.get("broken"), r.world.get("fixed"))
	}

	// The reset point is the verification step: the breakage before it is
	// replayed, so the trainee is back at "fix".
	if r.step("break").State != StepComplete || r.step("fix").State != StepActive ||
		r.step("break-again").State != StepLocked || r.step("fix-again").State != StepLocked {
		t.Errorf("states = %s %s %s %s", r.step("break").State, r.step("fix").State, r.step("break-again").State, r.step("fix-again").State)
	}
	if s := r.e.Snapshot(); s.Status != StatusActive || s.ActiveStep != 1 {
		t.Errorf("snapshot = %+v", s)
	}

	// Times and notes are kept, and the roll back is recorded (4.7).
	if n := r.step("break").Notes; n != "Started with the logs." {
		t.Errorf("notes = %q, want them kept", n)
	}
	fix := r.step("fix")
	if fix.Attempts != 2 || fix.Verified || fix.Elapsed != 6*time.Minute+0 {
		t.Errorf("fix = attempts %d verified %v elapsed %v, want a second attempt that remembers the first (6m)", fix.Attempts, fix.Verified, fix.Elapsed)
	}
	if ba := r.step("break-again"); ba.Attempts != 1 || ba.Elapsed != 1*time.Minute {
		t.Errorf("break-again = attempts %d elapsed %v, want its earlier minute kept", ba.Attempts, ba.Elapsed)
	}
	rollbacks := 0
	for _, k := range r.kinds() {
		if k == EventRollback {
			rollbacks++
		}
	}
	if rollbacks != 2 {
		t.Errorf("rollback events = %d, want one at the start and one when it finished", rollbacks)
	}

	// The run carries on from there.
	r.world.set("fixed", true)
	r.ok(r.e.Verify(ctx))
	r.ok(r.e.Continue(ctx))
	if r.step("break-again").State != StepActive {
		t.Error("the run did not carry on after the roll back")
	}
}

func TestRollbackNeedsACompletedResetPoint(t *testing.T) {
	r := newRig(t, testScenario)
	if err := r.e.Rollback(ctx, "fix"); !errors.Is(err, ErrNoRun) {
		t.Errorf("Rollback with no run = %v, want ErrNoRun", err)
	}
	r.start()

	for _, tt := range []struct{ step, why string }{
		{"break", "is not a reset point"},
		{"fix", "has not been completed yet"},
	} {
		err := r.e.Rollback(ctx, tt.step)
		if !errors.Is(err, ErrWrongState) || !strings.Contains(err.Error(), tt.why) {
			t.Errorf("Rollback to %s = %v, want ErrWrongState %q", tt.step, err, tt.why)
		}
	}
	if err := r.e.Rollback(ctx, "nope"); err == nil || !strings.Contains(err.Error(), "no step") {
		t.Errorf("Rollback to an unknown step = %v", err)
	}
	if r.dep.count() != 1 {
		t.Error("a refused roll back still redeployed")
	}
}

func TestRollbackWithACustomReset(t *testing.T) {
	yaml := strings.Replace(testScenario, "    resetPoint: true\n", "    resetPoint: true\n    reset:\n      - primitive: t.set\n        args: { key: customReset }\n", 1)
	r := newRig(t, yaml)
	reachBreakAgain(r)

	r.ok(r.e.Rollback(ctx, "fix"))

	if r.dep.count() != 1 {
		t.Errorf("deploys = %d, want the custom reset to replace the redeploy", r.dep.count())
	}
	if !r.world.get("customReset") {
		t.Error("the step's own reset calls did not run")
	}
	if !r.world.get("fixed") {
		t.Error("the environment was rebuilt although the step brought its own reset")
	}
	if r.step("fix").State != StepActive || r.step("break-again").State != StepLocked || r.step("fix").Attempts != 2 {
		t.Errorf("states = %+v", r.e.Snapshot().Steps)
	}
}

func TestACustomResetThatFailsLeavesTheRunAsItWas(t *testing.T) {
	yaml := strings.Replace(testScenario, "    resetPoint: true\n", "    resetPoint: true\n    reset:\n      - primitive: t.fail\n", 1)
	r := newRig(t, yaml)
	reachBreakAgain(r)

	err := r.e.Rollback(ctx, "fix")
	if err == nil || !strings.Contains(err.Error(), "the reset failed") {
		t.Fatalf("Rollback = %v", err)
	}
	if s := r.e.Snapshot(); s.Status != StatusActive || s.ActiveStep != 2 {
		t.Errorf("snapshot = %+v, want the run left as it was", s)
	}
	if !contains(r.kinds(), EventRollbackFailed) {
		t.Error("the failed roll back was not recorded")
	}
	// It can be tried again.
	if err := r.e.Rollback(ctx, "fix"); err == nil || errors.Is(err, ErrBusy) {
		t.Errorf("a second try = %v", err)
	}
}

func TestRollbackThatCannotRedeployCanBeRetried(t *testing.T) {
	r := newRig(t, testScenario)
	reachBreakAgain(r)
	r.dep.fail = []error{errors.New("no space left")}

	err := r.e.Rollback(ctx, "fix")
	if err == nil || !strings.Contains(err.Error(), "no space left") {
		t.Fatalf("Rollback = %v", err)
	}
	if s := r.e.Snapshot(); s.Status != StatusDeployFailed {
		t.Fatalf("status = %s, want a failed deployment the instructor can retry or end", s.Status)
	}

	r.ok(r.e.Retry(ctx))
	if s := r.e.Snapshot(); s.Status != StatusActive || s.ActiveStep != 1 {
		t.Errorf("after Retry the snapshot is %+v, want the rollback finished", s)
	}
	if !r.world.get("broken") {
		t.Error("the replay did not run after the retry")
	}
}

func TestRollbackWhoseReplayFailsCanBeRetriedOrEnded(t *testing.T) {
	// break's check expects a flag that only the trainee-side setup sets, so the
	// replay on a fresh environment cannot confirm the breakage.
	yaml := strings.Replace(testScenario, "verify:\n      - primitive: t.expect\n        args: { key: broken }", "verify:\n      - primitive: t.expect\n        args: { key: neverAfterRedeploy }", 1)
	r := newRig(t, yaml)
	r.start()
	r.ok(r.e.Apply(ctx))
	r.world.set("neverAfterRedeploy", true)
	r.ok(r.e.Verify(ctx))
	r.ok(r.e.Continue(ctx))
	r.world.set("fixed", true)
	r.ok(r.e.Verify(ctx))
	r.ok(r.e.Continue(ctx))

	err := r.e.Rollback(ctx, "fix")
	if err == nil || !strings.Contains(err.Error(), "replaying step break, check") {
		t.Fatalf("Rollback = %v, want the replay failure named", err)
	}
	s := r.e.Snapshot()
	if s.Status != StatusDeployFailed || !strings.Contains(s.DeployError, "replaying step break") {
		t.Fatalf("snapshot = %+v", s)
	}
	if !contains(r.kinds(), EventRollbackFailed) {
		t.Error("the failed replay was not recorded")
	}

	if err := r.e.Retry(ctx); err == nil {
		t.Error("the retry of an impossible replay succeeded")
	}
	snap, err := r.e.End(ctx)
	if err != nil || snap.Result != "ended early" || r.containers() != 0 {
		t.Errorf("End = %q, %v, containers %d, want the run closed and cleaned up", snap.Result, err, r.containers())
	}
}

func TestTheScenarioFilesStayInsideTheScenario(t *testing.T) {
	r := newRig(t, testScenario)
	r.start()
	// The engine hands primitives the scenario's own ReadFile, which refuses to leave the folder.
	_, env, err := r.e.prepare(r.e.run, scenario.Call{Primitive: "t.set", Args: map[string]any{"key": "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.ReadFile("../outside"); err == nil {
		t.Error("a primitive could read outside the scenario folder (R-SCN-15)")
	}
	if b, err := env.ReadFile("steps/break.md"); err != nil || string(b) != "# break\n" {
		t.Errorf("ReadFile = %q, %v", b, err)
	}
}

// Section 4.6, the rows not covered above.

func TestAVerificationThatErrorsLooksDifferentFromOneThatFails(t *testing.T) {
	yaml := strings.Replace(testScenario, "    verify:\n      - primitive: t.expect\n        args: { key: fixed }", "    verify:\n      - primitive: t.error", 1)
	r := newRig(t, yaml)
	r.start()
	r.ok(r.e.Apply(ctx))
	r.ok(r.e.Continue(ctx))

	r.ok(r.e.Verify(ctx))
	f := r.step("fix")
	if f.Last.Status != "error" || !strings.Contains(f.Last.Message, "the container is gone") {
		t.Errorf("last = %+v, want a problem running the check shown as an error, not as red", f.Last)
	}
	if f.Verified || f.CanContinue || !f.CanVerify {
		t.Errorf("fix = %+v, want Continue closed and the check available again", f)
	}
}

func TestAnActionThatFailsMidwayOffersRetryAndRollBack(t *testing.T) {
	yaml := strings.Replace(testScenario, "primitive: t.set\n        args: { key: broken2 }", "primitive: t.fail", 1)
	r := newRig(t, yaml)
	reachBreakAgain(r)

	r.ok(r.e.Apply(ctx))
	ba := r.step("break-again")
	if ba.State != StepActive || ba.Last.Status != "fail" || !ba.CanApply {
		t.Fatalf("break-again = %+v, want it active with Retry offered", ba)
	}
	if !r.step("fix").CanRollBack {
		t.Error("Roll back is not offered although a reset point exists (4.6)")
	}
}

// TestAScenarioRunsStartToFinishWithRollBackAndEarlyEnd is the acceptance test
// for milestone M3: a fixture scenario with a start step, breakages and
// verifications runs start to finish against the fake runtime, including a roll
// back and ending early.
func TestAScenarioRunsStartToFinishWithRollBackAndEarlyEnd(t *testing.T) {
	r := newRig(t, testScenario)

	// Start step: deploy.
	r.start()
	if s := r.e.Snapshot(); s.Status != StatusActive || r.containers() != 2 {
		t.Fatalf("after Start: %+v", s)
	}

	// Breakage, then the fix.
	r.ok(r.e.Apply(ctx))
	r.ok(r.e.Continue(ctx))
	r.world.set("fixed", true)
	r.ok(r.e.Verify(ctx))
	r.ok(r.e.Continue(ctx))

	// The instructor wants to try the first breakage again: roll back.
	r.ok(r.e.Rollback(ctx, "fix"))
	if r.step("fix").State != StepActive || r.dep.count() != 2 {
		t.Fatal("the roll back did not return to the fix step")
	}

	// Carry on to the end.
	r.world.set("fixed", true)
	r.ok(r.e.Verify(ctx))
	r.ok(r.e.Continue(ctx))
	r.ok(r.e.Apply(ctx))
	r.ok(r.e.Continue(ctx))
	r.world.set("fixed2", true)
	r.ok(r.e.Verify(ctx))
	r.ok(r.e.Continue(ctx))
	if s := r.e.Snapshot(); s.Status != StatusFinished || s.Result != "completed" {
		t.Fatalf("the run did not finish: %+v", s)
	}
	snap, err := r.e.End(ctx)
	if err != nil || snap.Result != "completed" || r.containers() != 0 {
		t.Fatalf("End = %q, %v, containers %d", snap.Result, err, r.containers())
	}
	if !contains(eventKinds(snap.Events), EventRollback) {
		t.Error("the roll back is missing from the report's events")
	}

	// A second run is ended early, part way through.
	r.start()
	r.ok(r.e.Apply(ctx))
	snap, err = r.e.End(ctx)
	if err != nil || snap.Result != "ended early" {
		t.Fatalf("End = %q, %v, want ended early", snap.Result, err)
	}
	if got := snap.Steps[0].State; got != StepApplied {
		t.Errorf("the final snapshot records break as %s, want the state it was in when the run ended (R-REPORT-6)", got)
	}
	if cs, _ := r.rt.List(ctx, "run2"); len(cs) != 0 {
		t.Error("the second run left containers behind")
	}
}

func eventKinds(evs []Event) []EventKind {
	out := make([]EventKind, len(evs))
	for i, e := range evs {
		out[i] = e.Kind
	}
	return out
}
