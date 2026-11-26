package gui

import (
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
	list, err := NewScenarioService(b.catalogue).List()
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
	list, err := NewScenarioService(b.catalogue).List()
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
	list, err := NewScenarioService(b.catalogue).List()
	if err != nil {
		t.Fatal(err)
	}
	if list.Scenarios == nil || list.Problems == nil {
		t.Errorf("nil slices become null in the frontend: %+v", list)
	}
}

func TestScenarioGetReturnsTheStartForm(t *testing.T) {
	b := newBackend(t, map[string]string{"a": scenarioYAML("alpha", "Alpha", "vault", true)})
	d, err := NewScenarioService(b.catalogue).Get("alpha")
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
	svc := NewScenarioService(b.catalogue)
	for _, id := range []string{"", "  ", "missing"} {
		if _, err := svc.Get(id); err == nil {
			t.Errorf("Get(%q) succeeded", id)
		}
	}
}
