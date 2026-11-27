package boundary

import (
	"context"
	"strings"
	"testing"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/runtime"
)

// primbed is a deployed cluster and the registry its primitives are run through.
type primbed struct {
	*testbed
	d   product.Deployment
	reg *primitive.Registry
	env primitive.Env
}

func newPrimbed(t *testing.T) *primbed {
	t.Helper()
	tb := newTestbed(t, 1)
	d := tb.mustDeploy()
	reg := primitive.NewRegistry()
	for _, p := range tb.p.Primitives() {
		if err := reg.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	nodes := map[string]primitive.Node{}
	for _, n := range d.Nodes {
		nodes[n.Name] = n
	}
	env := primitive.Env{
		Runtime: tb.rt, Run: tb.spec.Run, Nodes: nodes,
		Creds:    map[string]map[string]string{"boundary": d.Creds},
		Redactor: primitive.NewRedactor(d.Creds["adminPassword"], simToken),
	}
	return &primbed{testbed: tb, d: d, reg: reg, env: env}
}

func (b *primbed) run(name string, args map[string]any) primitive.Result {
	b.t.Helper()
	if args["node"] == nil {
		args["node"] = "node-1"
	}
	res, err := b.reg.Run(context.Background(), name, b.env, args)
	if err != nil {
		b.t.Fatalf("%s: %v", name, err)
	}
	return res
}

func (b *primbed) runErr(name string, args map[string]any) error {
	b.t.Helper()
	if args["node"] == nil {
		args["node"] = "node-1"
	}
	_, err := b.reg.Run(context.Background(), name, b.env, args)
	return err
}

func (b *primbed) reply(key string, r runtime.ExecResult) {
	b.sim.mu.Lock()
	defer b.sim.mu.Unlock()
	if b.sim.reply == nil {
		b.sim.reply = map[string]runtime.ExecResult{}
	}
	b.sim.reply[key] = r
}

func TestEveryBoundaryPrimitiveIsNamedAndDescribed(t *testing.T) {
	prims := New(Options{}).Primitives()
	if len(prims) != 5 {
		t.Fatalf("%d primitives, want the five section 4.5 lists", len(prims))
	}
	for _, p := range prims {
		s := p.Spec()
		if !strings.HasPrefix(s.Name, "boundary.") || s.Summary == "" || s.Timeout <= 0 {
			t.Errorf("primitive %+v is not named, described and bounded", s)
		}
	}
}

func TestCliRunsACommandSignedInAsTheAdminWithoutPuttingTheTokenOnTheCommandLine(t *testing.T) {
	b := newPrimbed(t)
	b.reply("scopes list", runtime.ExecResult{Stdout: `{"items":[{"name":"global"}]}`})

	res := b.run("boundary.cli", map[string]any{"command": []string{"scopes", "list"}, "contains": "global", "save": "scopes"})
	if res.Status != primitive.Pass || res.Outputs["scopes"] == "" {
		t.Fatalf("result = %+v", res)
	}
	var ran []string
	for _, c := range b.sim.callsOn("node-1", binaryPath, "scopes", "list") {
		ran = c.argv
		if c.env["BOUNDARY_TOKEN"] != simToken {
			t.Errorf("the command ran with the token %q, want the admin's", c.env["BOUNDARY_TOKEN"])
		}
	}
	if strings.Join(ran, " ") != binaryPath+" scopes list -token=env://BOUNDARY_TOKEN" {
		t.Errorf("command line = %v", ran)
	}
	for _, c := range b.sim.callsOn("", binaryPath) {
		for _, a := range c.argv {
			if strings.Contains(a, simToken) || strings.Contains(a, simPassword) {
				t.Errorf("a secret is on a command line: %v", c.argv)
			}
		}
	}
}

func TestCliChecksTheExitCodeAndTheOutput(t *testing.T) {
	b := newPrimbed(t)
	b.reply("scopes list", runtime.ExecResult{ExitCode: 1, Stderr: "boom"})
	if res := b.run("boundary.cli", map[string]any{"command": []string{"scopes", "list"}}); res.Status != primitive.Fail || !strings.Contains(res.Message, "exited with 1") {
		t.Errorf("a failing command: %+v", res)
	}
	if res := b.run("boundary.cli", map[string]any{"command": []string{"scopes", "list"}, "expectExit": 1}); res.Status != primitive.Pass {
		t.Errorf("an expected failure: %+v", res)
	}
	b.reply("scopes list", runtime.ExecResult{Stdout: "something else"})
	if res := b.run("boundary.cli", map[string]any{"command": []string{"scopes", "list"}, "contains": "global"}); res.Status != primitive.Fail || !strings.Contains(res.Message, "does not contain") {
		t.Errorf("missing text: %+v", res)
	}
}

func TestCliPassesExtraEnvironmentVariablesSoAPasswordNeverTouchesTheCommandLine(t *testing.T) {
	b := newPrimbed(t)
	b.reply("accounts create", runtime.ExecResult{Stdout: `{"item":{"id":"acctpw_1"}}`})
	res := b.run("boundary.cli", map[string]any{
		"command": []string{"accounts", "create", "password", "-login-name=jsmith", "-password=env://NEW_PASSWORD"},
		"env":     map[string]any{"NEW_PASSWORD": "a-new-password-1"},
	})
	if res.Status != primitive.Pass {
		t.Fatalf("result = %+v", res)
	}
	calls := b.sim.callsOn("node-1", binaryPath, "accounts", "create")
	if len(calls) != 1 || calls[0].env["NEW_PASSWORD"] != "a-new-password-1" || calls[0].env["BOUNDARY_ADDR"] == "" {
		t.Errorf("the command's environment = %v", calls[0].env)
	}
	for _, a := range calls[0].argv {
		if strings.Contains(a, "a-new-password-1") {
			t.Errorf("the password is on the command line: %q", a)
		}
	}
}

func TestSigningInAsAnotherUserNeedsTheirPasswordAndUsesIt(t *testing.T) {
	b := newPrimbed(t)
	b.sim.mu.Lock()
	b.sim.users = map[string]string{"jsmith": "jsmith-pw"}
	b.sim.mu.Unlock()
	b.reply("scopes list", runtime.ExecResult{Stdout: "{}"})

	if err := b.runErr("boundary.cli", map[string]any{"command": []string{"scopes", "list"}, "as": "jsmith"}); err == nil || !strings.Contains(err.Error(), "needs a password") {
		t.Errorf("without a password: %v", err)
	}
	if err := b.runErr("boundary.cli", map[string]any{"command": []string{"scopes", "list"}, "as": "jsmith", "password": "wrong"}); err == nil || !strings.Contains(err.Error(), "sign in as jsmith failed") {
		t.Errorf("with the wrong password: %v", err)
	}
	if res := b.run("boundary.cli", map[string]any{"command": []string{"scopes", "list"}, "as": "jsmith", "password": "jsmith-pw"}); res.Status != primitive.Pass {
		t.Fatalf("with the right one: %+v", res)
	}
	calls := b.sim.callsOn("node-1", binaryPath, "scopes", "list")
	if calls[len(calls)-1].env["BOUNDARY_TOKEN"] != "at_jsmith" {
		t.Error("the command did not run as jsmith")
	}
}

func TestAuthorizeSessionReportsAllowedAndDeniedAsTheScenarioExpects(t *testing.T) {
	b := newPrimbed(t)
	b.reply("targets list", runtime.ExecResult{Stdout: `{"items":[{"id":"ttcp_sim","name":"Generated target using host sources"},{"id":"ttcp_other","name":"Other"}]}`})

	b.reply("targets authorize-session", runtime.ExecResult{Stdout: `{"status_code":200}`})
	if res := b.run("boundary.authorize-session", map[string]any{}); res.Status != primitive.Pass || !strings.Contains(res.Message, "ttcp_sim") {
		t.Errorf("allowed: %+v", res)
	}
	if res := b.run("boundary.authorize-session", map[string]any{"expect": "denied"}); res.Status != primitive.Fail || !strings.Contains(res.Message, "should have been denied") {
		t.Errorf("allowed when denial was expected: %+v", res)
	}

	b.reply("targets authorize-session", runtime.ExecResult{ExitCode: 1, Stderr: "Error from controller: Status: 403 PermissionDenied"})
	if res := b.run("boundary.authorize-session", map[string]any{"expect": "denied"}); res.Status != primitive.Pass {
		t.Errorf("denied as expected: %+v", res)
	}
	if res := b.run("boundary.authorize-session", map[string]any{}); res.Status != primitive.Fail || !strings.Contains(res.Message, "403") {
		t.Errorf("denied when allowed was expected: %+v", res)
	}

	// A failure that is not about permission is not an answer, so it never passes as "denied".
	b.reply("targets authorize-session", runtime.ExecResult{ExitCode: 1, Stderr: "Status: 500 No workers are available"})
	if res := b.run("boundary.authorize-session", map[string]any{"expect": "denied"}); res.Status != primitive.Fail || !strings.Contains(res.Message, "not permission") {
		t.Errorf("a 500 counted as denied: %+v", res)
	}

	if err := b.runErr("boundary.authorize-session", map[string]any{"expect": "maybe"}); err == nil {
		t.Error("an unknown expectation was accepted")
	}
}

func TestATargetIsFoundByIDOrByName(t *testing.T) {
	b := newPrimbed(t)
	b.reply("targets list", runtime.ExecResult{Stdout: `{"items":[{"id":"ttcp_a","name":"Alpha"}]}`})
	b.reply("targets authorize-session", runtime.ExecResult{Stdout: `{}`})

	if res := b.run("boundary.authorize-session", map[string]any{"target": "Alpha"}); !strings.Contains(res.Message, "ttcp_a") {
		t.Errorf("by name: %+v", res)
	}
	if res := b.run("boundary.authorize-session", map[string]any{"target": "ttcp_direct"}); !strings.Contains(res.Message, "ttcp_direct") {
		t.Errorf("by ID: %+v", res)
	}
	if err := b.runErr("boundary.authorize-session", map[string]any{"target": "Nonexistent"}); err == nil || !strings.Contains(err.Error(), `no target called "Nonexistent"`) {
		t.Errorf("an unknown name: %v", err)
	}
}

func TestTargetInfoReadsASettingAndComparesItAsText(t *testing.T) {
	b := newPrimbed(t)
	b.reply("targets list", runtime.ExecResult{Stdout: `{"items":[{"id":"ttcp_sim","name":"Generated target using host sources"}]}`})
	b.reply("targets read", runtime.ExecResult{Stdout: `{"item":{"id":"ttcp_sim","session_max_seconds":28800,"session_connection_limit":-1,"name":"T"}}`})

	res := b.run("boundary.target-info", map[string]any{"field": "session_max_seconds", "equals": "28800", "save": "max"})
	if res.Status != primitive.Pass || res.Outputs["max"] != "28800" {
		t.Errorf("whole number: %+v", res)
	}
	if res := b.run("boundary.target-info", map[string]any{"field": "session_connection_limit", "equals": "-1"}); res.Status != primitive.Pass {
		t.Errorf("minus one: %+v", res)
	}
	if res := b.run("boundary.target-info", map[string]any{"field": "session_max_seconds", "equals": "60"}); res.Status != primitive.Fail || !strings.Contains(res.Message, "is 28800, expected 60") {
		t.Errorf("a different value: %+v", res)
	}
	if res := b.run("boundary.target-info", map[string]any{"field": "no_such_field"}); res.Status != primitive.Fail || !strings.Contains(res.Message, "no field") {
		t.Errorf("an unknown field: %+v", res)
	}
	if res := b.run("boundary.target-info", map[string]any{"save": "all"}); res.Status != primitive.Pass || !strings.Contains(res.Outputs["all"], "session_max_seconds") {
		t.Errorf("the whole target: %+v", res)
	}
	if err := b.runErr("boundary.target-info", map[string]any{"equals": "1"}); err == nil || !strings.Contains(err.Error(), "needs a field") {
		t.Errorf("equals with no field: %v", err)
	}
}

func TestSessionInfoCountsTheSessionsThatMatch(t *testing.T) {
	b := newPrimbed(t)
	b.reply("targets list", runtime.ExecResult{Stdout: `{"items":[{"id":"ttcp_sim","name":"Generated target using host sources"}]}`})
	b.reply("sessions list", runtime.ExecResult{Stdout: `{"items":[
		{"id":"s_1","target_id":"ttcp_sim","status":"terminated","termination_reason":"connection limit reached"},
		{"id":"s_2","target_id":"ttcp_sim","status":"terminated","termination_reason":"timed out"},
		{"id":"s_3","target_id":"ttcp_sim","status":"active"},
		{"id":"s_4","target_id":"ttcp_other","status":"terminated","termination_reason":"timed out"}]}`})

	if res := b.run("boundary.session-info", map[string]any{"status": "terminated", "atLeast": 2}); res.Status != primitive.Pass || !strings.Contains(res.Message, "2 session(s)") {
		t.Errorf("by status: %+v", res)
	}
	if res := b.run("boundary.session-info", map[string]any{"reason": "Connection limit reached"}); res.Status != primitive.Pass {
		t.Errorf("by reason, ignoring case: %+v", res)
	}
	if res := b.run("boundary.session-info", map[string]any{"status": "terminated", "atLeast": 3}); res.Status != primitive.Fail || !strings.Contains(res.Message, "expected at least 3") {
		t.Errorf("too few: %+v", res)
	}
	if res := b.run("boundary.session-info", map[string]any{"status": "pending"}); res.Status != primitive.Fail {
		t.Errorf("none match: %+v", res)
	}
}

func TestRecordingsListCountsRecordingsAndTheirState(t *testing.T) {
	b := newPrimbed(t)
	b.reply("session-recordings list", runtime.ExecResult{Stdout: `{"items":[{"state":"available"},{"state":"started"}]}`})
	if res := b.run("boundary.recordings-list", map[string]any{"atLeast": 2}); res.Status != primitive.Pass {
		t.Errorf("two recordings: %+v", res)
	}
	if res := b.run("boundary.recordings-list", map[string]any{"state": "available", "atLeast": 2}); res.Status != primitive.Fail {
		t.Errorf("only one is available: %+v", res)
	}
	b.reply("session-recordings list", runtime.ExecResult{ExitCode: 1, Stderr: "this is not an Enterprise build"})
	if res := b.run("boundary.recordings-list", map[string]any{}); res.Status != primitive.Fail || !strings.Contains(res.Message, "not an Enterprise build") {
		t.Errorf("a build with no recordings: %+v", res)
	}
}

// What is shown to the instructor is redacted. A value saved for a later step is not, because the step has to be able to use it.
func TestNoPasswordOrTokenReachesWhatTheInstructorIsShown(t *testing.T) {
	b := newPrimbed(t)
	b.reply("scopes list", runtime.ExecResult{Stdout: "the admin password is " + simPassword + " and the token " + simToken})
	res := b.run("boundary.cli", map[string]any{"command": []string{"scopes", "list"}, "contains": "nonsense"})
	if res.Status != primitive.Fail {
		t.Fatalf("result = %+v", res)
	}
	if strings.Contains(res.Evidence, simPassword) || strings.Contains(res.Evidence, simToken) || strings.Contains(res.Message, simPassword) {
		t.Errorf("a secret is in what was shown: %+v", res)
	}
	if !strings.Contains(res.Evidence, "the admin password is") {
		t.Error("the rest of the output was lost")
	}
}
