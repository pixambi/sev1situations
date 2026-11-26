# S4: YAML library

**Result: go.** Use `go.yaml.in/yaml/v4` (decision D3 confirmed). `github.com/goccy/go-yaml` is the fallback.

Tested 2026-11-26 with Go 1.27.4, `go.yaml.in/yaml/v4` v4.0.0 and `github.com/goccy/go-yaml` v1.19.2. The program is [`s4-yaml/main.go`](s4-yaml/main.go), in its own nested Go module so the main `go.mod` is untouched (`cd spikes/s4-yaml && go run .`).

## What was proved

Both libraries are pure Go (`CGO_ENABLED=0` builds), have no dependencies of their own, and are permissively licensed (v4 Apache-2.0, goccy MIT).

Each was run against the same seven scenario documents:

| Case | v4 | goccy |
|---|---|---|
| Valid document | ok | ok |
| Unknown top-level field | `line 3: field stepz not found in type main.Scenario` | `[3:1] unknown field "stepz"` with a code excerpt |
| Unknown nested field | `line 4: field kynd not found in type main.Step` | `[4:5] unknown field "kynd"` with an excerpt |
| Wrong type | `line 2: cannot construct !!seq into string` | `[2:8] cannot unmarshal []interface {} into ... Scenario.Title of type string` |
| Syntax error (unterminated quote) | `line 4: found unexpected end of stream` | `[2:8] could not find end character of double-quoted text` |
| Duplicate key | `line 2: mapping key "id" already defined at line 1` | `[2:1] ... already defined at [1:1]` |
| Tab indentation | `line 3: found character that cannot start any token` | `[3:1] found character '\t' that cannot start any token` |

Strict decoding in v4 is `dec := yaml.NewDecoder(r); dec.KnownFields(true)`. In goccy it is `goccy.Strict()`.

For errors found **after** decoding (an unknown step kind, a missing reference), v4 keeps `Line` and `Column` on every `yaml.Node`. Decode into a `yaml.Node` first, validate against the node tree, and the error can name the file, the line and the field (R-CONTRIB-6). The test printed `step kind "bogus" is at line 6, column 11`.

## Findings that change the definition

- **v4's syntax errors can point past the real mistake.** The unterminated quote was reported at line 4 (end of stream), not line 2. goccy reported line 2 and column 8, with a source excerpt. The loader should still show the file and the reported line, and should say "near" for syntax errors. Everything else (unknown fields, wrong types, duplicate keys) was precise in both.
- **v4 has no column on decode errors,** only on nodes. That is enough for file and line.
- **Release activity:** v4.0.0 was released 2026-11-04 and v3.0.6 on 2026-10-13 (active). goccy's last release was v1.19.2 on 2026-01-08.

## Decision and the rule or section it updates

D3 is confirmed: `go.yaml.in/yaml/v4`. `DEPENDENCIES.md` moves it to Accepted. If syntax-error positions turn out to matter for contributors writing scenarios by hand, switch the loader to goccy, which is the one module to change (the loader owns all YAML access).
