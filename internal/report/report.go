package report

import (
	"time"
)

// SchemaVersion is the version of the report data written today. It goes up when a
// field changes in a way an older reader would misread. A reader refuses a report
// with a higher version than it knows, and reads an older one as it was written.
const SchemaVersion = 1

// Result says how a run ended (R-REPORT-6).
const (
	// Completed means every step was completed, or the scenario was an open sandbox.
	Completed = "completed"
	// EndedEarly means the instructor ended the run before its last step.
	EndedEarly = "ended early"
)

// Report is the record of one run: who ran what, when, how each step went and what
// happened along the way (R-REPORT-5). It never holds a credential.
type Report struct {
	// Version is the schema version this report was written with.
	Version int `json:"version"`
	// ID names the report. It is unique and is the name of its file.
	ID string `json:"id"`

	// Scenario says what ran.
	Scenario Scenario `json:"scenario"`
	// Instructor and Trainee are the names entered when the run started.
	Instructor string `json:"instructor"`
	Trainee    string `json:"trainee"`
	// Params are the parameter values the run was started with, by parameter ID.
	Params map[string]any `json:"params"`

	// StartedAt and EndedAt are when the run began and when it ended, in UTC.
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`
	// Result is Completed or EndedEarly.
	Result string `json:"result"`

	// Steps are the scenario's steps in order, each in the state it was left in.
	Steps []Step `json:"steps"`
	// Events are the notable things that happened, in order.
	Events []Event `json:"events"`
}

// Scenario identifies the scenario a report is about.
type Scenario struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Product string `json:"product"`
}

// Step is one step of the run as it ended (R-REPORT-5).
type Step struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Kind is breakage or verification.
	Kind string `json:"kind"`
	// State is where the step was when the run ended: locked, active, applying,
	// applied or complete.
	State string `json:"state"`
	// Verified reports that the step's verification passed.
	Verified bool `json:"verified"`
	// StartedAt and EndedAt are the current attempt's times. They are zero for a
	// step never reached, and EndedAt is zero for one left unfinished.
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`
	// DurationMs is the total time spent on the step, including attempts before a
	// roll back, in milliseconds.
	DurationMs int64 `json:"durationMs"`
	// Attempts counts how many times the step was started.
	Attempts int `json:"attempts"`
	// Notes are the instructor's notes on the step.
	Notes string `json:"notes"`
	// Rating is the instructor's rating from 1 to 5, or 0 for none.
	Rating int `json:"rating"`
	// Outcome is how the last operation on the step ended, if there was one.
	Outcome *Outcome `json:"outcome,omitempty"`
}

// Outcome is how an operation on a step ended.
type Outcome struct {
	// Status is pass, fail or error.
	Status  string    `json:"status"`
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}

// Event is one notable thing that happened during the run, such as a roll back, a
// retry or the run being ended early (R-REPORT-5).
type Event struct {
	At time.Time `json:"at"`
	// Kind says what happened, using the engine's event kinds.
	Kind string `json:"kind"`
	// Step is the step it concerns, if any.
	Step    string `json:"step,omitempty"`
	Message string `json:"message"`
}

// Summary is a report's line in the list (R-REPORT-7).
type Summary struct {
	ID           string    `json:"id"`
	ScenarioName string    `json:"scenarioName"`
	Instructor   string    `json:"instructor"`
	Trainee      string    `json:"trainee"`
	StartedAt    time.Time `json:"startedAt"`
	EndedAt      time.Time `json:"endedAt"`
	Result       string    `json:"result"`
}

// Summary returns the report's line in the list.
func (r *Report) Summary() Summary {
	return Summary{
		ID: r.ID, ScenarioName: r.Scenario.Name, Instructor: r.Instructor, Trainee: r.Trainee,
		StartedAt: r.StartedAt, EndedAt: r.EndedAt, Result: r.Result,
	}
}

// Duration is how long the run lasted.
func (r *Report) Duration() time.Duration { return r.EndedAt.Sub(r.StartedAt) }

// NewID returns the ID for the report of a run: when it started, to the second, and
// the run's own ID, so reports sort by date and two runs never share one.
func NewID(runID string, started time.Time) string {
	return started.UTC().Format("20060102-150405") + "-" + runID
}

// Renderer turns a report into a document in some format. It is a pure function of
// the report: it reads nothing else and changes nothing, so a new format, such as
// PDF, needs no change to the data or to the code that produces it (R-REPORT-3).
type Renderer interface {
	// Render returns the report as a document.
	Render(r *Report) ([]byte, error)
	// Extension is the file extension of the format, with its dot, for example ".html".
	Extension() string
	// ContentType is the format's media type.
	ContentType() string
}
