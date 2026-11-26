package primitive

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// TimeoutArg is the argument every primitive accepts.
const TimeoutArg = "timeout"

// maxTimeout caps what a scenario may ask for.
const maxTimeout = 30 * time.Minute

// Args are a primitive's arguments after Prepare: every declared argument is
// present, with the Go type its declaration says.
type Args map[string]any

// Has reports whether an argument has a value, either given or defaulted.
func (a Args) Has(name string) bool { _, ok := a[name]; return ok }

// String returns a String or Duration-as-text argument.
func (a Args) String(name string) string { s, _ := a[name].(string); return s }

// Int returns an Int argument.
func (a Args) Int(name string) int { n, _ := a[name].(int); return n }

// Bool returns a Bool argument.
func (a Args) Bool(name string) bool { b, _ := a[name].(bool); return b }

// List returns a StringList argument.
func (a Args) List(name string) []string { l, _ := a[name].([]string); return l }

// Duration returns a Duration argument.
func (a Args) Duration(name string) time.Duration { d, _ := a[name].(time.Duration); return d }

// Map returns a Map argument.
func (a Args) Map(name string) map[string]any { m, _ := a[name].(map[string]any); return m }

// withTimeout returns the spec's arguments plus the universal timeout.
func (s Spec) withTimeout() []Arg {
	return append(slices.Clone(s.Args), Arg{
		Name: TimeoutArg, Type: Duration, Default: s.Timeout.String(),
		Help: "How long the primitive may run before it is stopped, for example 30s.",
	})
}

// check validates raw arguments against a spec and returns the typed, complete
// result. With templates true, a string containing {{ }} is accepted for any
// argument type, because it is only resolved when the step runs.
func (s Spec) check(raw map[string]any, templates bool) (Args, []string) {
	specs := s.withTimeout()
	known := map[string]Arg{}
	names := make([]string, 0, len(specs))
	for _, a := range specs {
		known[a.Name] = a
		names = append(names, a.Name)
	}
	sort.Strings(names)

	var problems []string
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, ok := known[k]; !ok {
			problems = append(problems, fmt.Sprintf("unknown argument %q. %s takes: %s", k, s.Name, strings.Join(names, ", ")))
		}
	}

	out := Args{}
	for _, a := range specs {
		v, given := raw[a.Name]
		if !given {
			switch {
			case a.Required:
				problems = append(problems, fmt.Sprintf("missing required argument %q (%s)", a.Name, a.Type))
			case a.Default != nil:
				cv, err := convert(a, a.Default, false)
				if err != nil {
					problems = append(problems, fmt.Sprintf("the default of %q is wrong: %v", a.Name, err))
					continue
				}
				out[a.Name] = cv
			}
			continue
		}

		if str, ok := v.(string); ok && templates && strings.Contains(str, "{{") {
			continue // resolved when the step runs, then checked again
		}
		cv, err := convert(a, v, true)
		if err != nil {
			problems = append(problems, fmt.Sprintf("argument %q: %v", a.Name, err))
			continue
		}
		out[a.Name] = cv
	}

	if d, ok := out[TimeoutArg].(time.Duration); ok && (d <= 0 || d > maxTimeout) {
		problems = append(problems, fmt.Sprintf("argument %q: %s must be between 1ns and %s", TimeoutArg, d, maxTimeout))
	}
	return out, problems
}

// convert turns a raw value into the declared type. With lenient true a text
// value is also parsed, which is how a rendered template becomes a number.
func convert(a Arg, v any, lenient bool) (any, error) {
	switch a.Type {
	case String:
		if s, ok := v.(string); ok {
			return s, nil
		}
	case Int:
		switch n := v.(type) {
		case int:
			return n, nil
		case string:
			if lenient {
				if i, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
					return i, nil
				}
			}
		}
	case Bool:
		switch b := v.(type) {
		case bool:
			return b, nil
		case string:
			if lenient {
				if p, err := strconv.ParseBool(strings.TrimSpace(b)); err == nil {
					return p, nil
				}
			}
		}
	case Duration:
		switch d := v.(type) {
		case time.Duration:
			return d, nil
		case string:
			if p, err := time.ParseDuration(strings.TrimSpace(d)); err == nil {
				return p, nil
			}
		}
	case StringList:
		switch l := v.(type) {
		case []string:
			return slices.Clone(l), nil
		case []any:
			out := make([]string, len(l))
			for i, item := range l {
				s, ok := item.(string)
				if !ok {
					return nil, fmt.Errorf("item %d is %T, want a list of text values", i, item)
				}
				out[i] = s
			}
			return out, nil
		}
	case Map:
		if m, ok := v.(map[string]any); ok {
			return m, nil
		}
	}
	return nil, fmt.Errorf("%v (%T) is not a %s", v, v, a.Type)
}
