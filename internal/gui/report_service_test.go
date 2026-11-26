package gui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/report"
	htmlreport "github.com/pixambi/sev1situations/internal/report/html"
)

// chosen is a save dialog that answers with a fixed path, and remembers what it was asked.
type chosen struct {
	path      string
	err       error
	suggested []string
}

func (c *chosen) dialog(name string) (string, error) {
	c.suggested = append(c.suggested, name)
	return c.path, c.err
}

type reportsRig struct {
	svc   *ReportService
	store *report.Store
	dir   string // the store's folder
	out   string // where the dialog sends exports
	pick  *chosen
	id    string
}

func reportsOf(t *testing.T) reportsRig {
	t.Helper()
	store, id, dir := savedReportIn(t)
	out := t.TempDir()
	c := &chosen{path: filepath.Join(out, "exported")}
	return reportsRig{NewReportService(store, htmlreport.Renderer{}, c.dialog), store, dir, out, c, id}
}

func TestListShowsTheSavedReportsAndWhatCouldNotBeRead(t *testing.T) {
	r := reportsOf(t)
	if err := os.WriteFile(filepath.Join(r.dir, "bad.json"), []byte(`{"version":1,`), 0o600); err != nil {
		t.Fatal(err)
	}
	list, err := r.svc.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Reports) != 1 || list.Reports[0].ScenarioName != "Lost Quorum" {
		t.Errorf("reports = %+v", list.Reports)
	}
	if len(list.Problems) != 1 || !strings.HasPrefix(list.Problems[0], "bad.json: ") {
		t.Errorf("problems = %v", list.Problems)
	}
}

func TestListOfNothingIsEmptyListsNotNull(t *testing.T) {
	svc := NewReportService(report.NewStore(filepath.Join(t.TempDir(), "none")), htmlreport.Renderer{}, (&chosen{}).dialog)
	list, err := svc.List()
	if err != nil || list.Reports == nil || list.Problems == nil {
		t.Errorf("got %+v, %v", list, err)
	}
}

func TestPageGivesTheAddressOfAReportThatCanBeShown(t *testing.T) {
	r := reportsOf(t)
	got, err := r.svc.Page(r.id)
	if err != nil || got != "/_reports/"+r.id+".html" {
		t.Errorf("got %q, %v", got, err)
	}
	for _, id := range []string{"nope", "../x", ""} {
		if _, err := r.svc.Page(id); err == nil {
			t.Errorf("Page(%q) succeeded", id)
		}
	}
}

func TestDeleteRemovesAReportAndExplainsWhenItIsGone(t *testing.T) {
	r := reportsOf(t)
	if err := r.svc.Delete(r.id); err != nil {
		t.Fatal(err)
	}
	if _, err := r.store.Get(r.id); !errors.Is(err, report.ErrNotFound) {
		t.Errorf("the report is still there: %v", err)
	}
	if err := r.svc.Delete(r.id); err == nil || !strings.Contains(err.Error(), "no longer exists") {
		t.Errorf("deleting twice: %v", err)
	}
	if err := r.svc.Delete("../settings"); err == nil || !strings.Contains(err.Error(), "not a report") {
		t.Errorf("a bad ID: %v", err)
	}
}

func TestExportingThePageWritesTheSelfContainedHTMLWhereTheInstructorChose(t *testing.T) {
	r := reportsOf(t)
	res, err := r.svc.Export(r.id, "html")
	if err != nil || res.Cancelled || res.Path != r.pick.path {
		t.Fatalf("got %+v, %v", res, err)
	}
	got, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "<!doctype html>") || !strings.Contains(string(got), "Lost Quorum") || strings.Contains(string(got), "<b>Hopper</b>") {
		t.Error("the file is not the rendered report")
	}
	if len(r.pick.suggested) != 1 || r.pick.suggested[0] != "Lost Quorum - Grace bHopperb - 2026-11-26.html" {
		t.Errorf("suggested = %q", r.pick.suggested)
	}
}

func TestExportingTheDataWritesTheStoredReportExactly(t *testing.T) {
	r := reportsOf(t)
	res, err := r.svc.Export(r.id, "data")
	if err != nil || res.Path == "" {
		t.Fatalf("got %+v, %v", res, err)
	}
	got, _ := os.ReadFile(res.Path)
	stored, _ := os.ReadFile(filepath.Join(r.dir, r.id+".json"))
	if string(got) != string(stored) {
		t.Error("the exported data is not what is stored")
	}
	var back report.Report
	if err := json.Unmarshal(got, &back); err != nil || back.ID != r.id {
		t.Errorf("the export is not a report: %v", err)
	}
	if !strings.HasSuffix(r.pick.suggested[0], ".json") {
		t.Errorf("suggested = %q", r.pick.suggested)
	}
}

func TestCancellingTheDialogWritesNothingAndIsNotAnError(t *testing.T) {
	r := reportsOf(t)
	r.pick.path = ""
	res, err := r.svc.Export(r.id, "html")
	if err != nil || !res.Cancelled || res.Path != "" {
		t.Errorf("got %+v, %v", res, err)
	}
	if entries, _ := os.ReadDir(r.out); len(entries) != 0 {
		t.Errorf("a cancelled export left %d files", len(entries))
	}
}

func TestExportExplainsWhatWentWrong(t *testing.T) {
	r := reportsOf(t)
	if _, err := r.svc.Export(r.id, "pdf"); err == nil || !strings.Contains(err.Error(), "page or the raw data") {
		t.Errorf("an unknown format: %v", err)
	}
	if _, err := r.svc.Export("nope", "html"); err == nil || !strings.Contains(err.Error(), "no longer exists") {
		t.Errorf("an unknown report: %v", err)
	}
	if len(r.pick.suggested) != 0 {
		t.Error("the dialog opened for an export that could not happen")
	}

	r.pick.err = errors.New("no window")
	if _, err := r.svc.Export(r.id, "html"); err == nil || !strings.Contains(err.Error(), "no window") {
		t.Errorf("a dialog that failed: %v", err)
	}

	r.pick.err, r.pick.path = nil, filepath.Join(r.out, "missing-folder", "x.html")
	if _, err := r.svc.Export(r.id, "html"); err == nil || !strings.Contains(err.Error(), "x.html") {
		t.Errorf("a folder that vanished: %v", err)
	}
}

func TestExportNamesAreSafeAndReadable(t *testing.T) {
	at := func(name, trainee string) *report.Report {
		return &report.Report{ID: "20261126-090000-abc", Scenario: report.Scenario{Name: name}, Trainee: trainee, StartedAt: savedAt}
	}
	for _, tt := range []struct {
		r    *report.Report
		want string
	}{
		{at("Lost Quorum", "Grace Hopper"), "Lost Quorum - Grace Hopper - 2026-11-26.html"},
		{at("A/B: test*", `x\y?`), "AB test - xy - 2026-11-26.html"},
		{at("../../etc", "passwd"), "etc - passwd - 2026-11-26.html"},
		{at("日本語", "名前"), "report 20261126-090000-abc.html"},
		{at("", ""), "report 20261126-090000-abc.html"},
	} {
		if got := exportName(tt.r, ".html"); got != tt.want {
			t.Errorf("exportName(%q, %q) = %q, want %q", tt.r.Scenario.Name, tt.r.Trainee, got, tt.want)
		}
		if strings.ContainsAny(exportName(tt.r, ".html"), `/\:*?"<>|`) {
			t.Errorf("an unsafe name: %q", exportName(tt.r, ".html"))
		}
	}
}

var savedAt = time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
