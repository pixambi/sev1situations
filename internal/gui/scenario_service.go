package gui

import (
	"fmt"
	"strings"

	"github.com/pixambi/sev1situations/internal/scenario"
)

// Catalogue loads the scenarios. It is called for every request, so a scenario the
// user adds to their folder appears without restarting. The error lists the
// scenarios that did not load, and the others are still returned.
type Catalogue func() (*scenario.Registry, error)

// ScenarioService lists the scenarios and describes one (3.4).
type ScenarioService struct {
	catalogue Catalogue
}

// NewScenarioService returns a service over the catalogue.
func NewScenarioService(c Catalogue) *ScenarioService { return &ScenarioService{catalogue: c} }

// ScenarioSummary is one line of the catalogue.
type ScenarioSummary struct {
	ID               string
	Name             string
	Summary          string
	Product          string
	Editions         []string
	Difficulty       string
	EstimatedMinutes int
	Tags             []string
	// Source is "built-in" or "user".
	Source string
}

// ScenarioList is the catalogue, plus a description of each scenario that could
// not be loaded, for the Scenarios page to show.
type ScenarioList struct {
	Scenarios []ScenarioSummary
	Problems  []string
}

// ParameterView is one field of the Start form.
type ParameterView struct {
	ID, Type, Label, Help string
	// Default is the initial value, if the scenario gives one.
	Default any
	// Product feeds a version field's choices.
	Product string
	// Min and Max bound a whole-number field. They are zero when unbounded.
	Min, Max int
}

// ScenarioDetail is everything the Start form shows about a scenario.
type ScenarioDetail struct {
	ScenarioSummary
	Objectives    []string
	Prerequisites []string
	Parameters    []ParameterView
	// Steps are the titles of the steps after the implicit Start step. A scenario
	// with none is an open-ended sandbox.
	Steps []string
}

// List returns the catalogue, ordered by product and name.
func (s *ScenarioService) List() (ScenarioList, error) {
	reg, err := s.catalogue()
	if reg == nil {
		return ScenarioList{}, fmt.Errorf("load the scenarios: %w", err)
	}
	out := ScenarioList{Scenarios: []ScenarioSummary{}, Problems: problemLines(err)}
	for _, e := range reg.List() {
		out.Scenarios = append(out.Scenarios, summarise(e))
	}
	return out, nil
}

// Get returns one scenario's detail.
func (s *ScenarioService) Get(id string) (ScenarioDetail, error) {
	if strings.TrimSpace(id) == "" {
		return ScenarioDetail{}, fmt.Errorf("choose a scenario")
	}
	reg, err := s.catalogue()
	if reg == nil {
		return ScenarioDetail{}, fmt.Errorf("load the scenarios: %w", err)
	}
	e, ok := reg.Get(id)
	if !ok {
		return ScenarioDetail{}, fmt.Errorf("there is no scenario %q", id)
	}
	sc := e.Scenario
	d := ScenarioDetail{
		ScenarioSummary: summarise(e),
		Objectives:      nonNil(sc.Objectives),
		Prerequisites:   nonNil(sc.Prerequisites),
		Parameters:      []ParameterView{},
		Steps:           []string{},
	}
	for _, p := range sc.Parameters {
		v := ParameterView{ID: p.ID, Type: p.Type, Label: p.Label, Help: p.Help, Default: p.Default, Product: p.Product}
		if p.Min != nil {
			v.Min = *p.Min
		}
		if p.Max != nil {
			v.Max = *p.Max
		}
		d.Parameters = append(d.Parameters, v)
	}
	for _, st := range sc.Steps {
		d.Steps = append(d.Steps, st.Title)
	}
	return d, nil
}

func summarise(e scenario.Entry) ScenarioSummary {
	sc := e.Scenario
	return ScenarioSummary{
		ID: sc.ID, Name: sc.Name, Summary: sc.Summary, Product: sc.Product,
		Editions: nonNil(sc.Editions), Difficulty: sc.Difficulty, EstimatedMinutes: sc.EstimatedMinutes,
		Tags: nonNil(sc.Tags), Source: string(e.Source),
	}
}

// nonNil keeps an empty list as [] in the frontend, not null.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// problemLines splits a load error into one line for each scenario that failed.
func problemLines(err error) []string {
	if err == nil {
		return []string{}
	}
	var lines []string
	for _, l := range strings.Split(err.Error(), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}
