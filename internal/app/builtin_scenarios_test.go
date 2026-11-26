package app

import (
	"strings"
	"testing"

	sev1situations "github.com/pixambi/sev1situations"
)

// Every scenario shipped in the executable must meet the Scenario Standard (5.4) and carry the written
// guidance an instructor needs (R-SCN-7). This runs over whatever is in the scenarios folder, so a new
// scenario is held to it without anyone editing this file (R-SCN-16).
func TestEveryBuiltInScenarioMeetsTheStandard(t *testing.T) {
	products, err := DefaultProducts()
	if err != nil {
		t.Fatal(err)
	}
	reg, err := LoadCatalogue(Options{Builtin: sev1situations.Scenarios, Products: products, UserDir: t.TempDir()})
	if err != nil {
		t.Fatalf("a built-in scenario does not load: %v", err)
	}
	entries := reg.List()
	if len(entries) < 2 {
		t.Fatalf("only %d built-in scenarios found, which looks like the embedded folder is wrong", len(entries))
	}

	seen := map[string]bool{}
	for _, e := range entries {
		sc := e.Scenario
		t.Run(sc.ID, func(t *testing.T) {
			if seen[sc.ID] {
				t.Errorf("the ID %q is used twice", sc.ID)
			}
			seen[sc.ID] = true

			if !strings.HasPrefix(sc.ID, sc.Product+"-") {
				t.Errorf("the ID %q should start with its product, %q", sc.ID, sc.Product+"-")
			}
			if sc.EstimatedMinutes <= 0 || len(sc.Objectives) == 0 || len(sc.Prerequisites) == 0 || len(sc.Tags) == 0 {
				t.Errorf("it lacks minutes, objectives, prerequisites or tags: %d %d %d %d", sc.EstimatedMinutes, len(sc.Objectives), len(sc.Prerequisites), len(sc.Tags))
			}
			if len(sc.References) == 0 {
				t.Error("it links to no documentation")
			}
			for _, r := range sc.References {
				if !strings.HasPrefix(r.URL, "https://") || r.Title == "" {
					t.Errorf("reference %+v needs a title and an https address", r)
				}
			}
			if len(sc.Steps) == 0 {
				return // an open sandbox has no incident to describe
			}
			if strings.TrimSpace(sc.FailureCreated) == "" || strings.TrimSpace(sc.SuccessCriteria) == "" || len(sc.Pitfalls) == 0 {
				t.Error("a scenario with steps must say what failure it creates, what success is, and the common pitfalls")
			}

			hasBreakage := false
			for _, st := range sc.Steps {
				hasBreakage = hasBreakage || st.Kind == "breakage"
				text, err := sc.ReadFile(st.Guide)
				if err != nil {
					t.Errorf("step %s: its guide cannot be read: %v", st.ID, err)
					continue
				}
				g := string(text)
				if !strings.Contains(g, "## Briefing") {
					t.Errorf("step %s: the guide has no Briefing section, so the instructor does not know what to say", st.ID)
				}
				if len(st.Verify) > 0 && st.Kind == "verification" {
					for _, want := range []string{"## Hints", "## Solution"} {
						if !strings.Contains(g, want) {
							t.Errorf("step %s: the guide has no %q section", st.ID, strings.TrimPrefix(want, "## "))
						}
					}
				}
				if len(st.Verify) == 0 {
					t.Errorf("step %s has nothing to verify, so the instructor could never continue past it", st.ID)
				}
				if st.Kind == "breakage" && len(st.Action) == 0 {
					t.Errorf("step %s is a breakage with no action", st.ID)
				}
			}
			if !hasBreakage {
				t.Error("a scenario with steps needs a breakage step to create its incident")
			}
		})
	}
}
