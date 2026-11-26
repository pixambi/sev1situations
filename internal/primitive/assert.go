package primitive

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const assertTimeout = 5 * time.Second

type assertEquals struct{}

func (assertEquals) Spec() Spec {
	return Spec{
		Name: "assert.equals", Summary: "Checks that two values are equal. It changes nothing and is safe to run again.",
		Timeout: assertTimeout,
		Args: []Arg{
			{Name: "actual", Type: String, Required: true, Help: "The value found, usually a template such as {{ .Outputs.leader }}."},
			{Name: "expected", Type: String, Required: true, Help: "The value it should be."},
			{Name: "trim", Type: Bool, Default: true, Help: "Ignore spaces and line breaks at both ends."},
		},
	}
}

func (assertEquals) Run(_ context.Context, _ Env, a Args) (Result, error) {
	actual, expected := a.String("actual"), a.String("expected")
	if a.Bool("trim") {
		actual, expected = strings.TrimSpace(actual), strings.TrimSpace(expected)
	}
	if actual != expected {
		return failed("", "expected %q, got %q", expected, actual), nil
	}
	return passed("equal to %q", expected), nil
}

type assertContains struct{}

func (assertContains) Spec() Spec {
	return Spec{
		Name: "assert.contains", Summary: "Checks that a text contains another text. It changes nothing and is safe to run again.",
		Timeout: assertTimeout,
		Args: []Arg{
			{Name: "text", Type: String, Required: true, Help: "The text to search."},
			{Name: "substring", Type: String, Required: true, Help: "What it should contain."},
		},
	}
}

func (assertContains) Run(_ context.Context, _ Env, a Args) (Result, error) {
	if !strings.Contains(a.String("text"), a.String("substring")) {
		return failed(a.String("text"), "the text does not contain %q", a.String("substring")), nil
	}
	return passed("contains %q", a.String("substring")), nil
}

type assertMatches struct{}

func (assertMatches) Spec() Spec {
	return Spec{
		Name: "assert.matches", Summary: "Checks that a text matches a regular expression. It changes nothing and is safe to run again.",
		Timeout: assertTimeout,
		Args: []Arg{
			{Name: "text", Type: String, Required: true, Help: "The text to search."},
			{Name: "pattern", Type: String, Required: true, Help: "A Go regular expression. It may match anywhere in the text."},
		},
	}
}

func (assertMatches) Run(_ context.Context, _ Env, a Args) (Result, error) {
	re, err := regexp.Compile(a.String("pattern"))
	if err != nil {
		return Result{}, fmt.Errorf("pattern %q is not a valid regular expression: %w", a.String("pattern"), err)
	}
	if !re.MatchString(a.String("text")) {
		return failed(a.String("text"), "the text does not match %q", a.String("pattern")), nil
	}
	return passed("matches %q", a.String("pattern")), nil
}

type assertJSONPath struct{}

func (assertJSONPath) Spec() Spec {
	return Spec{
		Name: "assert.json-path",
		Summary: "Reads a value from JSON by a path such as data.items[0].name, and checks that it exists " +
			"and, if given, equals a value. It changes nothing and is safe to run again.",
		Timeout: assertTimeout,
		Args: []Arg{
			{Name: "json", Type: String, Required: true, Help: "The JSON text, usually a template such as {{ .Outputs.status }}."},
			{Name: "path", Type: String, Required: true, Help: "Dotted names and [index] steps, for example raft.voters[0]."},
			{Name: "equals", Type: String, Help: "If set, the value, written as text, must equal this."},
			argSave,
		},
	}
}

func (assertJSONPath) Run(_ context.Context, _ Env, a Args) (Result, error) {
	var doc any
	if err := json.Unmarshal([]byte(a.String("json")), &doc); err != nil {
		return Result{}, fmt.Errorf("the value is not valid JSON: %w", err)
	}
	value, err := jsonPath(doc, a.String("path"))
	if err != nil {
		return failed(a.String("json"), "%s", err), nil
	}

	text := jsonText(value)
	res := Result{Evidence: truncate(text)}
	if name := a.String("save"); name != "" {
		res.Outputs = map[string]string{name: text}
	}
	if a.Has("equals") && text != a.String("equals") {
		res.Status, res.Message = Fail, fmt.Sprintf("%s is %q, expected %q", a.String("path"), text, a.String("equals"))
		return res, nil
	}
	res.Status, res.Message = Pass, fmt.Sprintf("%s is %q", a.String("path"), text)
	return res, nil
}

var pathStep = regexp.MustCompile(`^([^\[\]]*)((?:\[\d+\])*)$`)
var indexStep = regexp.MustCompile(`\[(\d+)\]`)

// jsonPath follows a path of names and [index] steps through decoded JSON.
func jsonPath(doc any, path string) (any, error) {
	cur := doc
	for _, seg := range strings.Split(path, ".") {
		m := pathStep.FindStringSubmatch(seg)
		if m == nil || (m[1] == "" && m[2] == "") {
			return nil, fmt.Errorf("the path %q is not valid at %q", path, seg)
		}
		if m[1] != "" {
			obj, ok := cur.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%q: cannot read %q from a value that is not an object", path, m[1])
			}
			if cur, ok = obj[m[1]]; !ok {
				return nil, fmt.Errorf("%q: there is no %q", path, m[1])
			}
		}
		for _, idx := range indexStep.FindAllStringSubmatch(m[2], -1) {
			i, _ := strconv.Atoi(idx[1])
			list, ok := cur.([]any)
			if !ok || i >= len(list) {
				return nil, fmt.Errorf("%q: there is no item %d", path, i)
			}
			cur = list[i]
		}
	}
	return cur, nil
}

// jsonText writes a decoded JSON value as plain text: strings without quotes,
// whole numbers without a decimal point, and objects and lists as compact JSON.
func jsonText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case nil:
		return "null"
	}
	b, _ := json.Marshal(v)
	return string(b)
}
