package gui

import (
	"context"
	"strings"
	"testing"

	"github.com/pixambi/sev1situations/internal/engine"
)

func twoStep(t *testing.T) (*backend, *RunService) {
	t.Helper()
	b := newBackend(t, map[string]string{"s": scenarioYAML("demo", "Demo", "vault", true)})
	return b, b.runService(context.Background())
}

func form() StartForm {
	return StartForm{ScenarioID: "demo", Instructor: "Ada", Trainee: "Grace", Params: map[string]any{"nodes": float64(3)}}
}

func TestStartChecksTheFormBeforeTouchingTheEngine(t *testing.T) {
	b, svc := twoStep(t)
	long := strings.Repeat("x", maxNameLength+1)
	tests := []struct {
		name string
		edit func(*StartForm)
		want string
	}{
		{"no scenario", func(f *StartForm) { f.ScenarioID = " " }, "Choose a scenario"},
		{"no instructor", func(f *StartForm) { f.Instructor = "" }, "instructor"},
		{"no trainee", func(f *StartForm) { f.Trainee = "  " }, "trainee"},
		{"a very long name", func(f *StartForm) { f.Trainee = long }, "at most"},
		{"an unknown scenario", func(f *StartForm) { f.ScenarioID = "nope" }, `"nope"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := form()
			tt.edit(&f)
			_, err := svc.Start(f)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want one mentioning %q", err, tt.want)
			}
			if got := b.eng.Snapshot().Status; got != engine.StatusIdle {
				t.Errorf("status = %s, a refused form must not start a run", got)
			}
		})
	}
}

func TestStartDeploysAndAcceptsParametersAsTheFrontendSendsThem(t *testing.T) {
	b, svc := twoStep(t)

	// JSON numbers reach Go as float64.
	snap, err := svc.Start(form())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Status != engine.StatusActive || snap.ScenarioID != "demo" || snap.Instructor != "Ada" || snap.Trainee != "Grace" {
		t.Errorf("snapshot = %+v", snap)
	}
	if len(snap.Nodes) != 3 {
		t.Errorf("nodes = %d, want the 3 that were asked for", len(snap.Nodes))
	}
	if got := b.out.named(EventRunState); len(got) == 0 {
		t.Error("the new state was not published")
	}
}

func TestTheFullFlowThroughTheServices(t *testing.T) {
	_, svc := twoStep(t)
	if _, err := svc.Start(form()); err != nil {
		t.Fatal(err)
	}

	snap, err := svc.RunAction("break")
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Steps[0].Verified || !snap.Steps[0].CanContinue {
		t.Fatalf("after applying: %+v", snap.Steps[0])
	}
	if snap, err = svc.SaveNotes("break", "trainee found it fast"); err != nil || snap.Steps[0].Notes != "trainee found it fast" {
		t.Fatalf("notes: %v %+v", err, snap.Steps[0])
	}
	if snap, err = svc.SetRating("break", 4); err != nil || snap.Steps[0].Rating != 4 {
		t.Fatalf("rating: %v %+v", err, snap.Steps[0])
	}
	if snap, err = svc.Continue("break"); err != nil || snap.ActiveStep != 1 {
		t.Fatalf("continue: %v active=%d", err, snap.ActiveStep)
	}

	// The fix has not been made, so the check fails and Continue is refused by the engine.
	if snap, err = svc.Verify("fix"); err != nil || snap.Steps[1].Last == nil || snap.Steps[1].Last.Status != "fail" {
		t.Fatalf("verify: %v %+v", err, snap.Steps[1].Last)
	}
	if _, err = svc.Continue("fix"); err == nil || !strings.Contains(err.Error(), "cannot be done at this point") {
		t.Fatalf("continue without a passing check: %v", err)
	}

	final, err := svc.End()
	if err != nil {
		t.Fatal(err)
	}
	if final.Result != "ended early" || svc.Current().Status != engine.StatusIdle {
		t.Errorf("final result = %q, current = %s", final.Result, svc.Current().Status)
	}
}

func TestStepActionsAreRefusedForAStepThatIsNotActive(t *testing.T) {
	b, svc := twoStep(t)
	if _, err := svc.RunAction("break"); err == nil {
		t.Error("an action with no run was accepted")
	}
	if _, err := svc.Start(form()); err != nil {
		t.Fatal(err)
	}
	for name, call := range map[string]func(string) (engine.Snapshot, error){
		"RunAction": svc.RunAction, "Verify": svc.Verify, "Continue": svc.Continue,
	} {
		if _, err := call("fix"); err == nil || !strings.Contains(err.Error(), "not the one in progress") {
			t.Errorf("%s on a locked step: %v", name, err)
		}
	}
	if b.eng.Snapshot().Steps[0].Last != nil {
		t.Error("a refused call still reached the engine")
	}
}

func TestNotesRatingAndNodeActionsAreChecked(t *testing.T) {
	_, svc := twoStep(t)
	if _, err := svc.Start(form()); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.SaveNotes("break", strings.Repeat("n", maxNotesLength+1)); err == nil {
		t.Error("oversized notes were accepted")
	}
	for _, n := range []int{-1, 6} {
		if _, err := svc.SetRating("break", n); err == nil {
			t.Errorf("rating %d was accepted", n)
		}
	}
	if _, err := svc.NodeAction("", "stop"); err == nil {
		t.Error("an empty node was accepted")
	}
	if _, err := svc.NodeAction("node-1", "explode"); err == nil || !strings.Contains(err.Error(), "explode") {
		t.Errorf("an unknown action: %v", err)
	}
	if _, err := svc.Rollback(" "); err == nil {
		t.Error("a rollback with no step was accepted")
	}
}

func TestEngineErrorsReachTheInstructorInPlainLanguage(t *testing.T) {
	_, svc := twoStep(t)
	if _, err := svc.Continue("break"); err == nil {
		t.Fatal("expected an error with no run")
	}
	if _, err := svc.Retry(); err == nil || !strings.Contains(err.Error(), "No scenario is running") {
		t.Errorf("retry with no run: %v", err)
	}
	if _, err := svc.Start(form()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start(form()); err == nil || strings.Contains(err.Error(), "engine") {
		t.Errorf("a second start: %v", err)
	}
}

func TestEndWithNoRunSaysSo(t *testing.T) {
	_, svc := twoStep(t)
	if _, err := svc.End(); err == nil || !strings.Contains(err.Error(), "No scenario is running") {
		t.Errorf("err = %v", err)
	}
}
