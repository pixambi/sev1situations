package scenario

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Problem is one thing wrong in a scenario file, located by file and line.
type Problem struct {
	// File is the path of the file, including the scenario folder.
	File string
	// Line is the 1-based line, or 0 if unknown.
	Line int
	// Field is the path of the field the problem is about, for example
	// "steps[1].kind". It is empty for problems about the file as a whole.
	Field string
	// Message says what is wrong.
	Message string
}

func (p Problem) Error() string {
	where := p.File
	if p.Line > 0 {
		where = fmt.Sprintf("%s:%d", p.File, p.Line)
	}
	if p.Field != "" {
		return fmt.Sprintf("%s: %s: %s", where, p.Field, p.Message)
	}
	return fmt.Sprintf("%s: %s", where, p.Message)
}

// LoadError is returned when a scenario cannot be loaded or does not pass
// validation. It lists every problem found, so a contributor can fix them in one
// pass.
type LoadError struct {
	// Problems lists what is wrong, in file order.
	Problems []Problem
}

func (e *LoadError) Error() string {
	lines := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		lines[i] = p.Error()
	}
	return strings.Join(lines, "\n")
}

// Load reads and decodes the scenario folder dir in fsys. Decoding is strict:
// unknown fields, wrong types, duplicate keys and extra documents are errors.
// Load only decodes. Checking the Scenario Standard and the files a scenario
// refers to is Validate's job.
func Load(fsys fs.FS, dir string) (*Scenario, error) {
	file := path.Join(dir, FileName)
	data, err := fs.ReadFile(fsys, file)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, &LoadError{Problems: []Problem{{File: file, Message: "scenario file not found. A scenario folder needs a " + FileName}}}
		}
		return nil, fmt.Errorf("read %s: %w", file, err)
	}

	var sc Scenario
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&sc); err != nil {
		return nil, &LoadError{Problems: problems(file, err)}
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, &LoadError{Problems: []Problem{{File: file, Message: "contains more than one YAML document"}}}
	}

	sc.Dir = dir
	sc.fsys = fsys
	sc.lines = fieldLines(data)
	return &sc, nil
}

// problems turns a YAML error into located problems.
func problems(file string, err error) []Problem {
	if errors.Is(err, io.EOF) {
		return []Problem{{File: file, Message: "file is empty"}}
	}

	var many *yaml.LoadErrors
	if errors.As(err, &many) {
		out := make([]Problem, 0, len(many.Errors))
		for _, e := range many.Errors {
			out = append(out, Problem{File: file, Line: e.Mark.Line, Message: e.Message})
		}
		return out
	}

	var one *yaml.LoadError
	if errors.As(err, &one) {
		return []Problem{{File: file, Line: one.Mark.Line, Message: one.Message}}
	}
	return []Problem{{File: file, Message: err.Error()}}
}

// fieldLines maps the path of every field in a YAML document to its line, for
// example "steps[1].kind" to 14. It returns an empty map if the document does
// not parse, which Load has already reported.
func fieldLines(data []byte) map[string]int {
	lines := map[string]int{}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil || len(root.Content) == 0 {
		return lines
	}
	walk(root.Content[0], "", lines)
	return lines
}

func walk(n *yaml.Node, prefix string, lines map[string]int) {
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, val := n.Content[i], n.Content[i+1]
			p := key.Value
			if prefix != "" {
				p = prefix + "." + key.Value
			}
			lines[p] = key.Line
			walk(val, p, lines)
		}
	case yaml.SequenceNode:
		for i, item := range n.Content {
			p := fmt.Sprintf("%s[%d]", prefix, i)
			lines[p] = item.Line
			walk(item, p, lines)
		}
	}
}
