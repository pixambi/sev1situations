package engine

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/pixambi/sev1situations/internal/scenario"
)

// ResolveParams combines the values the instructor chose with the scenario's
// defaults and checks each against its declaration (R-SCN-11): an int must be a
// whole number within its bounds, an edition must be one the scenario supports,
// and a version must not be empty. A value the scenario does not declare is an
// error, so a stale form cannot slip in an input nothing reads.
func ResolveParams(sc *scenario.Scenario, given map[string]any) (map[string]any, error) {
	declared := map[string]bool{}
	for _, p := range sc.Parameters {
		declared[p.ID] = true
	}
	var unknown []string
	for k := range given {
		if !declared[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("unknown parameter(s): %s", strings.Join(unknown, ", "))
	}

	out := map[string]any{}
	for _, p := range sc.Parameters {
		v, ok := given[p.ID]
		if !ok || v == nil || v == "" {
			v = p.Default
		}
		if v == nil || v == "" {
			return nil, fmt.Errorf("parameter %q (%s) needs a value", p.ID, p.Label)
		}
		checked, err := checkParam(sc, p, v)
		if err != nil {
			return nil, fmt.Errorf("parameter %q (%s): %w", p.ID, p.Label, err)
		}
		out[p.ID] = checked
	}
	return out, nil
}

func checkParam(sc *scenario.Scenario, p scenario.Parameter, v any) (any, error) {
	switch p.Type {
	case scenario.ParamInt:
		n, err := toInt(v)
		if err != nil {
			return nil, err
		}
		if p.Min != nil && n < *p.Min || p.Max != nil && n > *p.Max {
			return nil, fmt.Errorf("%d is outside %d to %d", n, deref(p.Min), deref(p.Max))
		}
		return n, nil
	case scenario.ParamEdition:
		s, _ := v.(string)
		if !slices.Contains(sc.Editions, s) {
			return nil, fmt.Errorf("%q is not one of %s", v, strings.Join(sc.Editions, ", "))
		}
		return s, nil
	default: // version
		s, ok := v.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return nil, fmt.Errorf("%v is not a version", v)
		}
		return strings.TrimSpace(s), nil
	}
}

func toInt(v any) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case float64:
		if n == float64(int(n)) {
			return int(n), nil
		}
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
			return i, nil
		}
	}
	return 0, fmt.Errorf("%v is not a whole number", v)
}

func deref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
