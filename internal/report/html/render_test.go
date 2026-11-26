package html

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/report"
)

var update = flag.Bool("update", false, "rewrite the golden files")

var t0 = time.Date(2026, 11, 26, 9, 15, 30, 0, time.UTC)

func full() *report.Report {
	return &report.Report{
		Version: 1, ID: "20261126-091530-abc123",
		Scenario:   report.Scenario{ID: "vault-lost-quorum", Name: "Lost Quorum", Product: "vault"},
		Instructor: "Ada Lovelace", Trainee: "Grace Hopper",
		Params:    map[string]any{"version": "2.2.2", "edition": "community", "nodes": float64(3)},
		StartedAt: t0, EndedAt: t0.Add(42*time.Minute + 7*time.Second), Result: report.EndedEarly,
		Steps: []report.Step{
			{ID: "break", Title: "Cause the outage", Kind: "breakage", State: "complete", Verified: true,
				StartedAt: t0.Add(time.Minute), EndedAt: t0.Add(9 * time.Minute), DurationMs: 600000, Attempts: 2,
				Notes: "Looked at the logs first.\nThen found the peers file.", Rating: 4,
				Outcome: &report.Outcome{Status: "pass", Message: "the breakage is in place", At: t0.Add(9 * time.Minute)}},
			{ID: "fix", Title: "Recover quorum", Kind: "verification", State: "active", Attempts: 1,
				StartedAt: t0.Add(9 * time.Minute), DurationMs: 1938000,
				Outcome: &report.Outcome{Status: "fail", Message: "no leader elected yet", At: t0.Add(30 * time.Minute)}},
			{ID: "later", Title: "Verify the cluster", Kind: "verification", State: "locked"},
		},
		Events: []report.Event{
			{At: t0.Add(20 * time.Minute), Kind: "rollback", Step: "break", Message: "rolled back to break"},
			{At: t0.Add(42*time.Minute + 7*time.Second), Kind: "run-ended", Message: "the run ended: ended early"},
		},
	}
}

func sandbox() *report.Report {
	return &report.Report{
		Version: 1, ID: "20261126-100000-sb0001",
		Scenario:   report.Scenario{ID: "vault-sandbox", Name: "Vault Sandbox", Product: "vault"},
		Instructor: "Ada", Trainee: "Grace", Params: map[string]any{},
		StartedAt: t0, EndedAt: t0.Add(90 * time.Second), Result: report.Completed,
		Steps: []report.Step{}, Events: []report.Event{},
	}
}

func render(t *testing.T, r *report.Report) string {
	t.Helper()
	out, err := Renderer{}.Render(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestGolden(t *testing.T) {
	for name, r := range map[string]*report.Report{"full": full(), "sandbox": sandbox()} {
		t.Run(name, func(t *testing.T) {
			got := render(t, r)
			path := filepath.Join("testdata", name+".golden.html")
			if *update {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run the tests with -update to write it)", err)
			}
			if got != string(want) {
				t.Errorf("the page changed. If that was intended, run the tests with -update and look at the diff.\n--- got ---\n%s", firstDifference(got, string(want)))
			}
		})
	}
}

// firstDifference shows where two pages part, which is easier to read than two whole pages.
func firstDifference(got, want string) string {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := 0; i < len(g) && i < len(w); i++ {
		if g[i] != w[i] {
			return fmt.Sprintf("line %d\n got:  %s\n want: %s", i+1, g[i], w[i])
		}
	}
	return "the pages differ in length"
}

func TestThePageLoadsNothing(t *testing.T) {
	page := render(t, full())
	for _, forbidden := range []string{"<script", "<link", "<img", "<iframe", "<object", "<embed", "<video", "<audio", "<source", "@import", "url(", " src=", "http://", "https://", "//cdn", "<form", "javascript:"} {
		if strings.Contains(strings.ToLower(page), forbidden) {
			t.Errorf("the page contains %q, which could load something or run", forbidden)
		}
	}
	if !strings.Contains(page, "default-src 'none'") {
		t.Error("the page does not forbid every kind of load itself")
	}
}

func TestMarkupInTheDataIsShownAsTextAndNeverRuns(t *testing.T) {
	r := full()
	r.Trainee = `<img src=x onerror=alert(1)>`
	r.Steps[0].Notes = "<script>alert('x')</script> and a link http://evil.example/a and [md](https://evil.example)"
	r.Steps[0].Title = `Step "one" & <b>two</b>`
	r.Events[0].Message = `</li><script>bad()</script>`
	page := render(t, r)

	for _, bad := range []string{"<script>alert", "<img src=x", "<b>two</b>", "<script>bad", "<a "} {
		if strings.Contains(page, bad) {
			t.Errorf("the page contains live markup %q", bad)
		}
	}
	for _, shown := range []string{"&lt;script&gt;alert", "&lt;img src=x", "&lt;b&gt;two&lt;/b&gt;", "http://evil.example/a"} {
		if !strings.Contains(page, shown) {
			t.Errorf("the page does not show %q as text", shown)
		}
	}
}

func TestEveryFactOfTheReportIsOnThePage(t *testing.T) {
	page := render(t, full())
	for _, want := range []string{
		"Lost Quorum", "Vault scenario", "Ada Lovelace", "Grace Hopper", "Ended early",
		"26 Nov 2026, 09:15:30 UTC", "42m 7s", "2.2.2", "community",
		"Cause the outage", "Complete", "In progress", "This step was not reached.", "2 attempts", "10m",
		"★★★★☆  4 of 5", "Not rated",
		"Passed: the breakage is in place", "Not as expected: no leader elected yet",
		"Looked at the logs first.\nThen found the peers file.",
		"rolled back to break", "the run ended: ended early", "20261126-091530-abc123",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page does not say %q", want)
		}
	}
}

func TestASandboxHasNoStepsAndSaysSo(t *testing.T) {
	page := render(t, sandbox())
	for _, want := range []string{"open sandbox", "Completed", "1m 30s", "Nothing out of the ordinary"} {
		if !strings.Contains(page, want) {
			t.Errorf("the page does not say %q", want)
		}
	}
}

func TestTimesAreShownInTheChosenZoneAndDefaultToUTC(t *testing.T) {
	zone := time.FixedZone("CEST", 2*3600)
	out, err := Renderer{Location: zone}.Render(full())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "26 Nov 2026, 11:15:30 CEST") {
		t.Error("the page ignores the chosen zone")
	}
	if !strings.Contains(render(t, full()), "09:15:30 UTC") {
		t.Error("the default is not UTC")
	}
}

func TestRenderingIsAPureFunction(t *testing.T) {
	r := full()
	a, b := render(t, r), render(t, r)
	if a != b {
		t.Error("the same report rendered two different pages")
	}
	if r.Steps[0].Notes != "Looked at the logs first.\nThen found the peers file." {
		t.Error("rendering changed the report")
	}
}

func TestADamagedReportStillRendersWithoutPanicking(t *testing.T) {
	r := &report.Report{ID: "x", Result: "ended early"} // no steps, no times, no params, nil maps
	page := render(t, r)
	if !strings.Contains(page, "not recorded") {
		t.Error("a report with no end time does not say so")
	}
}

func TestSpanReadsLikeAPersonSaysIt(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0: "0s", 400 * time.Millisecond: "0s", 9 * time.Second: "9s", 60 * time.Second: "1m",
		2*time.Minute + 5*time.Second: "2m 5s", time.Hour: "1h", time.Hour + 5*time.Minute + 30*time.Second: "1h 5m",
		26*time.Hour + 3*time.Minute: "26h 3m", -time.Second: "0s",
	} {
		if got := span(d); got != want {
			t.Errorf("span(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestTheRendererDescribesItself(t *testing.T) {
	var r report.Renderer = Renderer{}
	if r.Extension() != ".html" || !strings.HasPrefix(r.ContentType(), "text/html") {
		t.Errorf("%q %q", r.Extension(), r.ContentType())
	}
}
