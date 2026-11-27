//go:build integration

package app

import (
	"strings"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/engine"
)

// TestAuthorizeSession403OnRealPodman plays the scenario end to end on a real machine (R-CONTRIB-4). The repair is typed into
// the controller as the step's guide tells the instructor to, so the guide's commands are proved.
func TestAuthorizeSession403OnRealPodman(t *testing.T) {
	r := playScenario(t, "boundary-authorize-session-403", "boundary", 15*time.Minute)
	eng, ctx := r.eng, r.ctx
	r.start()

	env := map[string]string{"BOUNDARY_ADDR": "http://127.0.0.1:9200"}
	targetID := r.secretFact("boundary", "Target ID")
	authMethod := r.secretFact("boundary", "Auth method ID")
	adminPassword := r.secretFact("boundary", "Admin password")

	r.step("apply the breakage", func() error { return eng.Apply(ctx) })
	r.mustPass("create-permission-problem")

	// What the trainee sees as jsmith: signed in, the target visible, no session.
	asUser := func(login, password string) map[string]string {
		res := r.sh("node-1", "boundary authenticate password -auth-method-id="+authMethod+" -login-name="+login+
			" -password=env://PW -keyring-type=none -format=json", map[string]string{"BOUNDARY_ADDR": env["BOUNDARY_ADDR"], "PW": password}, false)
		if res.ExitCode != 0 {
			t.Fatalf("sign in as %s: %s", login, res.Stderr)
		}
		out := r.sh("node-1", "echo '"+res.Stdout+"' | sed -n 's/.*\"token\": *\"\\([^\"]*\\)\".*/\\1/p' | head -1", nil, false)
		return map[string]string{"BOUNDARY_ADDR": env["BOUNDARY_ADDR"], "BOUNDARY_TOKEN": strings.TrimSpace(out.Stdout)}
	}
	jsmith := asUser("jsmith", adminPassword)
	if list := r.sh("node-1", "boundary targets list -recursive", jsmith, false); !strings.Contains(list.Stdout, "ttcp_") {
		t.Fatalf("jsmith cannot see the target: %+v", list)
	}
	denied := r.sh("node-1", "boundary targets authorize-session -id="+targetID, jsmith, false)
	if denied.ExitCode == 0 || !strings.Contains(denied.Stdout+denied.Stderr, "403") {
		t.Fatalf("jsmith was not refused with a 403: %+v", denied)
	}
	if err := eng.Continue(ctx); err != nil {
		t.Fatal(err)
	}

	r.step("verify before the fix fails", func() error { return eng.Verify(ctx) })
	if o := r.outcome("access-restored"); o == nil || o.Status == "pass" {
		t.Fatalf("the check passed with jsmith still denied: %+v", o)
	}

	// The repair, as the guide gives it.
	admin := asUser("admin", adminPassword)
	r.step("add the grant", func() error {
		roles := r.sh("node-1", "boundary roles list -scope-id=$(boundary scopes list -recursive -format=json | sed -n 's/.*\"id\": *\"\\(o_[^\"]*\\)\".*/\\1/p' | head -1)", admin, false)
		var roleID string
		for _, f := range strings.Fields(roles.Stdout) {
			if strings.HasPrefix(f, "r_") {
				roleID = f
				break
			}
		}
		if roleID == "" {
			t.Fatalf("the role is not listed as the guide says: %+v", roles)
		}
		res := r.sh("node-1", "boundary roles add-grants -id="+roleID+" -grant='ids="+targetID+";actions=authorize-session'", admin, false)
		if res.ExitCode != 0 {
			t.Fatalf("adding the grant: %+v", res)
		}
		return nil
	})

	r.step("verify the fix", func() error { return eng.Verify(ctx) })
	if o := r.outcome("access-restored"); o == nil || o.Status != "pass" {
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
