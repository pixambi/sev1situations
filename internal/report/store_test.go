package report

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func sample(id string, started time.Time) *Report {
	return &Report{
		ID:         id,
		Scenario:   Scenario{ID: "vault-lost-quorum", Name: "Lost Quorum", Product: "vault"},
		Instructor: "Ada Lovelace", Trainee: "Grace Hopper",
		Params:    map[string]any{"version": "2.2.2", "edition": "community", "nodes": 3},
		StartedAt: started, EndedAt: started.Add(42*time.Minute + 7*time.Second), Result: EndedEarly,
		Steps: []Step{
			{ID: "break", Title: "Cause the outage", Kind: "breakage", State: "complete", Verified: true,
				StartedAt: started.Add(time.Minute), EndedAt: started.Add(5 * time.Minute), DurationMs: 240000, Attempts: 2,
				Notes: "found it fast", Rating: 4, Outcome: &Outcome{Status: "pass", Message: "the breakage is in place", At: started.Add(5 * time.Minute)}},
			{ID: "fix", Title: "Recover quorum", Kind: "verification", State: "active", Attempts: 1},
		},
		Events: []Event{
			{At: started.Add(10 * time.Minute), Kind: "rollback", Step: "break", Message: "rolled back to break"},
			{At: started.Add(42 * time.Minute), Kind: "run-ended", Message: "the run ended: ended early"},
		},
	}
}

var t0 = time.Date(2026, 11, 26, 9, 15, 30, 123456789, time.UTC)

func TestAReportSurvivesBeingSavedAndRead(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "reports"))
	in := sample("20261126-091530-abc123", t0)
	if err := s.Save(in); err != nil {
		t.Fatal(err)
	}

	out, err := s.Get(in.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out.Version != SchemaVersion {
		t.Errorf("version = %d, want %d", out.Version, SchemaVersion)
	}
	// JSON numbers come back as float64, so compare through the type JSON uses.
	in.Params["nodes"] = float64(3)
	if !reflect.DeepEqual(in, out) {
		t.Errorf("the report changed on the way\n in: %+v\nout: %+v", in, out)
	}
	if !out.StartedAt.Equal(t0) || out.Duration() != 42*time.Minute+7*time.Second {
		t.Errorf("times: started %v, duration %v", out.StartedAt, out.Duration())
	}
}

func TestTheFileIsPrivateAndHasNoTemporaryFilesBesideIt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "reports")
	s := NewStore(dir)
	if err := s.Save(sample("r1", t0)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "r1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
	if di, _ := os.Stat(dir); di.Mode().Perm() != 0o700 {
		t.Errorf("folder mode = %v, want 0700", di.Mode().Perm())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("folder holds %d entries, want 1", len(entries))
	}
}

func TestASavedReportIsNeverOverwritten(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Save(sample("r1", t0)); err != nil {
		t.Fatal(err)
	}
	changed := sample("r1", t0)
	changed.Trainee = "Someone Else"
	if err := s.Save(changed); !errors.Is(err, ErrExists) {
		t.Fatalf("err = %v, want ErrExists", err)
	}
	got, _ := s.Get("r1")
	if got.Trainee != "Grace Hopper" {
		t.Errorf("the saved report was changed to %q", got.Trainee)
	}
}

func TestAnIDCannotNameAFileOutsideTheFolder(t *testing.T) {
	s := NewStore(t.TempDir())
	for _, id := range []string{"", "../escape", "a/b", `a\b`, ".hidden", "a b", strings.Repeat("x", 129), "a\x00b"} {
		if err := s.Save(sample(id, t0)); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Save(%q) = %v", id, err)
		}
		if _, err := s.Get(id); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Get(%q) = %v", id, err)
		}
		if err := s.Delete(id); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Delete(%q) = %v", id, err)
		}
	}
}

func TestListIsNewestFirstAndOneBadFileDoesNotHideTheRest(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	for i, id := range []string{"old", "newest", "middle"} {
		r := sample(id, t0.Add(time.Duration([]int{0, 2, 1}[i])*time.Hour))
		if err := s.Save(r); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(dir, "broken.json"), []byte(`{"version": 1, "id": "broken", "trainee": 5`), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a report"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, ".half-*.json"), []byte("{"), 0o600) // a temporary file from a crash
	_ = os.Mkdir(filepath.Join(dir, "sub.json"), 0o700)

	list, problems, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, x := range list {
		ids = append(ids, x.ID)
	}
	if want := []string{"newest", "middle", "old"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("order = %v, want %v", ids, want)
	}
	if len(problems) != 1 || problems[0].File != "broken.json" {
		t.Errorf("problems = %+v, want only broken.json", problems)
	}
	if list[0].ScenarioName != "Lost Quorum" || list[0].Result != EndedEarly {
		t.Errorf("summary = %+v", list[0])
	}
}

func TestListWithNothingSavedIsEmptyNotAnError(t *testing.T) {
	list, problems, err := NewStore(filepath.Join(t.TempDir(), "never-made")).List()
	if err != nil || list == nil || len(list) != 0 || problems != nil {
		t.Errorf("got %v, %v, %v", list, problems, err)
	}
}

func TestDeleteRemovesAReportAndSaysWhenThereIsNone(t *testing.T) {
	s := NewStore(t.TempDir())
	_ = s.Save(sample("r1", t0))
	if err := s.Delete("r1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("r1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("after deleting: %v", err)
	}
	if err := s.Delete("r1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting twice: %v", err)
	}
}

func TestAReportFromANewerVersionIsRefusedAndAnOlderOneIsRead(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	write := func(id string, version int) {
		b, _ := json.Marshal(map[string]any{"version": version, "id": id, "trainee": "Grace"})
		_ = os.WriteFile(filepath.Join(dir, id+".json"), b, 0o600)
	}
	write("future", SchemaVersion+1)
	write("current", SchemaVersion)

	if _, err := s.Get("future"); !errors.Is(err, ErrTooNew) {
		t.Errorf("a newer report: %v", err)
	}
	if r, err := s.Get("current"); err != nil || r.Trainee != "Grace" {
		t.Errorf("the current version: %+v, %v", r, err)
	}
	_ = os.WriteFile(filepath.Join(dir, "unversioned.json"), []byte(`{"id":"unversioned"}`), 0o600)
	if _, err := s.Get("unversioned"); err == nil {
		t.Error("a file with no version was read as a report")
	}
}

func TestADamagedOrRenamedFileIsDescribedWithoutEchoingItsContent(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	_ = os.WriteFile(filepath.Join(dir, "bad.json"), []byte(`{"version":1,"id":"bad","notes":"SECRET-NOTE" oops`), 0o600)
	_, err := s.Get("bad")
	if err == nil || strings.Contains(err.Error(), "SECRET-NOTE") || !strings.Contains(err.Error(), "damaged") {
		t.Errorf("err = %v", err)
	}

	_ = s.Save(sample("orig", t0))
	b, _ := os.ReadFile(filepath.Join(dir, "orig.json"))
	_ = os.WriteFile(filepath.Join(dir, "copy.json"), b, 0o600)
	if _, err := s.Get("copy"); err == nil || !strings.Contains(err.Error(), "renamed or copied") {
		t.Errorf("a copied file: %v", err)
	}
}

func TestRawReturnsTheStoredBytesAndRefusesAnInvalidFile(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	_ = s.Save(sample("r1", t0))
	raw, err := s.Raw("r1")
	if err != nil {
		t.Fatal(err)
	}
	onDisk, _ := os.ReadFile(filepath.Join(dir, "r1.json"))
	if string(raw) != string(onDisk) {
		t.Error("Raw is not what is stored")
	}
	var back Report
	if err := json.Unmarshal(raw, &back); err != nil || back.ID != "r1" {
		t.Errorf("the raw data is not a report: %v", err)
	}

	_ = os.WriteFile(filepath.Join(dir, "bad.json"), []byte("not json"), 0o600)
	if _, err := s.Raw("bad"); err == nil {
		t.Error("Raw handed out a file that is not a report")
	}
	if _, err := s.Raw("missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
}

func TestNewIDSortsByDateAndKeepsTheRunID(t *testing.T) {
	a := NewID("abc123", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	b := NewID("abc123", time.Date(2026, 11, 26, 9, 15, 30, 0, time.UTC))
	if a != "20260102-030405-abc123" || a >= b {
		t.Errorf("ids = %q, %q", a, b)
	}
	if !idPattern.MatchString(a) {
		t.Error("a generated ID is not a valid ID")
	}
	// A time in another zone gives the same ID as the same instant in UTC.
	if NewID("x", time.Date(2026, 1, 2, 5, 4, 5, 0, time.FixedZone("x", 7200))) != "20260102-030405-x" {
		t.Error("the ID depends on the time zone")
	}
}
