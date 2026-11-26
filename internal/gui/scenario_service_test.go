package gui

import (
	"context"
	"errors"

	"github.com/pixambi/sev1situations/internal/scenario"
	"reflect"
	"strings"
	"testing"
)

func TestScenarioListIsOrderedAndDescribesEachScenario(t *testing.T) {
	b := newBackend(t, map[string]string{
		"b": scenarioYAML("zeta", "Zeta", "vault", true),
		"a": scenarioYAML("alpha", "Alpha", "vault", false),
		"c": scenarioYAML("gate", "Gate", "boundary", true),
	})
	list, err := NewScenarioService(ScenarioDeps{Catalogue: b.catalogue}).List()
	if err != nil {
		t.Fatal(err)
	}

	var ids []string
	for _, s := range list.Scenarios {
		ids = append(ids, s.ID)
	}
	if want := []string{"gate", "alpha", "zeta"}; !reflect.DeepEqual(ids, want) { // by product, then name
		t.Errorf("order = %v, want %v", ids, want)
	}
	alpha := list.Scenarios[1]
	if alpha.Name != "Alpha" || alpha.Product != "vault" || alpha.Difficulty != "beginner" ||
		alpha.EstimatedMinutes != 10 || alpha.Source != "built-in" || !reflect.DeepEqual(alpha.Editions, []string{"community"}) {
		t.Errorf("summary = %+v", alpha)
	}
	if len(list.Problems) != 0 {
		t.Errorf("problems = %v, want none", list.Problems)
	}
}

func TestScenarioListKeepsGoodScenariosAndReportsTheBrokenOne(t *testing.T) {
	b := newBackend(t, map[string]string{
		"good": scenarioYAML("good", "Good", "vault", false),
		"bad":  "id: bad\nname: [not a string\n",
	})
	list, err := NewScenarioService(ScenarioDeps{Catalogue: b.catalogue}).List()
	if err != nil {
		t.Fatalf("one broken scenario must not hide the rest: %v", err)
	}
	if len(list.Scenarios) != 1 || list.Scenarios[0].ID != "good" {
		t.Errorf("scenarios = %+v", list.Scenarios)
	}
	if len(list.Problems) == 0 || !strings.Contains(strings.Join(list.Problems, "\n"), "bad") {
		t.Errorf("problems = %v, want one that names the broken scenario", list.Problems)
	}
}

func TestAnEmptyCatalogueIsAnEmptyListNotNull(t *testing.T) {
	b := newBackend(t, nil)
	list, err := NewScenarioService(ScenarioDeps{Catalogue: b.catalogue}).List()
	if err != nil {
		t.Fatal(err)
	}
	if list.Scenarios == nil || list.Problems == nil {
		t.Errorf("nil slices become null in the frontend: %+v", list)
	}
}

func TestScenarioGetReturnsTheStartForm(t *testing.T) {
	b := newBackend(t, map[string]string{"a": scenarioYAML("alpha", "Alpha", "vault", true)})
	d, err := NewScenarioService(ScenarioDeps{Catalogue: b.catalogue}).Get("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.Objectives, []string{"Learn it"}) || !reflect.DeepEqual(d.Steps, []string{"Break it", "Fix it"}) {
		t.Errorf("detail = %+v", d)
	}
	if len(d.Parameters) != 1 {
		t.Fatalf("parameters = %+v", d.Parameters)
	}
	if p := d.Parameters[0]; p.ID != "nodes" || p.Type != "int" || p.Min != 1 || p.Max != 5 || p.Default != 2 {
		t.Errorf("parameter = %+v", p)
	}
}

func TestScenarioGetRefusesWhatItCannotFind(t *testing.T) {
	b := newBackend(t, map[string]string{"a": scenarioYAML("alpha", "Alpha", "vault", false)})
	svc := NewScenarioService(ScenarioDeps{Catalogue: b.catalogue})
	for _, id := range []string{"", "  ", "missing"} {
		if _, err := svc.Get(id); err == nil {
			t.Errorf("Get(%q) succeeded", id)
		}
	}
}

func TestAnEnterpriseOnlyScenarioNeedsALicenseUntilOneIsSaved(t *testing.T) {
	enterprise := strings.Replace(scenarioYAML("ent", "Enterprise only", "vault", false), "editions: [community]", "editions: [enterprise]", 1)
	both := strings.Replace(scenarioYAML("both", "Either", "vault", false), "editions: [community]", "editions: [community, enterprise]", 1)
	b := newBackend(t, map[string]string{
		"e": enterprise, "b": both, "c": scenarioYAML("comm", "Community", "vault", false),
	})

	needs := func(licensed LicenseCheck) map[string]bool {
		list, err := NewScenarioService(ScenarioDeps{Catalogue: b.catalogue, Licensed: licensed}).List()
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, s := range list.Scenarios {
			out[s.ID] = s.NeedsLicense
		}
		return out
	}

	none := needs(func(string) bool { return false })
	if !none["ent"] || none["both"] || none["comm"] {
		t.Errorf("with no license: %v", none)
	}
	if got := needs(func(p string) bool { return p == "vault" }); got["ent"] {
		t.Errorf("with a Vault license the scenario still needs one: %v", got)
	}
	if got := needs(nil); got["ent"] {
		t.Errorf("with no check, every product counts as licensed: %v", got)
	}
}

func TestGetCarriesWhatTheStartFormNeedsBeyondTheScenario(t *testing.T) {
	yaml := strings.Replace(scenarioYAML("alpha", "Alpha", "vault", false), "editions: [community]", "editions: [community, enterprise]", 1)
	yaml = strings.Replace(yaml, "parameters:\n", "parameters:\n  - id: edition\n    type: edition\n    label: Edition\n    default: community\n", 1)
	b := newBackend(t, map[string]string{"a": yaml})

	d, err := NewScenarioService(ScenarioDeps{
		Catalogue: b.catalogue, Licensed: func(string) bool { return false }, Instructor: func() string { return "Ada" },
	}).Get("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if d.Instructor != "Ada" || d.Licensed {
		t.Errorf("instructor = %q, licensed = %v", d.Instructor, d.Licensed)
	}
	if got := d.Parameters[0]; got.Type != "edition" || !reflect.DeepEqual(got.Options, []string{"community", "enterprise"}) {
		t.Errorf("edition parameter = %+v", got)
	}
}

func TestVersionsAreListedForAnEditionAndFailuresAreExplained(t *testing.T) {
	var asked [][2]string
	svc := NewScenarioService(ScenarioDeps{
		Catalogue: func() (*scenario.Registry, error) { return nil, nil },
		Versions: func(_ context.Context, product, edition string) ([]string, error) {
			asked = append(asked, [2]string{product, edition})
			if product == "offline" {
				return nil, errors.New("dial tcp: no route to host")
			}
			return []string{"2.2.2", "2.2.1"}, nil
		},
	})

	got, err := svc.Versions("vault", "enterprise")
	if err != nil || !reflect.DeepEqual(got, []string{"2.2.2", "2.2.1"}) || asked[0] != [2]string{"vault", "enterprise"} {
		t.Fatalf("got %v, %v, asked %v", got, err, asked)
	}

	for _, c := range [][2]string{{"vault", "fips"}, {"", "community"}, {"  ", "community"}} {
		if _, err := svc.Versions(c[0], c[1]); err == nil {
			t.Errorf("Versions(%q, %q) succeeded", c[0], c[1])
		}
	}
	if len(asked) != 1 {
		t.Errorf("a refused request still reached the lister: %v", asked)
	}
	if _, err := svc.Versions("offline", "community"); err == nil || !strings.Contains(err.Error(), "online") {
		t.Errorf("offline error = %v", err)
	}
	if _, err := NewScenarioService(ScenarioDeps{Catalogue: svc.Catalogue}).Versions("vault", "community"); err == nil {
		t.Error("a build with no lister listed versions")
	}
}
