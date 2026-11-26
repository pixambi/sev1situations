// Package settings reads and writes the application's persisted settings: the
// instructor's name and the values products register, such as licenses
// (R-SET-1 to R-SET-5).
//
// A product adds its settings by registering Fields, so the settings page is
// never edited per product (R-SET-4). Values marked Secret are licenses: they
// are masked on every read the UI can make, never appear in an error or a log
// line, and the file holding them is created with owner-only permissions
// (R-SET-3, R-QUAL-3).
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/pixambi/sev1situations/internal/atomicfile"
)

const (
	fileName      = "settings.json"
	fileVersion   = 1
	maxValueLen   = 8192
	maxNameLen    = 100
	secretMask    = "********"
	dirPerm       = 0o700
	filePerm      = 0o600
	looseFileBits = 0o077
)

// Errors callers can match with errors.Is.
var (
	// ErrUnknownField means no product registered a field with that key.
	ErrUnknownField = errors.New("unknown settings field")
	// ErrInvalidValue means the value is not acceptable for the field.
	ErrInvalidValue = errors.New("invalid settings value")
)

// Field describes one setting a product registers.
type Field struct {
	// Key identifies the field, for example "vault.license". Keys are unique.
	Key string
	// Label is the short name shown on the settings page.
	Label string
	// Help explains what the value is for.
	Help string
	// Secret marks the value as sensitive: it is masked after saving.
	Secret bool
}

// FieldView is a field and whether it has a value, safe to show in the UI.
type FieldView struct {
	Field
	// Set reports whether a value is saved.
	Set bool
	// Display is the saved value, or a mask for a secret, or empty if not set.
	Display string
}

// View is everything the settings page shows. It never holds a secret value.
type View struct {
	// Instructor is the saved instructor name.
	Instructor string
	// Fields lists the registered fields in key order.
	Fields []FieldView
}

// Store holds the settings in memory and on disk.
type Store struct {
	path string

	mu         sync.Mutex
	fields     map[string]Field
	instructor string
	values     map[string]string
}

// stored is the on-disk shape.
type stored struct {
	Version    int               `json:"version"`
	Instructor string            `json:"instructor,omitempty"`
	Values     map[string]string `json:"values,omitempty"`
}

// Open loads the settings from dir, creating the directory if needed, and
// registers the given fields. A missing file is an empty store. A file that is
// readable by other users is tightened to owner-only.
func Open(dir string, fields ...Field) (*Store, error) {
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return nil, fmt.Errorf("create settings directory %s: %w", dir, err)
	}
	s := &Store{
		path:   filepath.Join(dir, fileName),
		fields: map[string]Field{},
		values: map[string]string{},
	}
	for _, f := range fields {
		if err := s.Register(f); err != nil {
			return nil, err
		}
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	info, err := os.Stat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read settings %s: %w", s.path, err)
	}
	if info.Mode().Perm()&looseFileBits != 0 {
		if err := os.Chmod(s.path, filePerm); err != nil {
			return fmt.Errorf("restrict permissions of %s: %w", s.path, err)
		}
	}

	b, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("read settings %s: %w", s.path, err)
	}
	var in stored
	if err := json.Unmarshal(b, &in); err != nil {
		return fmt.Errorf("settings file %s is damaged (%s), fix or remove it", s.path, describeJSONError(err))
	}
	if in.Version > fileVersion {
		return fmt.Errorf("settings file %s was written by a newer version (format %d)", s.path, in.Version)
	}
	s.instructor = in.Instructor
	if in.Values != nil {
		s.values = in.Values
	}
	return nil
}

// Register adds a field. A product calls this for its settings. Registering the
// same key twice is an error.
func (s *Store) Register(f Field) error {
	if f.Key == "" || f.Label == "" {
		return errors.New("register settings field: key and label are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.fields[f.Key]; ok {
		return fmt.Errorf("register settings field: %q is already registered", f.Key)
	}
	s.fields[f.Key] = f
	return nil
}

// Instructor returns the saved instructor name, or "" if none.
func (s *Store) Instructor() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.instructor
}

// SetInstructor saves the instructor name, which pre-fills the Start form (R-SET-5).
func (s *Store) SetInstructor(name string) error {
	name = strings.TrimSpace(name)
	if err := validate(name, maxNameLen); err != nil {
		return fmt.Errorf("instructor name: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.instructor
	s.instructor = name
	if err := s.save(); err != nil {
		s.instructor = previous
		return err
	}
	return nil
}

// Set saves a value for a registered field. An empty value clears it.
// Surrounding whitespace is removed, which makes a pasted license work.
func (s *Store) Set(key, value string) error {
	value = strings.TrimSpace(value)
	if err := validate(value, maxValueLen); err != nil {
		// The error names the field and the rule, never the value.
		return fmt.Errorf("setting %q: %w", key, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.fields[key]; !ok {
		return fmt.Errorf("setting %q: %w", key, ErrUnknownField)
	}

	previous, had := s.values[key]
	if value == "" {
		delete(s.values, key)
	} else {
		s.values[key] = value
	}
	if err := s.save(); err != nil {
		if had {
			s.values[key] = previous
		} else {
			delete(s.values, key)
		}
		return err
	}
	return nil
}

// Get returns the saved value of a registered field, for injecting into a
// deployment that needs it. It is for the core only: the UI reads View.
// The second result reports whether a value is saved.
func (s *Store) Get(key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.fields[key]; !ok {
		return "", false, fmt.Errorf("setting %q: %w", key, ErrUnknownField)
	}
	v, ok := s.values[key]
	return v, ok, nil
}

// View returns what the settings page shows. Secret values are masked.
func (s *Store) View() View {
	s.mu.Lock()
	defer s.mu.Unlock()

	keys := make([]string, 0, len(s.fields))
	for k := range s.fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	v := View{Instructor: s.instructor, Fields: make([]FieldView, 0, len(keys))}
	for _, k := range keys {
		f := s.fields[k]
		val, set := s.values[k]
		fv := FieldView{Field: f, Set: set}
		switch {
		case !set:
		case f.Secret:
			fv.Display = secretMask
		default:
			fv.Display = val
		}
		v.Fields = append(v.Fields, fv)
	}
	return v
}

// save writes the file atomically with owner-only permissions. The caller holds s.mu.
func (s *Store) save() error {
	out := stored{Version: fileVersion, Instructor: s.instructor, Values: s.values}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}

	// CreateTemp inside Write makes the file private, and Write sets the permissions
	// explicitly, because they are a requirement (R-SET-3).
	if err := atomicfile.Write(s.path, b, filePerm); err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	return nil
}

// describeJSONError says what is wrong with a settings file without echoing any
// of its content, which may hold a license.
func describeJSONError(err error) string {
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

// validate rejects values that are too long or contain control characters,
// without ever including the value in the error.
func validate(v string, max int) error {
	if len(v) > max {
		return fmt.Errorf("%w: longer than %d characters", ErrInvalidValue, max)
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return fmt.Errorf("%w: contains a control character such as a line break", ErrInvalidValue)
		}
	}
	return nil
}
