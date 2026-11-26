package scenario

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
)

// Source says where a scenario came from.
type Source string

const (
	// BuiltIn scenarios are embedded in the executable (R-SCN-4).
	BuiltIn Source = "built-in"
	// User scenarios come from the user's scenario directory (R-SCN-3).
	User Source = "user"
)

// Entry is a scenario in the catalogue.
type Entry struct {
	// Scenario is the loaded and validated scenario.
	Scenario *Scenario
	// Source says whether it is built in or the user's own.
	Source Source
}

// Registry is the catalogue of scenarios. Built-in scenarios are discovered
// from the embedded tree, so adding a scenario folder changes no Go file
// (R-SCN-16).
type Registry struct {
	opts Options

	mu      sync.RWMutex
	entries map[string]Entry
}

// NewRegistry returns an empty registry. opts is passed to Validate for every
// scenario that is loaded.
func NewRegistry(opts Options) *Registry {
	return &Registry{opts: opts, entries: map[string]Entry{}}
}

// Load finds every scenario folder under root in fsys, validates it and adds it
// to the catalogue. A folder is a directory that contains scenario.yaml, and a
// scenario folder is never searched for further scenarios (R-SCN-15).
//
// A scenario that does not load or validate is left out and described in the
// returned *LoadError, and the others are still added, so one broken file does
// not hide the rest of the catalogue. A scenario whose ID is already taken is
// also left out: load built-in scenarios first and they win. A root that does
// not exist adds nothing and is not an error, as for a user who has no
// scenarios of their own.
func (r *Registry) Load(fsys fs.FS, root string, source Source) error {
	var problems []Problem

	err := fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root && errors.Is(err, fs.ErrNotExist) {
				return fs.SkipAll
			}
			problems = append(problems, Problem{File: p, Message: err.Error()})
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if p != root && strings.HasPrefix(d.Name(), ".") {
			return fs.SkipDir
		}
		if _, err := fs.Stat(fsys, path.Join(p, FileName)); err != nil {
			return nil // not a scenario folder: keep looking inside it
		}

		problems = append(problems, r.add(fsys, p, source)...)
		return fs.SkipDir
	})
	if err != nil {
		return fmt.Errorf("search %s for scenarios: %w", root, err)
	}

	if len(problems) == 0 {
		return nil
	}
	return &LoadError{Problems: problems}
}

// add loads, validates and registers one scenario folder.
func (r *Registry) add(fsys fs.FS, dir string, source Source) []Problem {
	sc, err := Load(fsys, dir)
	if err == nil {
		err = sc.Validate(r.opts)
	}
	if err != nil {
		var le *LoadError
		if errors.As(err, &le) {
			return le.Problems
		}
		return []Problem{{File: path.Join(dir, FileName), Message: err.Error()}}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if have, ok := r.entries[sc.ID]; ok {
		return []Problem{{
			File:    path.Join(dir, FileName),
			Line:    sc.Line("id"),
			Field:   "id",
			Message: fmt.Sprintf("%q is already used by the %s scenario in %s", sc.ID, have.Source, have.Scenario.Dir),
		}}
	}
	r.entries[sc.ID] = Entry{Scenario: sc, Source: source}
	return nil
}

// Get returns the scenario with the given ID.
func (r *Registry) Get(id string) (Entry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[id]
	return e, ok
}

// List returns the catalogue ordered by product, then name, then ID.
func (r *Registry) List() []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Entry, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Scenario, out[j].Scenario
		if a.Product != b.Product {
			return a.Product < b.Product
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ID < b.ID
	})
	return out
}
