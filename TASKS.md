# Sev1 Situations — Build Plan and Tasks

The plan for building the project described in [`sev1situations-project-definition.md`](sev1situations-project-definition.md). The definition is the source of truth. This file only sequences the work. Rule IDs (`R-...`) and section numbers refer to the definition.

**Status:** M0, M1 (except W2) and M2 are done, M3 is done, and M4 is next. Done tasks are marked `[x]`.

**How to use this file**
- Work top to bottom. Milestones are ordered by dependency (R-AGENT-6). Tasks inside a milestone can usually be done in the order listed.
- Tick a task only when its **acceptance** line is true and the code, tests and docs for it are on `main`.
- Until 1.0.0, commit straight to `main` in small steps using Conventional Commits (R-CONTRIB-8).
- If a task cannot be done without breaking a rule, stop and flag it (R-AGENT-4).
- Size guide: **S** under half a day, **M** about a day, **L** several days.

---

## Plan at a glance

| Milestone | Goal | Outcome |
|---|---|---|
| **M0** | Foundation | Repo skeleton, tooling, CI, `s1s version` and `s1s prereq` |
| **M1** | Spikes | The riskiest assumptions proved or replaced before building on them |
| **M2** | Runtime and releases | Podman runtime, fake runtime, release download and cache |
| **M3** | Scenario engine | Scenario model, loader, primitives, engine, settings |
| **M4** | Vault product and first slice | Vault Sandbox running end to end through the engine, headless |
| **M5** | Wails shell and frontend | Carbon UI in a native window for every feature, events, terminals, logs |
| **M6** | Reports | Report data, store, HTML renderer, Reports tab |
| **M7** | Vault scenarios | The remaining six Vault scenarios |
| **M8** | Boundary | Boundary product package and the four Boundary scenarios |
| **M9** | Docs site | Hugo site, generator, GitHub Pages |
| **M10** | Hardening and 0.1.0 | Playwright and Vitest suites, manual test pass, release workflow, first release |

**Critical path:** M0 -> M1 -> M2 -> M3 -> M4 -> M5 -> M6. M7 and M8 need M4 and M5. M9 can start once M3 exists, and its scenario pages fill in as scenarios land. The Wails spikes (W1 to W4) must finish before M5 starts, and can run in parallel with M2 to M4.

**First milestone worth showing someone:** end of M5 (a working Vault Sandbox in the app window with terminals and logs).

---

## Decisions to confirm before starting

These affect early tasks. Defaults are shown so work is not blocked.

| # | Decision | Default |
|---|---|---|
| D1 | Default Vault and Boundary versions for scenarios | Newest available from `releases.hashicorp.com` at deploy time, selectable in the Start form. |
| D2 | Terminal transport | Wails events (R-TERM-2). Fallback: a loopback WebSocket with a per-launch token. Settled in spike W2. |
| D3 | YAML library | `go.yaml.in/yaml/v4` (pure Go, strict decoding). Confirmed in spike S4. |
| D4 | Frontend stack | Svelte 5, TypeScript, Vite, `carbon-components-svelte`. No SvelteKit. Confirmed in spike W3, with self-hosted fonts. |
| D5 | Where app data lives | `~/Library/Application Support/Sev1Situations/` with `reports/`, `scenarios/`, `cache/binaries/` and `settings.json`. |
| D6 | Run ID format | Short random hex, for example `a1b2c3`. |
| D7 | Verify the GPG signature of `SHA256SUMS` | **No.** Only the SHA-256 checksum from the published `SHA256SUMS` is checked, so there is no OpenPGP dependency and no key to embed or fetch. |
| D8 | Distribution | Unsigned `.app` in a GitHub release zip, with the `xattr -cr` note in the docs (Section 10). Signing is a roadmap item. |
| D9 | Terminology | The project is Sev1 Situations. The domain object stays "scenario" until you decide otherwise (definition, Section 12). |

---

## M0 — Foundation

| ID | Task | Refs | Size | Acceptance |
|---|---|---|---|---|
| T-001 | [x] Initialise the Go module `github.com/pixambi/sev1situations`, `go.mod` at the current stable Go, `.gitignore`, no LICENSE. | R-LANG-1, R-LANG-6, R-APP-1 | S | `go build ./...` passes on an empty tree. |
| T-002 | [x] Create the directory layout from 3.2 (without `frontend/` yet) with a doc comment (`doc.go`) in every package. | 3.2, R-QUAL-6 | S | `go vet ./...` clean. Every package has a doc comment. |
| T-003 | [x] `cmd/s1s/main.go` and `internal/app`: subcommands `version` and `prereq` using the standard library only (no CLI framework unless justified). | R-APP-1, R-APP-5, R-DEP-1 | M | `s1s version` prints version, commit and date stamped by ldflags. `s1s prereq` reports Podman and Podman machine state. |
| T-004 | [x] `Taskfile.yml`: `build`, `run`, `test`, `lint`, `vuln`, `test:e2e`, `docs`. (`dev` arrives with the shell in M5.) | 10 | S | Each task runs, or fails clearly if its target does not exist yet. |
| T-005 | [x] `golangci-lint` config, small and documented. | R-LANG-4 | S | `task lint` passes. |
| T-006 | [x] CI workflow on a macOS runner: vet, test, lint, `govulncheck`, and `CGO_ENABLED=0` build of the core packages. Dependabot config for Go modules, npm and actions. | 10, R-LANG-2 | M | CI is green on `main`. |
| T-007 | [x] `DEPENDENCIES.md` with the rule for justifying each module and npm package. | R-DEP-1 | S | File exists. |
| T-008 | [x] `README.md` (what it is, install, first run) and a `CONTRIBUTING.md` stub that points at the definition. | R-CONTRIB-5 | S | Both exist and match the definition. |

**M0 done when:** CI is green and `s1s version` and `s1s prereq` work on macOS.

---

## M1 — Spikes

Each spike ends with a short written result in `spikes/` (not under `docs/`, which is published) and a go or no-go. Throwaway code is fine. A spike that fails changes the definition before the milestone that depends on it starts. See [`spikes/README.md`](spikes/README.md).

| ID | Spike | Question to answer | Size | Done when |
|---|---|---|---|---|
| S1 | [x] **Podman REST API on macOS** | Can we reach the `podman machine` socket from Go with `net/http` over a unix socket? Can we create a network and container, exec, copy files, stream logs, and list and remove by label? What is the socket path discovery rule? | M | A small Go program does all of the above against a real machine, and the result lists the exact endpoints used. |
| S2 | [x] **Binaries on the base image** | Do `vault`, `vault+ent`, `boundary`, `boundary+ent` run on Alpine? **Likely problem:** Vault Enterprise's HSM (PKCS#11) builds are separate (`+ent.hsm`) and link to glibc, so the HSM scenario will probably need a glibc image (R-RUN-10). Does SoftHSM work with them? What tools must the image include? | M | A table of product, edition, and the base image that works, with the exceptions recorded. |
| S4 | [x] **YAML library** | Which pure-Go YAML library is maintained, strict (unknown fields rejected) and gives line numbers in errors? | S | Library chosen and justified for `DEPENDENCIES.md`. |
| S7 | [x] **Release listing and verification** | How do we list versions and verify `SHA256SUMS` from `releases.hashicorp.com`? What are the file names for `+ent` and `+ent.hsm`? | S | A program lists versions for Vault and Boundary, downloads one, and verifies the checksum. |
| W1 | [x] **Wails v3 scaffold** | Which template gives Svelte 5 and TypeScript? Does the root `embed.go` layout work with `cmd/s1s`? Does Wails offer single-instance behaviour (R-APP-8)? Can the same executable run CLI subcommands headless? Does `wails3 generate bindings` produce usable types for our service shapes? GoReleaser or a plain workflow for the bundle? | M | A throwaway app opens a window with a Svelte page that calls a Go service and receives an event. Findings recorded for 3.8 and Section 10. |
| W2 | **In-app terminal** | Does ghostty-web run in the macOS system webview (WKWebView) under a strict CSP? Can Wails events carry interactive terminal throughput (typing, `cat` of a large file, resize)? If not, does a token-guarded loopback WebSocket work? | M | A window shows a working, resizable terminal into a container. Findings recorded for R-TERM-1 to R-TERM-7. |
| W3 | [x] **Carbon in Svelte** | Does the current `carbon-components-svelte` release work with Svelte 5 and Vite for every component in 6.5 (header, side nav, tabs, tiles, data table, modal, code snippet, progress indicator)? Do light and dark themes switch? Use the Carbon MCP. | M | A page with those components renders in both themes, with no console errors and a clean axe audit. |
| W4 | [x] **Playwright against Wails** | Can Playwright drive the Wails dev server with the fake runtime selected? How are Wails runtime calls and events reached from a browser, and what does the suite need mocked? | M | One Playwright test starts the app in fake-runtime mode, clicks through a service call and sees an event. |

**M1 done when:** all spikes have a result and the definition is updated for anything that changed. W1 to W4 must be done before M5.

---

## M2 — Runtime and releases

| ID | Task | Refs | Size | Acceptance |
|---|---|---|---|---|
| T-010 | [x] `runtime` package: types and the `Runtime` interface (3.5), including options, results and labels. | R-RUN-3, 3.5 | M | Compiles. Documented. No Podman imports. |
| T-011 | [x] **Fake runtime**: in-memory containers, networks, files, scripted `Exec`, failure injection, log feeds. | 9, R-TEST-2 | M | Unit tests prove it behaves like the interface contract. |
| T-012 | [x] `runtime/podman`: socket discovery and a minimal HTTP client over the unix socket. Clear errors when Podman is missing or the machine is stopped. | R-APP-7, 7 | M | Table tests with a fake server. Manual check on a real machine. |
| T-013 | [x] Podman: networks and containers (create, start, stop, remove), labels, host port publishing on `127.0.0.1`. | R-RUN-4, 7 | M | Integration test (tagged, needs Podman) creates and removes a labelled container and network. |
| T-014 | [x] Podman: `Exec` with captured output and exit code, `Copy` into a container, `Logs` stream. | 7 | M | Integration test passes. |
| T-015 | [x] Podman: `Attach` for terminals (exec with a TTY). | R-TERM-2 | M | Integration test sends input and reads output. |
| T-016 | [x] Podman: `List` and `RemoveRun` by label, idempotent, continues past a failed removal and reports it. | R-STATE-3, 7 | M | Integration test leaves nothing behind, including after a simulated crash. |
| T-017 | [x] Host port allocator (high range, checks the port is free). | 7 | S | Unit tests. |
| T-018 | [x] `release` package: list versions for a product, including `+ent` and `+ent.hsm`. | R-RUN-7, S7 | M | Tests with recorded responses. |
| T-019 | [x] `release`: download, verify the SHA-256 against `SHA256SUMS` (no signature check, decision D7), unzip, cache by product, edition and version. Cache lives in the app data directory. | R-RUN-6, R-RUN-8, R-RUN-9 | L | Tests with a local fake server. A failed checksum aborts and nothing is cached. Second use needs no network. |
| T-020 | [x] Start-up checks and leftover cleanup in `app`: Podman present, machine running, remove labelled leftovers and report them. | F-10, F-11, R-STATE-3 | M | Tested with the fake runtime. |
| T-021 | [x] Image handling: pull on demand with progress, no deletion of images. | R-RUN-9, R-STATE-5 | S | Manual check, unit test for the policy. |

**M2 done:** the deploy flow test (`internal/app/deploy_flow_test.go`) passes against the fake runtime and, with `task test:integration`, against real Podman.

**M2 done when (original wording):** a small test program can deploy a labelled container, exec in it, copy a cached verified binary into it, and tear everything down, all through the `Runtime` interface.

---

## M3 — Scenario engine

| ID | Task | Refs | Size | Acceptance |
|---|---|---|---|---|
| T-030 | [x] `settings` package: file store with `0600` permissions, licenses (masked on read-back), instructor name, `Field` registration for products. | R-SET-1 to R-SET-5, R-QUAL-3 | M | Unit tests, including permissions and masking. |
| T-031 | [x] `scenario` package: Go types for the scenario model (4.4, 5.4) and the YAML loader with strict decoding. | R-SCN-1, R-SCN-13 | L | A valid scenario loads. Unknown fields fail with file and field names. |
| T-032 | [x] Validation: Scenario Standard fields, step IDs unique, referenced files exist, parameter types known, primitives exist, templates parse. | R-SCN-13, 4.5 | L | Table tests for every error. |
| T-033 | [x] Registry: embedded `scenarios/` tree via `go:embed`, user directory loading, duplicate ID handling. | R-SCN-3, R-SCN-16 | M | Adding a folder needs no Go change (test with a fixture scenario). |
| T-034 | [x] `primitive` package: interface, registry, result and error semantics, redaction of secrets in messages and evidence, argument validation at load. | 4.5, R-QUAL-3 | L | Contract tests. |
| T-035 | [x] Templating for arguments (`.Params`, `.Nodes`, `.Creds`, earlier outputs) with validation of unknown values. | 4.5 | M | Tests. |
| T-036 | [x] Generic primitives: `container.*`, `file.*`, `http.request`, `wait.until`, `assert.*`. | 4.5 | L | Each has tests against the fake runtime. |
| T-037 | [x] `engine`: the single run, deploy, step state machine (4.6), no-skip rule, per-step timing, notes and rating, goroutines owned by the run with one cancelable context. | R-SCN-8, R-SCN-9, 4.6 | L | State machine tests for every transition and failure row in 4.6. |
| T-038 | [x] Engine: roll back to a reset point (default redeploy and replay, with override), end early, teardown, events recorded. | R-SCN-6, R-SCN-10, 4.7 | L | Tests with the fake runtime. |
| T-039 | [x] Engine: node actions (stop, start, remove, revive) driven by what the product declares, with removed-node placeholders. | R-VIEW-6 | M | Tests. |
| T-040 | [x] CLI: `s1s scenario list`, `validate`, `new` (scaffold from a template). | R-CONTRIB-6 | M | `new` produces a folder that passes `validate`. |
| T-041 | [x] Engine event stream (state changes, progress, log lines) that the server can subscribe to. | 3.4 | M | Tests. |

**M3 acceptance test:** `TestAScenarioRunsStartToFinishWithRollBackAndEarlyEnd` in `internal/engine` runs it. T-039 to T-041 are done too, so M3 is complete.

**M3 done when (original wording):** a fixture scenario with a start step, a breakage and a verification runs start to finish against the fake runtime in a Go test, including roll back and end early.

---

## M4 — Vault product and the first vertical slice

| ID | Task | Refs | Size | Acceptance |
|---|---|---|---|---|
| T-050 | [x] `product` package: interface, registry (replaces the temporary `builtinProducts` list in `internal/app/scenario_cmd.go`), `ClusterView` and `LogStream` types, settings fields. | 3.5, R-SCOPE-2, R-SCOPE-3 | M | Documented. A fake product exists for engine tests. |
| T-051 | [x] `product/vault`: versions and editions from `release`, license from settings, base image choice from S2. | R-RUN-5 to R-RUN-10 | M | Tests. |
| T-052 | [x] Vault single and multi-node Raft deploy: install binary, render config, start, wait for readiness, `init` (5 shares, threshold 3), unseal, join, audit on. | R-VIEW-3, 7 | L | Integration test brings up 3 nodes with a leader. |
| T-053 | [x] Vault `Describe` (Cluster tab minimum, 5.5) and `LogStreams` (operational, audit). | R-VIEW-7, R-VIEW-8 | M | Tests. |
| T-054 | Vault primitives: `vault.cli`, `status`, `raft-peers`, `expect-no-leader`, `kv-put`, `kv-get`, `unseal`, `init`. | 4.5 | L | Each has tests with scripted exec responses. |
| T-055 | Vault node actions, including **revive** (fresh node that rejoins). | R-VIEW-6, 7 | M | Integration test. |
| T-056 | `scenarios/vault/sandbox/` (5.3.2) including the node-count parameter. | 5.3.2 | M | Loads, validates, and runs through the engine with the real runtime. |
| T-057 | Temporary CLI or test harness to run the sandbox and print state, so the slice can be proven before any UI exists. | R-AGENT-6 | S | Run manually. Everything is destroyed afterwards. |

**M4 done when:** the Vault Sandbox deploys on real Podman through the engine, can be torn down cleanly, and a second run after a forced kill cleans up leftovers.

---

## M5 — Wails shell and frontend

Use the Carbon MCP for every component (R-UI-3). Needs spikes W1 to W4.

| ID | Task | Refs | Size | Acceptance |
|---|---|---|---|---|
| T-060 | Scaffold the Wails v3 shell and the Svelte 5 + TypeScript + Vite frontend (`frontend/`, `internal/gui`, root `embed.go`), `task dev` and `task build`, CSP, offline build, committed generated bindings with a CI check. | R-UI-1 to R-UI-4, R-LANG-3, 3.8 | L | `task build` produces `Sev1 Situations.app` that opens a window with no network access. CI fails if the bindings are stale. |
| T-061 | Services and events skeleton in `internal/gui` (3.4): thin services over the engine, event publishing from the engine event stream, input validation. | R-APP-3, R-QUAL-5, 3.4 | L | Go tests for each service with the fake runtime. The frontend receives `run:state` snapshots. |
| T-062 | App launch: window versus CLI split, single-instance behaviour, start-up checks and leftover cleanup surfaced in the UI, graceful quit with confirmation during a run. | R-APP-2, R-APP-5, R-APP-6, R-APP-8, R-STATE-3 | M | Manual check. Quitting leaves nothing behind. A second launch focuses the first window. |
| T-063 | Scenarios page: grouped catalogue, filters, license-needed state, blocked-while-running notice. | 6.1, 6.6 | M | Vitest component tests. |
| T-064 | Start step form generated from the scenario's parameters, with names and validation. | R-SCN-11 | M | Tests for each parameter type. |
| T-065 | Run page: header, Steps tab, progress indicator, guidance (collapsible), action and verify buttons, red and green status, Continue, notes, rating. | 6.2, 4.2 | L | Tests for each step state. |
| T-066 | Live updates over events: deployment progress, step state, status. | 3.4 | M | Test with a fake engine stream. |
| T-067 | Confirmation modals, error notifications, empty and error states. | 6.3, 6.6 | M | Tests. |
| T-068 | Cluster tab: sections, node cards, masked credentials with copy, node actions. | R-VIEW-1, R-VIEW-5, R-VIEW-6 | L | Tests. |
| T-069 | Logs tab: node and stream selectors, live batched stream, pause and scroll lock, filter by text, regular expression and level with highlighted matches. | R-VIEW-3 | L | Tests, including the line cap. |
| T-070 | Terminals: Go bridge, events or fallback per spike W2, ghostty-web glue, resize, multiple terminals, closed on teardown. | R-TERM-1 to R-TERM-7 | L | Manual check on a running sandbox plus Go and Vitest tests. |
| T-071 | Settings page: licenses (masked), instructor name, theme override. | R-SET-1 to R-SET-5, 6.7 | M | Tests. |
| T-072 | Accessibility pass and light and dark themes. | R-UI-6 | M | Checked with keyboard and an automated axe audit in Playwright. |
| T-073 | Native behaviour: menu, quit confirmation and save dialogs opened from Go. | 6.7, 3.8 | S | Manual check on the packaged app. |

**M5 done when:** the Vault Sandbox runs fully in the app window: pick it, deploy, see the Cluster tab, open a terminal, watch logs, end it, and see everything destroyed.

---

## M6 — Reports

| ID | Task | Refs | Size | Acceptance |
|---|---|---|---|---|
| T-080 | `report` data model with a versioned schema, JSON store under the app data directory, atomic writes. | R-REPORT-1, R-STATE-6 | M | Round-trip tests. |
| T-081 | Engine writes a report at the end of every run, including early end, with events and per-step data. | R-REPORT-5, R-REPORT-6 | M | Tests. |
| T-082 | `report/html` renderer: self-contained, no external requests, credentials never present. | R-REPORT-2, R-REPORT-4 | M | Golden file tests. A test scans output for known secrets. |
| T-083 | Reports tab: list, open, delete with confirmation, export HTML and raw data. | R-REPORT-7, R-REPORT-8 | M | Tests. |

---

## M7 — Vault scenarios

One scenario per task, in this order (simple to complex). Each needs: the folder, step Markdown, config templates, validation passing, a fake-runtime scenario test, a manual run on real Podman, and the generated docs page (R-CONTRIB-4).

| ID | Scenario | Section | New work it needs | Size |
|---|---|---|---|---|
| T-090 | Raft Lost Quorum | 5.3.1 | Kill and remove nodes, `peers.json` flow checks, revive as fresh nodes | L |
| T-091 | Broken KMS (proposed) | 5.3.7 | Transit seal, a KMS Vault, token revoke, `product/vault` seal options | L |
| T-092 | HSM Auto-Unseal Failure | 5.3.6 | `+ent.hsm` binary, glibc image, SoftHSM install and `softhsm.*` primitives | L |
| T-093 | DR Failover and Failback | 5.3.3 | DR replication setup, batch DR token, `vault.replication-status` | L |
| T-094 | DR Break Glass Promotion | 5.3.4 | Config directory loading, `SIGHUP`, `generate-root -dr-token` flow, `vault.*` checks | L |
| T-095 | Broken Performance Replication | 5.3.5 | Two three-node clusters, PR enable, token minting, address override | L |

---

## M8 — Boundary

| ID | Task | Refs | Size | Acceptance |
|---|---|---|---|---|
| T-100 | `product/boundary`: versions and editions, license, base image from S2, `database init`, controllers, load balancer, worker, target host, readiness. | 5.3.8 | L | Integration test brings up the sandbox topology. |
| T-101 | Supporting services: Postgres and HAProxy as third-party images. | R-RUN-5 | M | Tests. |
| T-102 | Boundary `Describe` and `LogStreams` (5.5), primitives (`cli`, `authorize-session`, `session-info`, `target-info`, `recordings-list`). | 4.5, 5.5 | L | Tests. |
| T-103 | `scenarios/boundary/sandbox/` | 5.3.8 | M | Runs end to end. |
| T-104 | Authorize-Session 403 | 5.3.9 | M | Done per R-CONTRIB-4. |
| T-105 | Session Limits | 5.3.10 | M | Done per R-CONTRIB-4. |
| T-106 | `product/minio` and Session Recording Storage (Enterprise only, `bsr` KMS, storage buckets, `minio.mc`) | 5.3.11 | L | Done per R-CONTRIB-4. |

---

## M9 — Docs site

| ID | Task | Refs | Size | Acceptance |
|---|---|---|---|---|
| T-110 | `docs/` Hugo site skeleton and the custom Carbon-styled theme (layouts, shortcodes, light and dark). | R-DOC-1 to R-DOC-6, 8 | L | `hugo` builds with no errors. |
| T-111 | Home and marketing page, Getting started, user docs (UI, run page, Cluster, Logs, terminals, reports, settings, CLI, troubleshooting). | 8 | L | Content matches the shipped behaviour. |
| T-112 | `tools/docgen`: generate scenario pages from the scenario folders. CI checks they are up to date. | R-SCN-14 | M | Changing a scenario without regenerating fails CI. |
| T-113 | Contributing docs: architecture, writing a scenario by hand, adding a primitive, a step kind, a product, the Scenario Standard, the done checklist. | R-CONTRIB-5, R-CONTRIB-9 | L | A new contributor can add a trivial scenario by following it. |
| T-114 | Offline client-side search. | 8 | M | Works without network. |
| T-115 | GitHub Actions workflow deploying `docs/` to GitHub Pages. Content check for secrets and internal hostnames. | R-DOC-7 | M | Site is live. |
| T-116 | Issue template for new scenarios. Contributor AI skills (define, scaffold, build). | R-CONTRIB-3, R-CONTRIB-7 | M | Templates and skills exist and are documented. |

---

## M10 — Hardening and 0.1.0

| ID | Task | Refs | Size | Acceptance |
|---|---|---|---|---|
| T-120 | Playwright setup in `tests/e2e/` (page objects, dev server on a free port, temp data dir, fake runtime, console-error failures) and Vitest setup for the frontend. | R-TEST-1 to R-TEST-5 | L | One command runs both suites. |
| T-121 | Playwright coverage for every UI feature's main path. | R-TEST-4 | L | Suite green in CI. |
| T-122 | Manual end-to-end test of every scenario on real Podman by a maintainer, recorded in the issue for each. Manual smoke test of the packaged `.app`. | R-CONTRIB-4, 9 | L | All eleven scenarios signed off. |
| T-123 | Security pass: CSP, no remote content, no network listener, service input validation, no secrets in logs, reports or docs, `govulncheck` clean. | R-QUAL-3, R-QUAL-5, R-APP-3 | M | Checklist done. |
| T-124 | Release workflow on macOS runners: build the bundles, zip, checksums, publish on tags. | 10 | M | A tagged snapshot produces archives. |
| T-125 | Verify the install path from the release archive on a clean Mac, including the Gatekeeper note in the docs. | 10 | S | Works on a clean machine. |
| T-126 | Cut **0.1.0**. | 10 | S | Release published. |

**After 0.1.0:** switch to the branch rules in 1.12 at 1.0.0, then take items from Section 11 (roadmap).

---

## Cross-cutting rules for every task

- Write the test first or with the code. Fake runtime for unit tests, real Podman only in tagged integration tests.
- Keep dependencies to the ones justified in `DEPENDENCIES.md` (R-DEP-1).
- Every new package has a doc comment (R-QUAL-6). No emojis (R-CONTRIB-10).
- Errors are wrapped with context and shown in plain language (R-QUAL-1). No secret is logged (R-QUAL-3).
- A feature is not done until it is documented (R-DOC-4).
- Anything the app creates carries the run label and is destroyed at the end (R-RUN-4, R-STATE-2).
- The core never imports Wails, and the frontend holds no business logic (R-LANG-2, R-UI-5).

---

## Risks

| Risk | Impact | Mitigation |
|---|---|---|
| Vault Enterprise HSM builds need glibc and a separate download (`+ent.hsm`). | The HSM scenario cannot use the Alpine default. | Spike S2. Per-product image exception (R-RUN-10). Task T-092. |
| Boundary or Vault binaries misbehave on Alpine. | Slower or broken deployments. | Spike S2 before M4. Fall back to a slim glibc image per product. |
| Podman REST API differs between versions on macOS. | Runtime breaks on some machines. | Spike S1. Record the minimum Podman version. |
| Wails v3 is new (stable since October 2026) and its API or templates may shift. | Shell work slips or needs rework. | Spike W1. Keep all Wails code in `internal/gui` (R-LANG-2). Pin the version. |
| Wails events are too slow for interactive terminals. | Terminal feels laggy. | Spike W2. Batch output. Fall back to a token-guarded loopback WebSocket (R-TERM-2). |
| ghostty-web is young and may misbehave in WKWebView. | Terminal work slips. | Spike W2. Fall back to xterm.js (R-TERM-1). |
| `carbon-components-svelte` gaps with Svelte 5. | UI work slips or the look changes. | Spike W3 early. Wrap or hand-build any missing component in Carbon style. |
| Playwright drives a browser, not the native webview. | Native-only bugs escape CI. | Packaged smoke checklist (T-122). Keep native code small (3.8). |
| Unsigned app is blocked by Gatekeeper. | Friction for first-time users. | Document the `xattr` step (D8). Signing is on the roadmap. |
| Multi-node Enterprise scenarios are slow or heavy on a Mac. | Poor instructor experience. | Cache binaries (R-RUN-8). Measure deploy time in M4 and M7. Guide Podman machine sizing in docs. |
| Enterprise licenses are needed to test many scenarios. | Testing is blocked. | Collect test licenses before M7. Keep fake-runtime tests free of licenses. |
| Broken KMS design is unproven. | T-091 may change. | Prototype early in M7. The definition says it can change. |
| Scope creep into roadmap items (drift detection, snapshots, Consul, Nomad, signing). | Delays 0.1.0. | Section 11 items stay out until after 0.1.0 (R-AGENT-3). |

---

## Open items carried from the definition

- Vault Broken KMS design (5.3.7) needs review.
- Licence is undecided (R-SCOPE-6). Choose before opening the repository wider.
- The repository and module path move to the company organisation later.
- YAML field names and primitive arguments are settled while building M3.
- The Wails-specific open questions in definition Section 12 are settled by spikes W1 to W4.

