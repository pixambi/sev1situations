//go:build integration

package app

import (
	"strings"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/engine"
)

// TestSessionLimitsOnRealPodman plays the scenario end to end on a real machine (R-CONTRIB-4), and proves the symptom the
// briefing tells the instructor to expect: with one connection allowed, the target says so.
func TestSessionLimitsOnRealPodman(t *testing.T) {
	r := playScenario(t, "boundary-session-limits", "boundary", 15*time.Minute)
	eng, ctx := r.eng, r.ctx
	r.start()

	targetID := r.secretFact("boundary", "Target ID")
	authMethod := r.secretFact("boundary", "Auth method ID")
	password := r.secretFact("boundary", "Admin password")
	signIn := r.sh("node-1", "boundary authenticate password -auth-method-id="+authMethod+" -login-name=admin -password=env://PW -keyring-type=none -format=json",
		map[string]string{"BOUNDARY_ADDR": "http://127.0.0.1:9200", "PW": password}, false)
	token := strings.TrimSpace(r.sh("node-1", "echo '"+signIn.Stdout+"' | sed -n 's/.*\"token\": *\"\\([^\"]*\\)\".*/\\1/p' | head -1", nil, false).Stdout)
	admin := map[string]string{"BOUNDARY_ADDR": "http://127.0.0.1:9200", "BOUNDARY_TOKEN": token}

	r.step("apply the limits", func() error { return eng.Apply(ctx) })
	r.mustPass("apply-the-limits")
	read := r.sh("node-1", "boundary targets read -id="+targetID, admin, false).Stdout
	if !strings.Contains(read, "Session Connection Limit:") || !strings.Contains(read, "Session Max Seconds:") || !strings.Contains(read, "60") {
		t.Fatalf("the target does not show the limits the briefing describes:\n%s", read)
	}
	if err := eng.Continue(ctx); err != nil {
		t.Fatal(err)
	}

	r.step("verify before the fix fails", func() error { return eng.Verify(ctx) })
	if o := r.outcome("limits-corrected"); o == nil || o.Status == "pass" {
		t.Fatalf("the check passed with the limits still in place: %+v", o)
	}

	r.step("update the target", func() error {
		res := r.sh("node-1", "boundary targets update tcp -id="+targetID+" -session-connection-limit=-1 -session-max-seconds=28800", admin, false)
		if res.ExitCode != 0 {
			t.Fatalf("the guide's command failed: %+v", res)
		}
		return nil
	})
	r.step("verify the fix", func() error { return eng.Verify(ctx) })
	if o := r.outcome("limits-corrected"); o == nil || o.Status != "pass" {
		t.Fatalf("the repair was not accepted: %+v", o)
	}
	if err := eng.Continue(ctx); err != nil {
		t.Fatal(err)
	}
	if s := eng.Snapshot(); s.Status != engine.StatusFinished || s.Result != "completed" {
		t.Errorf("after the last step: status %s, result %q", s.Status, s.Result)
	}
	r.finish(2)
}
