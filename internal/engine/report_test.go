package engine

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	htmlreport "github.com/pixambi/sev1situations/internal/report/html"
)

func TestAnEarlyEndSavesAReportWithEveryStepAsItWasLeft(t *testing.T) {
	r := newRig(t, testScenario)
	r.start()
	r.clock.Advance(2 * time.Minute)
	r.ok(r.e.Apply(ctx))
	r.ok(r.e.SetNotes("break", "trainee looked at the logs first"))
	r.ok(r.e.SetRating("break", 4))
	r.clock.Advance(3 * time.Minute)
	r.ok(r.e.Continue(ctx))
	r.clock.Advance(5 * time.Minute)

	if _, err := r.e.End(ctx); err != nil {
		t.Fatal(err)
	}

	all := r.reports.all()
	if len(all) != 1 {
		t.Fatalf("%d reports saved, want 1", len(all))
	}
	rep := all[0]
	if rep.Result != "ended early" || rep.Instructor != "Ada" || rep.Trainee != "Grace" {
		t.Errorf("header = %+v", rep)
	}
	if rep.Scenario.ID != "test-scenario" || rep.Scenario.Name != "Test Scenario" || rep.Scenario.Product != "vault" {
		t.Errorf("scenario = %+v", rep.Scenario)
	}
	if rep.Params["version"] != "1.21.0" {
		t.Errorf("params = %v", rep.Params)
	}
	if got := rep.Duration(); got != 10*time.Minute {
		t.Errorf("duration = %v, want 10m from the fake clock", got)
	}
	if !strings.HasPrefix(rep.ID, "20261126-090000-run") {
		t.Errorf("id = %q", rep.ID)
	}

	if len(rep.Steps) != 4 {
		t.Fatalf("%d steps", len(rep.Steps))
	}
	brk, fix, later := rep.Steps[0], rep.Steps[1], rep.Steps[2]
	if brk.State != "complete" || !brk.Verified || brk.Notes != "trainee looked at the logs first" || brk.Rating != 4 || brk.Attempts != 1 {
		t.Errorf("break = %+v", brk)
	}
	if brk.Outcome == nil || brk.Outcome.Status != "pass" {
		t.Errorf("break outcome = %+v", brk.Outcome)
	}
	// The step was active from the start of the run until Continue, 2 + 3 minutes on the fake clock.
	if brk.DurationMs != (5 * time.Minute).Milliseconds() {
		t.Errorf("break duration = %d ms, want 5 minutes", brk.DurationMs)
	}
	if fix.State != "active" || !fix.EndedAt.IsZero() || fix.Attempts != 1 {
		t.Errorf("the step left active = %+v", fix)
	}
	if later.State != "locked" || later.Attempts != 0 || !later.StartedAt.IsZero() {
		t.Errorf("a step never reached = %+v", later)
	}
}

func TestACompletedRunAndASandboxAreRecordedAsCompleted(t *testing.T) {
	r := newRig(t, testScenario)
	r.start()
	r.ok(r.e.Apply(ctx))
	r.ok(r.e.Continue(ctx))
	r.world.set("fixed", true)
	r.ok(r.e.Verify(ctx))
	r.ok(r.e.Continue(ctx))
	r.ok(r.e.Apply(ctx))
	r.ok(r.e.Continue(ctx))
	r.world.set("fixed2", true)
	r.ok(r.e.Verify(ctx))
	r.ok(r.e.Continue(ctx))
	if _, err := r.e.End(ctx); err != nil {
		t.Fatal(err)
	}
	rep := r.reports.all()[0]
	if rep.Result != "completed" {
		t.Errorf("result = %q", rep.Result)
	}
	for _, s := range rep.Steps {
		if s.State != "complete" {
			t.Errorf("step %s is %s", s.ID, s.State)
		}
	}

	sb := newRig(t, sandboxScenario)
	sb.start()
	if _, err := sb.e.End(ctx); err != nil {
		t.Fatal(err)
	}
	if got := sb.reports.all(); len(got) != 1 || got[0].Result != "completed" || len(got[0].Steps) != 0 {
		t.Errorf("sandbox report = %+v", got)
	}
}

func TestOnlyTheNotableEventsAreKept(t *testing.T) {
	r := newRig(t, testScenario)
	r.start()
	r.ok(r.e.Apply(ctx))
	r.ok(r.e.Continue(ctx))
	r.world.set("fixed", true)
	r.ok(r.e.Verify(ctx))
	r.ok(r.e.Continue(ctx)) // "fix" is complete, so the run can be rolled back to it
	r.ok(r.e.Rollback(ctx, "fix"))
	r.ok(r.e.NodeAction(ctx, "node-1", ActionStop))
	if _, err := r.e.End(ctx); err != nil {
		t.Fatal(err)
	}

	var kinds []string
	for _, ev := range r.reports.all()[0].Events {
		kinds = append(kinds, ev.Kind)
	}
	for _, want := range []string{"rollback", "node-action", "run-ended"} {
		found := false
		for _, k := range kinds {
			found = found || k == want
		}
		if !found {
			t.Errorf("events %v lack %q", kinds, want)
		}
	}
	for _, k := range kinds {
		if k == "deploy-progress" || k == "step-applying" || k == "verification" {
			t.Errorf("a progress event %q is in the report", k)
		}
	}
	evs := r.reports.all()[0].Events
	for i := 1; i < len(evs); i++ {
		if evs[i].At.Before(evs[i-1].At) {
			t.Errorf("events are out of order at %d", i)
		}
	}
}

func TestARetryAfterAFailedDeploymentIsRecordedAndTheRunStillGetsAReport(t *testing.T) {
	r := newRig(t, testScenario)
	r.dep.fail = []error{errors.New("the image could not be pulled")}
	if err := r.e.Start(ctx, r.sc, StartInput{Instructor: "Ada", Trainee: "Grace", Params: map[string]any{"version": "1.21.0"}}); err == nil {
		t.Fatal("the deployment was meant to fail")
	}
	r.ok(r.e.Retry(ctx))
	if _, err := r.e.End(ctx); err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, ev := range r.reports.all()[0].Events {
		kinds = append(kinds, ev.Kind)
	}
	if !strings.Contains(strings.Join(kinds, ","), "deploy-failed") || !strings.Contains(strings.Join(kinds, ","), "retry") {
		t.Errorf("events = %v, want the failed deployment and the retry", kinds)
	}
}

func TestNoCredentialReachesAReport(t *testing.T) {
	r := newRig(t, testScenario)
	r.start()
	r.ok(r.e.Apply(ctx))
	r.ok(r.e.Continue(ctx)) // now on the "fix" step, whose check fails and echoes the token in its output
	r.ok(r.e.Verify(ctx))
	r.ok(r.e.SetNotes("fix", "tried the token "+rootToken+" and it worked"))
	// The names are typed by hand, and could hold one too.
	if _, err := r.e.End(ctx); err != nil {
		t.Fatal(err)
	}

	raw, _ := json.Marshal(r.reports.all()[0])
	if strings.Contains(string(raw), rootToken) || strings.Contains(string(raw), "TestRootTokenValue") {
		t.Errorf("a credential is in the report:\n%s", raw)
	}
	fix := r.reports.all()[0].Steps[1]
	if !strings.Contains(fix.Notes, "tried the token") || !strings.Contains(fix.Notes, "and it worked") {
		t.Errorf("the rest of the note was lost: %q", fix.Notes)
	}
	if fix.Outcome == nil || fix.Outcome.Status != "fail" {
		t.Errorf("outcome = %+v", fix.Outcome)
	}
	if strings.Contains(string(raw), "output with") {
		t.Error("raw evidence is in the report, which keeps only the outcome's message")
	}
}

func TestAFailedSaveIsReportedButTheRunStillEnds(t *testing.T) {
	r := newRig(t, testScenario)
	r.start()
	r.reports.fail = errors.New("the disk is full")

	snap, err := r.e.End(ctx)
	if err == nil || !strings.Contains(err.Error(), "save the report") || !strings.Contains(err.Error(), "the disk is full") {
		t.Fatalf("err = %v", err)
	}
	if snap.Result == "" || r.e.Snapshot().Status != StatusIdle || r.containers() != 0 {
		t.Errorf("the run did not end cleanly: result %q, status %s, containers %d", snap.Result, r.e.Snapshot().Status, r.containers())
	}
}

func TestAnEngineWithNoReportSaverStillEnds(t *testing.T) {
	r := newRig(t, testScenario)
	r.e.deps.Reports = nil
	r.start()
	if _, err := r.e.End(ctx); err != nil {
		t.Fatal(err)
	}
	if len(r.reports.all()) != 0 {
		t.Error("a report was saved with no saver")
	}
}

func TestTheRenderedReportHoldsNoCredentialEitherWayItIsReached(t *testing.T) {
	r := newRig(t, testScenario)
	r.start()
	r.ok(r.e.Apply(ctx))
	r.ok(r.e.Continue(ctx))
	r.ok(r.e.Verify(ctx)) // fails, and echoes the token in its output
	r.ok(r.e.SetNotes("fix", "root token was "+rootToken+"; trainee pasted it into chat"))
	if _, err := r.e.End(ctx); err != nil {
		t.Fatal(err)
	}

	page, err := htmlreport.Renderer{}.Render(r.reports.all()[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{rootToken, "TestRootTokenValue"} {
		if strings.Contains(string(page), secret) {
			t.Errorf("the HTML report contains %q", secret)
		}
	}
	if !strings.Contains(string(page), "root token was") {
		t.Error("the rest of the note is missing from the page")
	}
}
