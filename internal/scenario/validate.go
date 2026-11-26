package scenario

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"sort"
	"strings"
	"text/template"
)

// PrimitiveSet says which primitives exist. The primitive registry satisfies it.
type PrimitiveSet interface {
	// Has reports whether a primitive with this dotted name is registered.
	Has(name string) bool
}

// Options configures Validate. A nil field skips the checks that need it, which
// lets the rules be tested one at a time.
type Options struct {
	// Primitives is checked for every primitive a step names.
	Primitives PrimitiveSet
	// Products lists the product names a scenario may use.
	Products []string
}

var (
	kebab      = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	identifier = regexp.MustCompile(`^[a-z][A-Za-z0-9_]*$`)
	difficulty = []string{Beginner, Intermediate, Advanced}
	editions   = []string{"community", "enterprise"}
	paramTypes = []string{ParamVersion, ParamEdition, ParamInt}
	stepKinds  = []string{KindBreakage, KindVerification}
)

const maxMinutes = 480

// Validate checks the scenario against the Scenario Standard (5.4, R-SCN-13) and
// the rules of the model. It returns a *LoadError listing every problem, each
// with the file, line and field, or nil if the scenario is valid. A scenario
// without steps is an open-ended sandbox (R-SCN-17) and is not required to
// describe a failure.
func (s *Scenario) Validate(opts Options) error {
	v := &validator{s: s, opts: opts, file: s.Dir + "/" + FileName}
	v.metadata()
	v.parameters()
	v.topology()
	v.steps()

	if len(v.problems) == 0 {
		return nil
	}
	sort.SliceStable(v.problems, func(i, j int) bool { return v.problems[i].Line < v.problems[j].Line })
	return &LoadError{Problems: v.problems}
}

type validator struct {
	s        *Scenario
	opts     Options
	file     string
	problems []Problem
}

// add records a problem against a field. When the field has no line, as when a
// required field is missing, the nearest parent that has one is used.
func (v *validator) add(field, format string, args ...any) {
	line := 0
	for p := field; p != ""; p = parent(p) {
		if line = v.s.Line(p); line != 0 {
			break
		}
	}
	v.problems = append(v.problems, Problem{File: v.file, Line: line, Field: field, Message: fmt.Sprintf(format, args...)})
}

func parent(path string) string {
	if i := strings.LastIndexAny(path, ".["); i >= 0 {
		return path[:i]
	}
	return ""
}

func (v *validator) required(field, value string) {
	if strings.TrimSpace(value) == "" {
		v.add(field, "is required")
	}
}

func (v *validator) requiredList(field string, n int) {
	if n == 0 {
		v.add(field, "needs at least one entry")
	}
}

func (v *validator) metadata() {
	s := v.s
	sandbox := len(s.Steps) == 0

	v.required("id", s.ID)
	if s.ID != "" && !kebab.MatchString(s.ID) {
		v.add("id", "%q must be kebab-case: lower-case letters and digits separated by single dashes", s.ID)
	}
	v.required("name", s.Name)
	v.required("summary", s.Summary)
	v.required("product", s.Product)
	v.knownProduct("product", s.Product)

	v.requiredList("editions", len(s.Editions))
	seen := map[string]bool{}
	for i, e := range s.Editions {
		f := fmt.Sprintf("editions[%d]", i)
		if !slices.Contains(editions, e) {
			v.add(f, "%q is not an edition. Use one of: %s", e, strings.Join(editions, ", "))
		}
		if seen[e] {
			v.add(f, "%q is listed twice", e)
		}
		seen[e] = true
	}

	if !slices.Contains(difficulty, s.Difficulty) {
		v.add("difficulty", "%q is not a difficulty. Use one of: %s", s.Difficulty, strings.Join(difficulty, ", "))
	}
	if s.EstimatedMinutes < 1 || s.EstimatedMinutes > maxMinutes {
		v.add("estimatedMinutes", "%d must be between 1 and %d", s.EstimatedMinutes, maxMinutes)
	}

	v.requiredList("tags", len(s.Tags))
	v.noBlanks("tags", s.Tags)
	v.requiredList("objectives", len(s.Objectives))
	v.noBlanks("objectives", s.Objectives)
	v.requiredList("prerequisites", len(s.Prerequisites))
	v.noBlanks("prerequisites", s.Prerequisites)
	v.required("initialState", s.InitialState)

	// A sandbox has nothing broken and nothing to fix.
	if !sandbox {
		v.required("failureCreated", s.FailureCreated)
		v.required("successCriteria", s.SuccessCriteria)
		v.requiredList("pitfalls", len(s.Pitfalls))
		v.noBlanks("pitfalls", s.Pitfalls)
	}

	v.requiredList("references", len(s.References))
	for i, r := range s.References {
		f := fmt.Sprintf("references[%d]", i)
		v.required(f+".title", r.Title)
		if !strings.HasPrefix(r.URL, "https://") {
			v.add(f+".url", "%q must be an https:// link", r.URL)
		}
	}
}

func (v *validator) noBlanks(field string, items []string) {
	for i, it := range items {
		if strings.TrimSpace(it) == "" {
			v.add(fmt.Sprintf("%s[%d]", field, i), "must not be blank")
		}
	}
}

func (v *validator) knownProduct(field, product string) {
	if product == "" || v.opts.Products == nil {
		return
	}
	if !slices.Contains(v.opts.Products, product) {
		v.add(field, "%q is not a known product. Use one of: %s", product, strings.Join(v.opts.Products, ", "))
	}
}

func (v *validator) parameters() {
	v.requiredList("parameters", len(v.s.Parameters))
	ids := map[string]bool{}
	for i, p := range v.s.Parameters {
		f := fmt.Sprintf("parameters[%d]", i)

		v.required(f+".id", p.ID)
		switch {
		case p.ID != "" && !identifier.MatchString(p.ID):
			v.add(f+".id", "%q must start with a lower-case letter and use only letters, digits and underscores, because templates read it as .Params.%s", p.ID, p.ID)
		case ids[p.ID]:
			v.add(f+".id", "%q is used by another parameter", p.ID)
		}
		ids[p.ID] = true
		v.required(f+".label", p.Label)

		if !slices.Contains(paramTypes, p.Type) {
			v.add(f+".type", "%q is not a parameter type. Use one of: %s", p.Type, strings.Join(paramTypes, ", "))
			continue
		}
		switch p.Type {
		case ParamVersion:
			v.required(f+".product", p.Product)
			v.knownProduct(f+".product", p.Product)
		case ParamEdition:
			if d, ok := p.Default.(string); p.Default != nil && (!ok || !slices.Contains(v.s.Editions, d)) {
				v.add(f+".default", "%v must be one of the scenario's editions: %s", p.Default, strings.Join(v.s.Editions, ", "))
			}
		case ParamInt:
			v.intParameter(f, p)
		}
	}
}

func (v *validator) intParameter(f string, p Parameter) {
	if p.Min == nil || p.Max == nil {
		v.add(f, "an int parameter needs both min and max")
		return
	}
	if *p.Min > *p.Max {
		v.add(f+".min", "%d is greater than max (%d)", *p.Min, *p.Max)
		return
	}
	if p.Default == nil {
		return
	}
	d, ok := p.Default.(int)
	if !ok {
		v.add(f+".default", "%v must be a whole number", p.Default)
		return
	}
	if d < *p.Min || d > *p.Max {
		v.add(f+".default", "%d is outside min %d and max %d", d, *p.Min, *p.Max)
	}
}

func (v *validator) topology() {
	t := v.s.Topology
	if len(t.Clusters) == 0 {
		v.add("topology.clusters", "needs at least one cluster, because every scenario deploys something")
	}

	names := map[string]bool{}
	name := func(field, n string) {
		v.required(field, n)
		switch {
		case n != "" && !kebab.MatchString(n):
			v.add(field, "%q must be kebab-case", n)
		case names[n]:
			v.add(field, "%q is used by another cluster or service, and credentials are keyed by name", n)
		}
		names[n] = true
	}

	for i, c := range t.Clusters {
		f := fmt.Sprintf("topology.clusters[%d]", i)
		name(f+".name", c.Name)
		v.required(f+".product", c.Product)
		v.knownProduct(f+".product", c.Product)
		if c.Nodes < 1 {
			v.add(f+".nodes", "%d must be at least 1", c.Nodes)
		}
	}
	for i, sv := range t.Services {
		f := fmt.Sprintf("topology.services[%d]", i)
		name(f+".name", sv.Name)
		v.required(f+".type", sv.Type)
	}
}

func (v *validator) steps() {
	ids := map[string]bool{}
	for i, st := range v.s.Steps {
		f := fmt.Sprintf("steps[%d]", i)

		v.required(f+".id", st.ID)
		switch {
		case st.ID != "" && !kebab.MatchString(st.ID):
			v.add(f+".id", "%q must be kebab-case", st.ID)
		case ids[st.ID]:
			v.add(f+".id", "%q is used by another step", st.ID)
		}
		ids[st.ID] = true
		v.required(f+".title", st.Title)
		v.guide(f+".guide", st.Guide)

		switch st.Kind {
		case KindBreakage:
			if len(st.Action) == 0 {
				v.add(f+".action", "a breakage step needs an action that applies the breakage")
			}
			if len(st.Verify) == 0 {
				v.add(f+".verify", "a breakage step needs a verify that confirms the environment is broken (4.2)")
			}
		case KindVerification:
			if len(st.Verify) == 0 {
				v.add(f+".verify", "a verification step needs a verify that checks the fix")
			}
			if len(st.Action) > 0 {
				v.add(f+".action", "a verification step does not apply anything. Use a breakage step to change the environment")
			}
		default:
			v.add(f+".kind", "%q is not a step kind. Use one of: %s", st.Kind, strings.Join(stepKinds, ", "))
		}

		if len(st.Reset) > 0 && !st.ResetPoint {
			v.add(f+".reset", "has no effect unless the step is a resetPoint")
		}

		v.calls(f+".action", st.Action)
		v.calls(f+".verify", st.Verify)
		v.calls(f+".reset", st.Reset)
	}
}

// guide checks that a step's guidance file exists inside the scenario folder.
func (v *validator) guide(field, rel string) {
	if rel == "" {
		v.add(field, "is required: the path of the Markdown guidance for the instructor (R-SCN-7)")
		return
	}
	data, err := v.s.ReadFile(rel)
	switch {
	case errors.Is(err, fs.ErrInvalid):
		v.add(field, "%q must be a path inside the scenario folder", rel)
	case err != nil:
		v.add(field, "%q does not exist in the scenario folder", rel)
	case strings.TrimSpace(string(data)) == "":
		v.add(field, "%q is empty", rel)
	}
}

func (v *validator) calls(field string, calls []Call) {
	for i, c := range calls {
		f := fmt.Sprintf("%s[%d]", field, i)
		if c.Primitive == "" {
			v.add(f+".primitive", "is required")
		} else if v.opts.Primitives != nil && !v.opts.Primitives.Has(c.Primitive) {
			v.add(f+".primitive", "%q is not a known primitive", c.Primitive)
		}
		v.templates(f+".args", c.Args)
	}
}

// templates checks that every string argument that uses {{ }} is valid
// template syntax. Whether the values it reads exist is checked separately.
func (v *validator) templates(field string, value any) {
	switch x := value.(type) {
	case string:
		if strings.Contains(x, "{{") {
			if _, err := template.New("arg").Parse(x); err != nil {
				v.add(field, "has a template error: %s%s", strings.TrimPrefix(err.Error(), "template: arg:1: "), dashHint(x))
			}
		}
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v.templates(field+"."+k, x[k])
		}
	case []any:
		for i, item := range x {
			v.templates(fmt.Sprintf("%s[%d]", field, i), item)
		}
	}
}

var dashedName = regexp.MustCompile(`\.[A-Za-z]+\.[A-Za-z0-9_]*-[A-Za-z0-9-]*`)

// dashHint explains the most likely cause of a template error: a dash in a name.
func dashHint(src string) string {
	if dashedName.MatchString(src) {
		return ". Names with a dash are written with an underscore inside a template, for example .Nodes.node_1.IP"
	}
	return ""
}
