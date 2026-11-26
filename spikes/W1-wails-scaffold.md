# W1: Wails v3 scaffold

**Result: go with changes.** The scaffold, the single executable with headless subcommands, single-instance and the bindings generator all work. The bindings need a few conventions on our side.

Tested 2026-11-26 on macOS (Apple Silicon) with Wails v3.0.0, Go 1.27.4 and Svelte 5. There is no separate spike program: the findings come from the real scaffold in this repository (M0), plus one scratch copy used to try service shapes.

## What was proved

| Question | Answer |
|---|---|
| Which template gives Svelte 5 and TypeScript? | `wails3 init -t svelte` ("Svelte + TypeScript + Vite"). `svelte-js` is the JavaScript one. The installed frontend is Svelte `^5.46`, Vite 8, TypeScript. |
| Does the root `embed.go` layout work with `cmd/s1s`? | Yes. `embed.go` in the module root package embeds `frontend/dist`, and `cmd/s1s` passes `sev1situations.Frontend` to `gui.Run`. `go build ./...` and the bundle build both work. |
| Does Wails offer single-instance behaviour (R-APP-8)? | Yes: `application.Options.SingleInstance` with a `UniqueID` and an `OnSecondInstanceLaunch` callback. Two launches of the built bundle left one process, and the second exited with code 0. The first window being brought forward (`UnMinimise`, `Show`, `Focus`) was not observed, because the test was headless. |
| Can the same executable run CLI subcommands headless? | Yes. `s1s version` and `s1s prereq` run from the same bundle binary and never touch Wails (`internal/app` does not import `internal/gui`). `s1s version` takes 0.04 s. They also work while the window is open, because only `gui.Run` takes the single-instance lock. |
| Does `task dev` still work after the restructure? | Yes: it builds the dev bundle, starts Vite on port 9245, the app connects, and the updater stays off (`version=dev`). |
| GoReleaser or a plain workflow? | Plain workflow. `task release:package` already builds the `.app`, zips it with `ditto` and writes `SHA256SUMS`. GoReleaser would add a tool (R-DEP-3) for what is a few lines of Taskfile. I did not evaluate GoReleaser beyond that. |

### Generated bindings for our service shapes

Tried in a scratch copy: a `RunService` with a struct argument, nested structs, slices, maps, `time.Time`, `time.Duration`, a string enum, a pointer return, `(T, error)` returns and typed events, generated with `wails3 generate bindings -clean=true -ts -i ./cmd/s1s`.

| Go | TypeScript |
|---|---|
| struct | `interface` with the `json` tag names |
| string type with constants | `enum` with a `$zero = ""` member |
| `time.Time` | `string` (ISO 8601) |
| `*time.Time` with `omitempty` | `started?: string \| null` |
| `time.Duration` | `enum Duration` from a generated `time/models.ts` (awkward) |
| `[]T`, `map[string]string` | `T[] \| null`, `{ [_ in string]?: string } \| null` |
| `*T` return | `T \| null` |
| `(T, error)` | `CancellablePromise<T>`; an error rejects the promise |
| `context.Context` first parameter | dropped from the TypeScript signature |
| `application.RegisterEvent[T](name)` | a constant for the event name and a typed payload |

## Findings that change the definition

- **Return empty slices and maps, not nil.** A nil slice or map becomes `null` in JSON and `... | null` in TypeScript. The services should return non-nil collections so the frontend does not need null checks (a rule for `internal/gui`, 3.4).
- **Do not expose `time.Duration` to the frontend.** It produces a generated `Duration` enum. Use `int64` milliseconds or a string in the DTO types (R-UI-5: the frontend renders, it does not compute).
- **Services take a `context.Context` first,** and it is not part of the generated signature, so R-LANG-4 and the typed bindings do not conflict.
- **Use `-ts -i` when generating bindings.** Without `-ts` the generator writes `.js`, which would break R-LANG-7 and the "bindings are committed" check. All commands (`task build`, `task test:e2e`, CI) pass these flags now.
- **The scaffold came with iOS, Android, Linux and Windows build files.** They were removed (R-APP-7). `wails3 generate icons` always writes a Windows icon, so the icon task sends it to `/dev/null`.
- **The cgo binary is also the CLI,** so even `s1s version` loads the cgo runtime. At 0.04 s this is not a problem.

## Not tested

- The window being focused by a second launch (needs a person or UI automation).
- `Quit while a run is active asks for confirmation` (R-APP-6). That belongs to M5.

## Decision and the rule or section it updates

Section 3.8 (bindings) and Section 10 (release) stay as written. The "plain workflow" answer settles the open question in Section 10: use a tag-triggered workflow around `task release:package`.
