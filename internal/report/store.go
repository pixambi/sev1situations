package report

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/pixambi/sev1situations/internal/atomicfile"
)

// Errors a store returns, to be matched with errors.Is.
var (
	// ErrNotFound means there is no report with that ID.
	ErrNotFound = errors.New("no such report")
	// ErrExists means a report with that ID is already saved. A saved report is a
	// record, so it is never overwritten.
	ErrExists = errors.New("a report with that ID already exists")
	// ErrInvalidID means the ID could not be the name of a report.
	ErrInvalidID = errors.New("not a valid report ID")
	// ErrTooNew means the report was written by a newer version of the application.
	ErrTooNew = errors.New("the report was written by a newer version of the application")
)

const (
	fileExt  = ".json"
	filePerm = 0o600 // names and notes about a trainee are the instructor's to share, not the machine's
	dirPerm  = 0o700
)

// idPattern is what an ID may look like. It cannot hold a path separator or start
// with a dot, so an ID can never name a file outside the reports folder.
var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// Store keeps reports as one JSON file each, in a folder (R-STATE-4). There is no
// database.
type Store struct {
	dir string
}

// NewStore returns a store that keeps its reports in dir. The folder is made when
// the first report is saved.
func NewStore(dir string) *Store { return &Store{dir: dir} }

// Save writes a report. It sets the schema version, and refuses a report whose ID is
// invalid or already saved. The write is atomic: a crash leaves no half report.
func (s *Store) Save(r *Report) error {
	if !idPattern.MatchString(r.ID) {
		return fmt.Errorf("save report %q: %w", r.ID, ErrInvalidID)
	}
	r.Version = SchemaVersion
	if err := os.MkdirAll(s.dir, dirPerm); err != nil {
		return fmt.Errorf("save report: %w", err)
	}
	path := s.path(r.ID)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("save report %q: %w", r.ID, ErrExists)
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("encode report %q: %w", r.ID, err)
	}
	return atomicfile.Write(path, append(b, '\n'), filePerm)
}

// Get reads one report.
func (s *Store) Get(id string) (*Report, error) {
	b, err := s.read(id)
	if err != nil {
		return nil, err
	}
	return decode(id, b)
}

// Raw returns the report file's bytes as stored, for exporting the raw data
// (R-REPORT-8).
func (s *Store) Raw(id string) ([]byte, error) {
	b, err := s.read(id)
	if err != nil {
		return nil, err
	}
	if _, err := decode(id, b); err != nil {
		return nil, err // never hand out something that is not a valid report
	}
	return b, nil
}

// Delete removes a report.
func (s *Store) Delete(id string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("delete report %q: %w", id, ErrInvalidID)
	}
	if err := os.Remove(s.path(id)); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("delete report %q: %w", id, ErrNotFound)
		}
		return fmt.Errorf("delete report %q: %w", id, err)
	}
	return nil
}

// Problem describes a file in the reports folder that could not be read as a report.
type Problem struct {
	File string
	Err  error
}

// List returns every saved report, newest first. A file that cannot be read does
// not hide the others: it is returned as a problem.
func (s *Store) List() ([]Summary, []Problem, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []Summary{}, nil, nil // nothing saved yet
		}
		return nil, nil, fmt.Errorf("list reports: %w", err)
	}

	out := []Summary{}
	var problems []Problem
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, fileExt) || strings.HasPrefix(name, ".") {
			continue // folders, other files and half-written temporary files are not reports
		}
		id := strings.TrimSuffix(name, fileExt)
		r, err := s.Get(id)
		if err != nil {
			problems = append(problems, Problem{File: name, Err: err})
			continue
		}
		out = append(out, r.Summary())
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].StartedAt.After(out[j].StartedAt)
		}
		return out[i].ID > out[j].ID
	})
	return out, problems, nil
}

func (s *Store) path(id string) string { return filepath.Join(s.dir, id+fileExt) }

func (s *Store) read(id string) ([]byte, error) {
	if !idPattern.MatchString(id) {
		return nil, fmt.Errorf("read report %q: %w", id, ErrInvalidID)
	}
	b, err := os.ReadFile(s.path(id))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("read report %q: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("read report %q: %w", id, err)
	}
	return b, nil
}

// decode reads a report and checks its version. A report from an older schema is
// read as it was written. One from a newer schema is refused, because reading it
// could drop what it says.
func decode(id string, b []byte) (*Report, error) {
	var head struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return nil, fmt.Errorf("report %q is damaged: %s", id, describe(err))
	}
	if head.Version > SchemaVersion {
		return nil, fmt.Errorf("report %q has version %d and this application reads up to %d: %w", id, head.Version, SchemaVersion, ErrTooNew)
	}
	if head.Version < 1 {
		return nil, fmt.Errorf("report %q has no version, so it is not a report this application wrote", id)
	}
	var r Report
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("report %q is damaged: %s", id, describe(err))
	}
	if r.ID != id {
		return nil, fmt.Errorf("report %q says it is %q, so the file was renamed or copied", id, r.ID)
	}
	return &r, nil
}

// describe says what is wrong with a report file without echoing its content, which
// holds a trainee's name and the instructor's notes.
func describe(err error) string {
	var syntax *json.SyntaxError
	var wrongType *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syntax):
		return fmt.Sprintf("syntax error at byte %d", syntax.Offset)
	case errors.As(err, &wrongType):
		return fmt.Sprintf("the field %q has the wrong type", wrongType.Field)
	default:
		return "unreadable"
	}
}
