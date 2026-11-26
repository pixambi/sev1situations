package engine

import (
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/report"
)

// ReportSaver stores a finished run's report. The report package's Store is one.
type ReportSaver interface {
	Save(r *report.Report) error
}

// reportEvents are the engine events a report keeps. The report lists what an
// instructor would want to see when looking back: the run's trouble and its turns, not
// every progress line (R-REPORT-5).
var reportEvents = map[EventKind]bool{
	EventRetry:          true,
	EventRollback:       true,
	EventRollbackFailed: true,
	EventDeployFailed:   true,
	EventNodeAction:     true,
	EventRunEnded:       true,
}

// buildReport turns a run's final snapshot into its report (R-REPORT-5, R-REPORT-6).
// Everything free-form, the notes the instructor typed and the messages the steps
// produced, is passed through the redactor, so a credential that was pasted into a
// note or echoed by a check never reaches a report (R-QUAL-3). Evidence, which is raw
// output, is left out altogether.
func buildReport(s Snapshot, red *primitive.Redactor) *report.Report {
	r := &report.Report{
		ID:       report.NewID(s.RunID, s.StartedAt),
		Scenario: report.Scenario{ID: s.ScenarioID, Name: s.ScenarioName, Product: s.Product},
		// Names are the instructor's own, and may be typed anywhere, so they are scrubbed as well.
		Instructor: red.Redact(s.Instructor),
		Trainee:    red.Redact(s.Trainee),
		Params:     maps.Clone(s.Params),
		StartedAt:  s.StartedAt.UTC(),
		EndedAt:    s.EndedAt.UTC(),
		Result:     s.Result,
		Steps:      []report.Step{},
		Events:     []report.Event{},
	}
	if r.Params == nil {
		r.Params = map[string]any{}
	}

	for _, st := range s.Steps {
		step := report.Step{
			ID: st.ID, Title: st.Title, Kind: st.Kind, State: string(st.State), Verified: st.Verified,
			StartedAt: utc(st.Started), EndedAt: utc(st.Ended), DurationMs: st.Elapsed.Milliseconds(),
			Attempts: st.Attempts, Notes: red.Redact(st.Notes), Rating: st.Rating,
		}
		if st.Last != nil {
			step.Outcome = &report.Outcome{Status: st.Last.Status, Message: red.Redact(st.Last.Message), At: utc(st.Last.At)}
		}
		r.Steps = append(r.Steps, step)
	}

	for _, ev := range s.Events {
		if !reportEvents[ev.Kind] {
			continue
		}
		r.Events = append(r.Events, report.Event{At: ev.Time.UTC(), Kind: string(ev.Kind), Step: ev.Step, Message: red.Redact(ev.Message)})
	}
	// A teardown that did not finish matters to the instructor, who may have containers to remove.
	for _, ev := range s.Events {
		if ev.Kind == EventTeardown && strings.HasPrefix(ev.Message, "teardown did not finish") {
			r.Events = append(r.Events, report.Event{At: ev.Time.UTC(), Kind: string(ev.Kind), Message: red.Redact(ev.Message)})
		}
	}
	sortEvents(r.Events)
	return r
}

func utc(t time.Time) time.Time {
	if t.IsZero() {
		return time.Time{}
	}
	return t.UTC()
}

// sortEvents keeps events in the order they happened, which the teardown event added
// last may not be in. The sort is stable, so events with one time keep their order.
func sortEvents(evs []report.Event) {
	for i := 1; i < len(evs); i++ {
		for j := i; j > 0 && evs[j].At.Before(evs[j-1].At); j-- {
			evs[j], evs[j-1] = evs[j-1], evs[j]
		}
	}
}

// saveReport writes the report of a run that has ended. A run's report is always
// saved, including one ended early (R-REPORT-6). With no ReportSaver nothing is saved,
// as in tests of the engine itself.
func (e *Engine) saveReport(s Snapshot, red *primitive.Redactor) error {
	if e.deps.Reports == nil {
		return nil
	}
	if err := e.deps.Reports.Save(buildReport(s, red)); err != nil {
		return fmt.Errorf("save the report: %w", err)
	}
	return nil
}
