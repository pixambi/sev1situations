package html

import (
	"bytes"
	_ "embed"
	"fmt"
	htmltemplate "html/template"
	"sort"
	"strings"
	"time"

	"github.com/pixambi/sev1situations/internal/report"
)

//go:embed report.html.tmpl
var source string

// Renderer renders reports as HTML. It implements report.Renderer.
type Renderer struct {
	// Location is the time zone the page shows times in. The data holds UTC. The
	// zero value shows UTC, which makes the output the same everywhere.
	Location *time.Location
}

var _ report.Renderer = Renderer{}

// Extension implements report.Renderer.
func (Renderer) Extension() string { return ".html" }

// ContentType implements report.Renderer.
func (Renderer) ContentType() string { return "text/html; charset=utf-8" }

// Render implements report.Renderer.
func (rd Renderer) Render(r *report.Report) ([]byte, error) {
	loc := rd.Location
	if loc == nil {
		loc = time.UTC
	}
	tmpl, err := htmltemplate.New("report").Funcs(htmltemplate.FuncMap{
		"when":      func(t time.Time) string { return when(t, loc) },
		"span":      func(d time.Duration) string { return span(d) },
		"spanMs":    func(ms int64) string { return span(time.Duration(ms) * time.Millisecond) },
		"title":     title,
		"stars":     stars,
		"fmtParam":  func(v any) string { return fmt.Sprint(v) },
		"inc":       func(i int) int { return i + 1 },
		"stateName": stateName,
		"outcome":   outcomeName,
	}).Parse(source)
	if err != nil {
		return nil, fmt.Errorf("parse the report template: %w", err)
	}

	var out bytes.Buffer
	if err := tmpl.Execute(&out, view(r)); err != nil {
		return nil, fmt.Errorf("render report %q: %w", r.ID, err)
	}
	return out.Bytes(), nil
}

// page is what the template reads.
type page struct {
	*report.Report
	Params   []param
	Duration time.Duration
	Ended    bool
}

type param struct {
	Name  string
	Value any
}

func view(r *report.Report) page {
	p := page{Report: r, Duration: r.Duration(), Ended: !r.EndedAt.IsZero()}
	names := make([]string, 0, len(r.Params))
	for k := range r.Params {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		p.Params = append(p.Params, param{Name: k, Value: r.Params[k]})
	}
	return p
}

// when formats a time in the page's zone, or says it never happened.
func when(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return "not reached"
	}
	return t.In(loc).Format("2 Jan 2006, 15:04:05 MST")
}

// span formats a length of time the way a person says it: "42m 7s", "1h 5m", "9s".
func span(d time.Duration) string {
	d = d.Round(time.Second)
	if d <= 0 {
		return "0s"
	}
	h, m, s := int(d/time.Hour), int(d%time.Hour/time.Minute), int(d%time.Minute/time.Second)
	var parts []string
	if h > 0 {
		parts = append(parts, fmt.Sprintf("%dh", h))
	}
	if m > 0 {
		parts = append(parts, fmt.Sprintf("%dm", m))
	}
	if s > 0 && h == 0 {
		parts = append(parts, fmt.Sprintf("%ds", s))
	}
	return strings.Join(parts, " ")
}

// title upper-cases the first letter: "ended early" becomes "Ended early".
func title(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// stars writes a rating as words and as filled and empty marks, so it reads without colour.
func stars(n int) string {
	if n < 1 || n > 5 {
		return "Not rated"
	}
	return fmt.Sprintf("%s%s  %d of 5", strings.Repeat("★", n), strings.Repeat("☆", 5-n), n)
}

// stateName says in words where a step was left.
func stateName(s string) string {
	switch s {
	case "complete":
		return "Complete"
	case "locked":
		return "Not reached"
	case "active":
		return "In progress"
	case "applying", "applied":
		return "In progress"
	default:
		return title(s)
	}
}

// outcomeName says in words how a step's last operation ended.
func outcomeName(s string) string {
	switch s {
	case "pass":
		return "Passed"
	case "fail":
		return "Not as expected"
	case "error":
		return "Could not run"
	default:
		return title(s)
	}
}
