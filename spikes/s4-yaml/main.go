// Spike S4: which pure-Go YAML library is strict and gives line numbers?
// A nested module so the main go.mod stays clean. Throwaway code.
// Run: cd spikes/s4-yaml && go run .
package main

import (
	"bytes"
	"fmt"
	"strings"

	goccy "github.com/goccy/go-yaml"
	yamlv4 "go.yaml.in/yaml/v4"
)

type Step struct {
	ID   string `yaml:"id"`
	Kind string `yaml:"kind"`
}

type Scenario struct {
	ID    string `yaml:"id"`
	Title string `yaml:"title"`
	Steps []Step `yaml:"steps"`
}

var cases = []struct{ name, doc string }{
	{"valid", "id: vault-broken-kms\ntitle: Broken KMS\nsteps:\n  - id: s1\n    kind: breakage\n"},
	{"unknown field", "id: x\ntitle: T\nstepz:\n  - id: s1\n"},
	{"unknown nested field", "id: x\nsteps:\n  - id: s1\n    kynd: breakage\n"},
	{"wrong type", "id: x\ntitle: [a, b]\n"},
	{"syntax error", "id: x\ntitle: \"unterminated\nsteps: []\n"},
	{"duplicate key", "id: x\nid: y\n"},
	{"tab indent", "id: x\nsteps:\n\t- id: s1\n"},
}

func main() {
	for _, c := range cases {
		fmt.Printf("\n--- %s\n", c.name)

		var a Scenario
		dec := yamlv4.NewDecoder(strings.NewReader(c.doc))
		dec.KnownFields(true)
		fmt.Printf("v4:    %s\n", oneLine(dec.Decode(&a)))

		var b Scenario
		err := goccy.NewDecoder(strings.NewReader(c.doc), goccy.Strict()).Decode(&b)
		fmt.Printf("goccy: %s\n", oneLine(err))
	}

	// Line numbers for errors found after decoding (validation): v4 Node keeps Line and Column.
	fmt.Println("\n--- v4 node positions for post-decode validation")
	doc := "id: x\nsteps:\n  - id: s1\n    kind: breakage\n  - id: s2\n    kind: bogus\n"
	var root yamlv4.Node
	if err := yamlv4.Unmarshal([]byte(doc), &root); err != nil {
		panic(err)
	}
	steps := find(root.Content[0], "steps")
	for _, st := range steps.Content {
		k := find(st, "kind")
		fmt.Printf("step kind %q is at line %d, column %d\n", k.Value, k.Line, k.Column)
	}
}

func find(m *yamlv4.Node, key string) *yamlv4.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func oneLine(err error) string {
	if err == nil {
		return "ok (no error)"
	}
	return strings.ReplaceAll(strings.TrimSpace(bytes.NewBufferString(err.Error()).String()), "\n", " | ")
}
