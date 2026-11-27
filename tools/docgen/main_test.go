package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

const sample = `id: demo-one
name: Demo One
summary: A demo.
product: vault
editions: [community]
difficulty: beginner
estimatedMinutes: 5
tags: [demo]
objectives: [Learn a thing.]
prerequisites: [None.]
parameters:
  - id: version
    type: version
    product: vault
    label: Vault version
    help: The version | to run.
topology:
  clusters:
    - name: primary
      product: vault
      nodes: 1
initialState: >-
  Wrapped
  over lines.
failureCreated: Something breaks.
successCriteria: It works.
pitfalls: [Forgetting.]
references:
  - title: Docs
    url: https://example.com
steps:
  - id: one
    kind: breakage
    title: Break it
    guide: steps/one.md
    resetPoint: true
    action: []
    verify: []
`

func fixture() fstest.MapFS {
	return fstest.MapFS{
		"vault/demo/scenario.yaml": {Data: []byte(sample)},
		"vault/demo/steps/one.md":  {Data: []byte("## Briefing\n\nSay it.\n\n```sh\n## not a heading\n```\n\n## Hints\n\n- one\n")},
	}
}

func TestAScenarioBecomesAPageWithEverySection(t *testing.T) {
	pages, err := generate(fixture())
	if err != nil {
		t.Fatal(err)
	}
	page := string(pages["vault/demo-one.md"])
	for _, want := range []string{
		`title: "Demo One"`, `scenario_id: "demo-one"`, "## Learning objectives", "- Learn a thing.", "| Vault version | version |",
		`The version \| to run.`, "Wrapped over lines.", "### 1. Break it", "You can roll back to the start of this step.",
		"#### Briefing", "#### Hints", "## Success criteria", "## Pitfalls", "[Docs](https://example.com)",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %q:\n%s", want, page)
		}
	}
	if !strings.Contains(page, "```sh\n## not a heading\n```") {
		t.Error("a comment inside a code block was treated as a heading")
	}
	for _, rel := range []string{"_index.md", "vault/_index.md"} {
		if _, ok := pages[rel]; !ok {
			t.Errorf("no %s", rel)
		}
	}
}

func TestASandboxWithNoStepsSaysSo(t *testing.T) {
	fsys := fixture()
	fsys["vault/demo/scenario.yaml"].Data = []byte(strings.Split(sample, "steps:")[0])
	pages, err := generate(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pages["vault/demo-one.md"]), "open-ended sandbox") {
		t.Error("a sandbox is not described as one")
	}
}

func TestCheckFindsMissingChangedAndExtraPages(t *testing.T) {
	pages, _ := generate(fixture())
	dir := t.TempDir()
	if err := write(dir, pages); err != nil {
		t.Fatal(err)
	}
	if stale, err := compare(dir, pages); err != nil || len(stale) != 0 {
		t.Fatalf("freshly written pages are stale: %v %v", stale, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vault", "demo-one.md"), []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "vault", "_index.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "old.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale, _ := compare(dir, pages)
	got := strings.Join(stale, "|")
	for _, want := range []string{"vault/demo-one.md differs", "vault/_index.md is missing", "old.md should not exist"} {
		if !strings.Contains(got, want) {
			t.Errorf("stale = %v, missing %q", stale, want)
		}
	}
}

func TestAMissingGuideIsAnError(t *testing.T) {
	fsys := fixture()
	delete(fsys, "vault/demo/steps/one.md")
	if _, err := generate(fsys); err == nil {
		t.Error("a scenario with no guide was rendered")
	}
}
