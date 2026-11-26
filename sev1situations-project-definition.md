# Sev1 Situations — Project Definition

> A from-scratch specification. It defines what to build, how to build it, and the rules the build must follow. It is written to be fed to an AI coding agent as a prompt. It describes the target project only.

**Status:** Planning draft for a Wails v3 desktop application with a Go backend and a Svelte and TypeScript frontend. Every section is written. Spikes W1 to W4 (TASKS.md) confirm the Wails-specific choices before the shell is built. Remaining items are the exact YAML field names and primitive arguments (settled during the build) and the open questions in Section 12.

---

## Table of Contents

1. [Rules & Principles](#1-rules--principles)
2. [Product Overview](#2-product-overview)
3. [Architecture](#3-architecture)
4. [Domain Model](#4-domain-model)
5. [Features & Scenarios](#5-features--scenarios)
6. [Frontend & UX](#6-frontend--ux)
7. [Container Runtime](#7-container-runtime)
8. [Documentation & Marketing Site](#8-documentation--marketing-site)
9. [Testing](#9-testing)
10. [Build, Release & Distribution](#10-build-release--distribution)
11. [Roadmap & Future Goals](#11-roadmap--future-goals)
12. [Open Questions](#12-open-questions)

---

## 1. Rules & Principles

These rules are binding. Every later section and every line of code must comply. Where a rule conflicts with convenience, the rule wins. Rules have stable IDs so they can be cited in prompts and reviews (e.g. "violates R-DEP-2").

### 1.1 Guiding Principle

**Simplicity first.** Choose the simplest design that works. Add complexity only when a concrete, present need justifies it, and record that need where the complexity lives.

### 1.2 Language & Build

| ID | Rule |
|----|------|
| R-LANG-1 | The backend is written in **Go**. |
| R-LANG-2 | The **core is pure Go**. Every package under `internal/` except `internal/gui` builds with `CGO_ENABLED=0`, and every dependency of those packages is pure Go. Only the Wails shell (`internal/gui` and `cmd/s1s`) may use cgo, because Wails uses the system webview through cgo on macOS. The core never imports Wails. |
| R-LANG-3 | The project ships as a **macOS application bundle** (`Sev1 Situations.app`) containing one executable, `s1s`, with the compiled frontend and every static asset embedded with `go:embed`. It needs no runtime files beside it and no Node, Go or browser installed. |
| R-LANG-4 | The code is **idiomatic Go**: follow *Effective Go* and the Go Code Review Comments. This means small interfaces defined at the point of use, accept interfaces and return structs, errors as values wrapped with `%w`, `context.Context` as the first parameter for anything that blocks or does I/O, and no package-level mutable state. It also means table-driven tests, clear package names (no `util`/`common`/`helpers`), `gofmt`/`go vet` clean, and a `golangci-lint` config that is kept small. |
| R-LANG-5 | Prefer the **standard library** over third-party packages. This includes `net/http` (the Podman unix socket and release downloads), `html/template` (the report renderer), `log/slog`, `encoding/json`, `embed`, `os/exec` and `testing`. |
| R-LANG-6 | The module targets the **current stable Go release** and declares it in `go.mod`. |
| R-LANG-7 | The frontend is written in **TypeScript** (strict mode) with **Svelte 5**, built with **Vite**. There are no plain JavaScript source files other than tool configuration. |

### 1.3 Dependencies

| ID | Rule |
|----|------|
| R-DEP-1 | Dependencies are **only the necessary ones**. Every third-party Go module and every npm package that ships in the frontend needs a one-line justification in `DEPENDENCIES.md` saying what it does and why the standard library (or the platform) is not enough. Dev-only npm tools are listed once, by group. |
| R-DEP-2 | Every Go dependency of the core must be **pure Go** (see R-LANG-2). Every dependency, Go or npm, must be actively maintained and permissively licensed. |
| R-DEP-3 | No heavy frameworks beyond the chosen ones (Wails, Svelte, Carbon). No ORM. No state-management or routing library unless a concrete need is recorded. Node, npm and Vite are **build-time and test-time** requirements only (R-UI-4). |
| R-DEP-4 | The project does not shell out to tools that the user has not been told to install. The only required external tool is the container runtime (R-RUN-1). |

### 1.4 Application Shape

| ID | Rule |
|----|------|
| R-APP-1 | The project is named **sev1situations**. The product is a macOS app named **Sev1 Situations** whose executable is **`s1s`**. The repository is `github.com/pixambi/sev1situations` and the Go module path is `github.com/pixambi/sev1situations`. |
| R-APP-2 | Launching the app (double-click, `open`, or `s1s` with no arguments) opens the **main window** on the UI. No other setup step is needed for the first run. The app is a **Wails v3** application: a Go process with a native window that shows a Svelte frontend in the system webview. |
| R-APP-3 | The app starts **no network listener** for its own UI. The frontend and backend talk through Wails services and events (3.4). Published container ports bind to `127.0.0.1` only (Section 7). Any feature that would open a listening socket needs a recorded justification and is loopback-only. |
| R-APP-4 | Ports bound for deployed products are allocated from a high range (e.g. 19000 upward), each checked free first. The app itself uses no fixed port. |
| R-APP-5 | The executable also has **headless CLI subcommands** (`version`, `prereq`, `scenario list`, `scenario validate`, `scenario new`). They never open a window and are thin wrappers over the same internal packages the app uses. There is no second code path. With no subcommand, the executable opens the window. |
| R-APP-6 | Quitting is **graceful**: closing the last window, quitting from the menu, or `SIGINT`/`SIGTERM` stops accepting work, cancels in-flight operations, destroys everything the session created (see 1.5) and leaves no orphaned state. Quitting while a run is active asks for confirmation. |
| R-APP-7 | **macOS is the only supported platform** (Apple Silicon and Intel). Do not add Linux or Windows code paths, build targets or abstractions. Podman on macOS runs through `podman machine`, and the app must detect and explain a missing or stopped machine. |
| R-APP-8 | **Only one instance** of the app runs at a time. A second launch focuses the first instead of starting. Start-up cleanup removes every labelled run (R-STATE-3), so two instances would destroy each other's work. |

### 1.5 State & Persistence

| ID | Rule |
|----|------|
| R-STATE-1 | **All runtime state is ephemeral.** Everything created by one launch of the app (containers, pods, networks, volumes, sessions, tokens, generated config, temp files) exists only for the life of that launch. |
| R-STATE-2 | When the app quits, everything it created is **destroyed**. This covers a normal quit and a signal. |
| R-STATE-3 | A crash or `kill -9` can skip shutdown. On the next start the app **finds and removes anything left over** from a previous launch, identified by the labels in R-RUN-4. |
| R-STATE-4 | Only the following persist across runs, each in a documented location under one app data directory: **reports** (R-REPORT-1); **settings**, which include product licenses and the instructor's name (R-SET-1 to R-SET-5); **user-defined scenarios** (R-SCN-3); and the **release-binary cache** (R-RUN-8). There is no database. |
| R-STATE-5 | Cleanup never deletes anything in R-STATE-4. Cleanup also never deletes container **images**. Image pulls and caching follow normal Podman behaviour. |
| R-STATE-6 | Reports contain no secrets (R-QUAL-3). |

### 1.5.1 Settings

| ID | Rule |
|----|------|
| R-SET-1 | The UI has a **Settings page** where **licenses for each product** (e.g. Vault Enterprise, Boundary Enterprise) are configured and saved. |
| R-SET-2 | Settings **persist across launches of the app**. |
| R-SET-3 | Licenses are secrets (R-QUAL-3). They are masked in the UI after saving, never logged, never written to reports, never baked into an image, and only injected into a deployment when a scenario needs them. The settings file is created with owner-only permissions (`0600`). The macOS Keychain is not used, to keep things simple. |
| R-SET-4 | The settings page is built so adding a product's settings means registering them with the product abstraction (R-SCOPE-2). The page is not edited per product. |
| R-SET-5 | Settings also hold the **instructor's name**, which pre-fills the Start scenario form on later runs. |

### 1.5.2 Reports

| ID | Rule |
|----|------|
| R-REPORT-1 | A report is stored as **structured data** (a versioned, documented schema, e.g. JSON). It is the source of truth. Presentation is derived from it. |
| R-REPORT-2 | **HTML** is the primary rendered format. The renderer is a pure function from report data to output. |
| R-REPORT-3 | The design must allow **other renderers (e.g. PDF)** to be added later without changing the data schema or the code that produces report data. PDF need not ship in the first release. |
| R-REPORT-4 | The HTML report is **self-contained** (inline CSS, no external requests), so it can be opened or shared as a single file. |
| R-REPORT-5 | A report records the **instructor**, the **trainee**, the **scenario** and its parameters, start and end times, and for **each step**: its status, its time frame (start, end, duration), the notes the instructor took and the instructor's **1-5 rating**. Credentials are never included (R-VIEW-5). It also records **events**: roll backs, retries and ending early, each with a time. |
| R-REPORT-6 | A report is **always saved**, including when a scenario is ended early. It records how the run ended (completed or ended early) and the state of every step at that point. |
| R-REPORT-7 | The UI has a **Reports tab** listing all saved reports (scenario, instructor, trainee, date, outcome). Any report can be opened from it and viewed as HTML (R-REPORT-2). |
| R-REPORT-8 | A report can be **deleted** from the Reports tab, after a confirmation that names it. A report can be **exported**: as the self-contained HTML file and as the raw report data (R-REPORT-1). Further export formats arrive with new renderers (R-REPORT-3). |
| R-REPORT-9 | The Reports tab shows a report's HTML **inside the app**, in a sandboxed frame (R-QUAL-5). Export uses a native save dialog. |

### 1.6 Frontend

| ID | Rule |
|----|------|
| R-UI-1 | The UI is a **Svelte 5 and TypeScript** frontend built with Vite and shown in the Wails window. It calls Go through the **generated, typed Wails bindings** and listens to Wails events. It does not call a local HTTP server. |
| R-UI-2 | The UI uses the **Carbon Design System** through **`carbon-components-svelte`** for components, layout, spacing, typography, color tokens and iconography (icons from `carbon-icons-svelte`). |
| R-UI-3 | UI work uses the **Carbon MCP server** to look up components, tokens and usage guidance. Do not guess component names, props or tokens. Look them up. |
| R-UI-4 | Node, npm and Vite are needed to **build** the frontend, never to run the app. The compiled frontend is embedded (R-LANG-3). `package-lock.json` is committed, versions are pinned exactly, and the generated TypeScript bindings are committed and checked in CI (3.8). |
| R-UI-5 | **Logic stays in Go.** The frontend renders state it receives and sends the instructor's intent back. Business rules (the step state machine, verification, reports) live in Go. Frontend state is view state and the latest snapshot from the backend. The frontend contains no Vault- or Boundary-specific code (R-VIEW-4). |
| R-UI-6 | The UI is **accessible** (Carbon's accessibility guidance followed, keyboard navigable, no color-only meaning) and supports Carbon's light and dark themes. |
| R-UI-7 | The UI works **offline**. It loads no assets from a CDN at runtime. |

### 1.6.1 Scenario View: Cluster and Logs

While a scenario is running, the top of the scenario view has tabs, including a **Cluster** tab and a **Logs** tab. Their content is **supplied by the scenario and its product(s)** through the product abstraction. The UI shell contains no Vault- or Boundary-specific code (R-SCOPE-2).

| ID | Rule |
|----|------|
| R-VIEW-1 | The **Cluster** tab is a well-organised display of the deployed environment. It is grouped into sections defined by the scenario (e.g. *Primary cluster*, *Secondary cluster*, *Supporting services*). It shows each node's name and IP address, its status, and the product details the instructor needs, such as the **root token and passwords**. |
| R-VIEW-2 | Each node has a button that opens an **in-app terminal**, attached to that node with the equivalent of `podman exec -it`. Several terminals can be open at once. See R-TERM-1 to R-TERM-7. |
| R-VIEW-3 | The **Logs** tab lets the instructor **select a node** and view its logs, **streamed live**. The kinds of log available (e.g. operational, audit) vary by node and product. **Audit logging is enabled by default** wherever a product supports it. The view can be **filtered** by text, by a regular expression, and by level (warnings and errors, or errors only), with matches highlighted. The filter works on the lines in the page, so it needs nothing from the log source. |
| R-VIEW-4 | Everything on these tabs **varies by scenario and product**. A Vault cluster, a Boundary deployment and a Postgres pod each describe themselves, and the UI renders what they provide. |
| R-VIEW-5 | Credentials such as root tokens and passwords are shown **in this tab only**, to the instructor, and never in reports (R-QUAL-3). |
| R-VIEW-6 | Nodes offer **actions the trainee may ask the instructor to perform**: stop, start, remove and revive (recreate a removed node with its original configuration). A scenario and product declare which actions are available for each node. Removed nodes remain visible as placeholders so they can be revived. |
| R-VIEW-7 | Every product defines the **minimum** the Cluster tab always shows for its deployments (table in 5.5). A scenario can **add** to that minimum (for example replication role, extra credentials, or extra services) but never removes from it. |
| R-VIEW-8 | Every product likewise defines the **minimum log streams** it offers per node. Scenarios can add streams. |
| R-TERM-1 | Terminals use **ghostty-web** (npm package `ghostty-web`, MIT licensed, no runtime dependencies, an xterm.js-compatible API backed by Ghostty's WASM terminal parser) as an npm dependency bundled by Vite. **xterm.js** is the fallback if spike W2 finds ghostty-web unfit in the system webview. The outcome is recorded in `DEPENDENCIES.md`. |
| R-TERM-2 | The Go side bridges each terminal to a container through the Podman API's exec attach stream, so no pty library is needed. Bytes travel between Go and the frontend as **Wails events** keyed by terminal ID, and input and resize go back through service methods. If spike W2 shows events cannot carry interactive throughput, the fallback is a loopback-only WebSocket guarded by a per-launch random token, and R-APP-3 records the exception. |
| R-TERM-3 | Terminals are tied to the run. They are closed when the scenario ends or the app quits. Only the app's own window can open, write to or read from one. |
| R-TERM-4 | A terminal opens **only** into containers created by the current run (R-RUN-4). |
| R-TERM-5 | **Resize and batching.** The frontend reports `{cols, rows}` after each fit (ghostty-web's `FitAddon`) and Go calls the exec resize endpoint. Output is batched on the Go side so a fast `cat` does not flood the event bus. |
| R-TERM-6 | The webview's Content-Security-Policy allows exactly what the WASM terminal needs and nothing more (R-QUAL-5), including `script-src 'self' 'wasm-unsafe-eval'`. Spike W2 records the final policy. |
| R-TERM-7 | The terminal protocol is small and documented: service methods `Open(nodeID)`, `Input(id, data)`, `Resize(id, cols, rows)` and `Close(id)`, and events for output and exit (3.4). Names are indicative and fixed when the terminal package is built. |

### 1.7 Container Runtime

| ID | Rule |
|----|------|
| R-RUN-1 | **Podman** is the container runtime. |
| R-RUN-2 | The app works **rootless** by default. |
| R-RUN-3 | Container operations go through a **small runtime interface** in Go. This keeps the rest of the code testable and keeps the runtime swappable. Podman is the only implementation that ships. |
| R-RUN-4 | The app creates and **labels everything it creates** with a run identifier. It must be able to list, stop and **clean up everything it created**, and it must never touch containers, pods, networks or volumes it did not create. |
| R-RUN-5 | **No HashiCorp-published container images are used, ever.** Each HashiCorp product runs in a container built from a **base Linux image**. The app **downloads the correct release binary from `https://releases.hashicorp.com/`** and installs it into that container. **Third-party images for supporting services** (e.g. Postgres) are fine and used as-is. |
| R-RUN-6 | Downloads are **verified** against the SHA-256 in the published `SHA256SUMS` file before install. The GPG signature on that file is deliberately not checked. A failed verification aborts the deployment with a clear error. |
| R-RUN-7 | Both **community and Enterprise** (`+ent`) builds are supported. Product, edition and version are scenario parameters. Enterprise licenses come from Settings (R-SET-1). |
| R-RUN-8 | Downloaded release binaries are **cached on disk** (R-STATE-4) so each product/edition/version is downloaded once. The cache is safe to delete. |
| R-RUN-9 | **The internet is needed only the first time** a new binary or image is used. After that the cached binary and the locally stored image are used, so a scenario deploys with no network. |
| R-RUN-10 | The **base image is the smallest that gets in the way least**, to make deployments fast. The default is **Alpine**, with only the tools a scenario needs (e.g. a shell, `curl`, CA certificates) and the usual troubleshooting tools. Every product binary must be verified to run on it. If one cannot (e.g. it needs glibc), that product alone uses a slim glibc image, and the exception is recorded in the product's docs. |
| R-RUN-11 | There are **no resource limits** imposed by the app. Scenarios may deploy up to roughly 7–8 pods or containers (e.g. a 3-node cluster plus supporting services). |

### 1.8 Product Scope

| ID | Rule |
|----|------|
| R-SCOPE-1 | The first-class focus is **HashiCorp Vault** and **HashiCorp Boundary**. Everything in the first release serves these two products. |
| R-SCOPE-2 | The design is **expandable to other products** (e.g. Consul, Nomad, Terraform tooling, and non-HashiCorp products). Product-specific knowledge lives behind a clear **product abstraction**. The core, the UI shell and the runtime layer contain no Vault- or Boundary-specific code. |
| R-SCOPE-3 | Adding a new product must be possible by adding a package and registering it. It must not require edits across the codebase. |
| R-SCOPE-4 | Do not build extension points beyond what R-SCOPE-2 and R-SCOPE-3 need. Do not build a plugin system until a second real product demands one. |
| R-SCOPE-5 | The first release covers **Vault and Boundary** only, with the scenarios in Section 5.2. **Consul and Nomad** are expandable future products (Section 11). Products whose binaries are not on `releases.hashicorp.com` (user-supplied binaries) are **out of scope** and the first release has no code for them. |
| R-SCOPE-6 | The project is a **company project** that lives in a company GitHub organisation. The repository is **access-controlled**: it is shared with the people who have been given access, and it is expected to be opened up to more people later. Distribution is through **GitHub releases** (download the app archive), which works for anyone with repository access. No licence is set for now. One must be chosen before the repository is opened beyond its current audience. |

### 1.8.1 Scenarios

| ID | Rule |
|----|------|
| R-SCN-1 | Scenarios are **declarative YAML files**: metadata, parameters, deployment topology, and steps with their text. YAML needs a pure-Go parser, which must be justified under R-DEP-1. |
| R-SCN-2 | What a step *does* (inject a breakage, verify a fix, reset) is expressed with a **fixed library of built-in primitives written in Go**, such as running a command in a container, making an HTTP/API call, or checking a response. YAML scenarios compose these primitives. They do not embed scripting. |
| R-SCN-3 | **Users can add their own scenarios** by placing YAML files in a documented directory (R-STATE-4). They are validated on load, and errors name the file and field. |
| R-SCN-4 | **Built-in scenarios** are embedded in the binary (R-LANG-3). If a scenario needs behaviour the primitives cannot express, it may use a Go-defined action registered by name. User-defined scenarios use only the primitives. |
| R-SCN-5 | The primitive library is the **extension point** for scenarios. Adding a primitive is a Go change with tests and documentation. |
| R-SCN-6 | Scenarios define **reset points** (checkpoints). The instructor can roll back to one to retry from there. By default a roll back destroys the environment, redeploys it and replays the actions up to the reset point. A scenario may override this with its own reset primitives (4.7). |
| R-SCN-7 | Steps carry **written guidance** for the instructor: a briefing, hints, and the solution and links to documentation. These are text only. There is no interactive hint system and no scoring. The guidance helps the instructor guide the trainee. |
| R-SCN-8 | **Only one scenario runs at a time.** There is no concurrency between scenarios. |
| R-SCN-9 | The instructor **can never skip a step**. The only ways to leave the normal path are rolling back to a reset point (if the scenario allows it) or ending the scenario early (R-SCN-10). |
| R-SCN-10 | **Ending a scenario early** destroys everything the run created, saves the report, and returns to scenario selection. |
| R-SCN-11 | **Every scenario begins with a "Start scenario" step.** The step is **implicit**: the engine builds it from the scenario's `parameters` and `topology`. It shows the parameters the scenario allows to be configured, which differ per scenario, plus the instructor's and trainee's names. Many scenarios expose only a **drop-down for the product version** (and edition). Each parameter has a type, a label, help text, a default and its allowed values. Submitting the step starts the automated deployment. |
| R-SCN-12 | **Step kinds are added when scenarios need them.** The first release defines only *breakage* and *verification* (4.2). Do not build speculative step kinds. |
| R-SCN-13 | Scenario metadata includes the fields in the **Scenario Standard** (5.4): ID, name, products and editions, difficulty, estimated time, tags, learning objectives, prerequisites, parameters, initial state, failure created, steps, success criteria, pitfalls and references. Validation rejects a scenario missing any of them. |
| R-SCN-14 | **Scenario documentation has one source.** The docs-site page for a scenario is generated from its folder (metadata plus step Markdown). Nobody hand-writes a second copy. |
| R-SCN-15 | A scenario is a **self-contained folder** (3.6). It never reaches into another scenario's files. Shared behaviour goes into a primitive or a product package. |
| R-SCN-16 | **Adding a scenario changes no existing Go file.** Built-in scenarios are discovered from the embedded `scenarios/` tree. |
| R-SCN-17 | A scenario may be **open-ended** (a sandbox): it has no breakage or verification steps and finishes when the instructor ends it. The report still records it (R-REPORT-6). |

### 1.9 Documentation & Marketing Site

| ID | Rule |
|----|------|
| R-DOC-1 | The repo has a top-level **`docs/`** folder that contains a **static Hugo site**. |
| R-DOC-2 | That one site serves **three purposes**: user documentation, contributor documentation, and the project's marketing/landing site. |
| R-DOC-3 | The site is **fully static**. It has no server-side runtime, builds with a single `hugo` command, and deploys to any static host. |
| R-DOC-4 | Docs are **part of the definition of done**. A feature is not complete until it is documented. |
| R-DOC-5 | Hugo is a **docs-only tool**. It is not a dependency of the Go build. |
| R-DOC-6 | The site uses a **custom Hugo theme styled to match Carbon** (tokens, type, spacing, light/dark). It does not use a third-party theme. Use the Carbon MCP for design guidance (R-UI-3). |
| R-DOC-7 | The site is hosted on **GitHub Pages**, built and deployed from `docs/` by a GitHub Actions workflow on changes to `main`. The site may be publicly visible, so it must contain no secrets, internal hostnames, customer data or license keys. |

### 1.10 Testing

| ID | Rule |
|----|------|
| R-TEST-1 | **Playwright** is used for end-to-end testing of the frontend, served by the Wails dev server against a fake runtime (Section 9). **Vitest** tests Svelte components and stores. Node, Playwright and Vitest are build-time and test-time tools only (R-DEP-3). |
| R-TEST-2 | Go code has **unit tests** (table-driven where it fits) using the standard `testing` package. Container-runtime code is tested through its interface with fakes. |
| R-TEST-3 | The test suite must run **locally with one command** and in CI. |
| R-TEST-4 | Every user-facing feature has at least one Playwright test covering its main path. |
| R-TEST-5 | Tests must be **deterministic**: no sleeps for synchronization (use Playwright auto-waiting and explicit conditions), and no dependence on test order. |

### 1.11 Quality, Security & Operations

| ID | Rule |
|----|------|
| R-QUAL-1 | Errors are **handled, wrapped with context and surfaced** to the user in plain language. They are not swallowed or only logged. |
| R-QUAL-2 | Logging uses `log/slog` with structured fields. |
| R-QUAL-3 | **No secret is ever hard-coded or logged.** Any token or secret the app handles is treated as sensitive in logs, UI and stored state. |
| R-QUAL-4 | The app is a **local training tool.** Its defaults assume a single trusted user, the **instructor**, on their own machine. The trainee never uses the app (see Section 2). Anything that weakens that assumption (a listening socket, shared use) must be opt-in and clearly warned. |
| R-QUAL-5 | The webview loads **no remote content** and runs under a strict `Content-Security-Policy`. Only the Go service methods the frontend needs are exposed, and each validates its input. HTML built from data (for example the HTML report) is produced by escaping templates or Svelte, or shown in a sandboxed frame. Secrets cross the Go to frontend boundary only when the instructor asks to see them. |
| R-QUAL-6 | Every package has a **single clear responsibility** and a doc comment explaining it. |

### 1.12 Contribution & Maintainability

The project must be **easy to maintain, extend and contribute to**. These rules protect that.

| ID | Rule |
|----|------|
| R-CONTRIB-1 | **Branch names:** scenarios use `scenario/<product>-<name>` (for example `scenario/vault-broken-kms`). Core changes use `feat/<description>`. |
| R-CONTRIB-2 | **One scenario per branch.** A scenario branch contains only scenario files, never core changes (R-SCN-16). If a scenario needs new core behaviour, that work merges first through a `feat/` branch. |
| R-CONTRIB-3 | Every scenario has an **issue** (from an issue template) that defines it. |
| R-CONTRIB-4 | A scenario is **done** only when it has: a well-defined issue, the final scenario, a **manual end-to-end test by a maintainer** on real Podman, and its generated docs page (R-SCN-14). |
| R-CONTRIB-5 | A contributor can add a scenario **without reading the Go core**: the Contributing docs give a step-by-step guide for writing one by hand, with no AI tooling required, covering the folder, the files, validation and testing. |
| R-CONTRIB-6 | `s1s scenario new` **scaffolds** a scenario folder, and `s1s scenario validate` checks it, giving errors that name the file and field. |
| R-CONTRIB-7 | The repo ships contributor tooling for AI-assisted work: skills to define a scenario, scaffold and build it, create the branch, open the issue and open the PRs. The tooling follows the same rules as people do. |
| R-CONTRIB-8 | Commits follow **Conventional Commits**. |
| R-CONTRIB-9 | Every package has a doc comment (R-QUAL-6), and the Contributing docs include an architecture overview matching Section 3, kept current with the code. |
| R-CONTRIB-10 | No emojis in code, comments, documentation or commit messages unless explicitly asked for. |

### 1.13 Working Agreement for the AI Agent Building This

| ID | Rule |
|----|------|
| R-AGENT-1 | **Read this file first**, then plan, then build. If a requirement is ambiguous, ask. Do not guess. |
| R-AGENT-2 | Build in **small, reviewable steps**. Each step compiles, passes its tests and leaves the repo working. |
| R-AGENT-3 | Do **not add features, abstractions or dependencies** that this file does not call for. |
| R-AGENT-4 | When a rule here cannot be met, **stop and flag it**. Do not work around it silently. |
| R-AGENT-5 | Use the **Carbon MCP** for all Carbon work (R-UI-3). |
| R-AGENT-6 | Build **in this order**: runtime and release layers, scenario model and engine, one end-to-end slice (Vault sandbox, driven by a headless test harness), the Wails spikes (W1 to W4), the Wails shell and frontend, then the remaining scenarios. Validate each layer with tests before building on it. |

---

## 2. Product Overview

**Sev1 Situations** (`s1s`) is a local training tool for role-played severity-1 incident calls. An experienced person runs real HashiCorp products (first Vault and Boundary) in containers on their own Mac and plays a customer whose environment has a problem. A trainee joins a call and helps them troubleshoot and fix it. The goal is that the trainee gains the experience and knowledge to handle Sev1 situations and to take part in real customer scenarios.

### 2.1 The problem

Support and solutions engineers learn to handle Sev1 incidents mostly on real customer calls, where mistakes are costly and the same situations are rare and unpredictable. Reproducing a realistic failure normally means building an environment by hand, which takes time, differs between people and is hard to repeat. There is no consistent way to practise the diagnosis and recovery of a broken, production-like deployment with a person playing the customer.

### 2.2 Users and roles

| Role | Who | How they use it |
|---|---|---|
| **Instructor** | An experienced engineer who runs a training session. | Opens the app, controls the UI, role-plays the customer, makes the changes the trainee asks for, triggers verifications, takes notes, rates steps and produces the report. The only user of the app. |
| **Trainee** | An engineer building Sev1 experience. | Joins a call, cannot see the instructor's screen, asks questions and tells the instructor what to check and change, as on a real customer call. Never uses the app. |
| **Contributor** | A person who builds or improves scenarios and the tool. | Works in the repository, following the contribution rules (1.12). |

### 2.3 Core value

- **Realism.** Real products, real binaries and realistic failures, not simulations.
- **Repeatability.** Every run starts from an automated, identical deployment.
- **Low friction.** One app, one window, and Podman. No accounts, services or cleanup chores.
- **Guided but human.** Written guidance helps the instructor coach, while the trainee practises asking the right questions of a person.
- **Evidence.** Each run produces a report with times, notes and ratings per step.
- **Easy to extend.** New scenarios and products are added without changing the core (1.12, 3.7).

### 2.4 How a session works

1. The instructor opens the app. The main window shows the scenario catalogue.
2. They pick a scenario, enter the instructor and trainee names, choose parameters (usually a version) and start. The environment deploys automatically.
3. The trainee joins the call. The instructor presents the customer's problem, using the step guidance.
4. A breakage step causes the failure. The trainee diagnoses by directing the instructor, who uses terminals, logs and the Cluster tab.
5. The instructor presses the verification button. Red means still broken. Green enables Continue, and the next step begins.
6. The instructor records notes and a rating per step. Times are recorded automatically.
7. The scenario ends when the final step completes, or early if the instructor ends it. Everything created is destroyed, and the report is saved.
8. The report can be opened, exported or deleted from the Reports tab.

### 2.5 Principles

- **The project name is not a credential.** There is no formal certification process, exam, badge or issued certificate. The aim is readiness: a trainee who has worked through scenarios has gained experience and knowledge for Sev1 situations and for taking part in real customer scenarios.
- **The app is built for the instructor.** The trainee's experience comes entirely from the call.
- **Ephemeral by default.** Persistent data is limited to what R-STATE-4 lists. Everything else is thrown away when the server stops.
- **Simplicity first** (1.1).

### 2.6 Out of scope

- Windows and Linux (R-APP-7).
- Several users, remote access or hosting the app for others (R-QUAL-4).
- Several scenarios at once (R-SCN-8).
- Skipping steps, scoring, grading or pass and fail marks.
- Formal certification, badges or issued certificates.
- Simulating products. Real binaries are always used.
- Products other than Vault and Boundary in the first release (R-SCOPE-5).
- User-supplied or early-access product binaries in the first release.
- Cloud deployments. Everything runs in local containers.

## 3. Architecture

### 3.1 Principles

1. **One generic engine.** A single scenario engine runs every scenario. The server, the UI and the runtime layer contain **no scenario-specific code**.
2. **Scenarios are data.** A scenario is a folder of YAML, Markdown and fixtures (R-SCN-1). Adding one touches no core package (R-SCN-16).
3. **Products and the runtime sit behind interfaces.** Product knowledge lives in `product/<name>` packages. Container operations live behind the runtime interface (R-RUN-3).
4. **Dependencies point inward.** `engine` knows interfaces, never concrete products or Podman. `server` knows `engine`, never scenarios. Wiring happens in one place (`app`).
5. **Everything the instructor sees is derived from run state.** There is one in-memory run (R-SCN-8) and the UI is a rendering of it.

### 3.2 Repository layout

```
cmd/s1s/                  main: calls app.Run (CLI subcommands, or the window)
embed.go                  go:embed of frontend/dist (root package)
internal/
  app/                    wiring, CLI subcommands, startup checks
  gui/                    Wails shell: application, window, services, events (the only cgo)
  engine/                 run lifecycle, step state machine, rollback, teardown
  scenario/               model, YAML loader, validation, registry
  primitive/              built-in step primitives (exec, http, wait, ...) and registry
  product/                product interface and registry
    vault/                Vault and Vault Enterprise knowledge
    boundary/             Boundary and Boundary Enterprise knowledge
    postgres/ minio/ ...  supporting services (third-party images)
  deploy/                 turns a scenario's topology into an environment through the products
  runtime/                runtime interface and shared types
    podman/               Podman implementation (REST API over the unix socket)
  release/                download, verify and cache release binaries
  settings/               persisted settings and licenses
  report/                 report data model, store, renderers
    html/                 HTML renderer
  logstream/              live log sources
  terminal/               container exec stream to byte channels
frontend/                 Svelte + TypeScript app (Vite) using Carbon components
  src/                    components, stores, views
  bindings/               generated Wails TypeScript bindings (committed)
  dist/                   build output, embedded, not committed
scenarios/                embedded built-in scenarios, one folder each (see 3.6)
docs/                     Hugo site (Section 8)
tests/e2e/                Playwright tests (Section 9)
tools/                    developer tooling (e.g. docs generator)
```

### 3.3 Package responsibilities

| Package | Responsibility | Depends on |
|---|---|---|
| `app` | Parse the command line, run start-up checks, build and connect the components, then run a CLI subcommand or start the Wails application. | all |
| `gui` | The Wails shell: application, window, **services** (thin: map frontend calls to engine calls) and **events** (publish engine events to the frontend). No business logic. The only package that imports Wails. | `engine`, `report`, `settings`, `terminal`, `logstream` |
| `engine` | Own the single run: deploy, step state, verification, reset, teardown, timing. | `scenario`, `primitive`, `product`, `runtime`, `report` |
| `scenario` | Load and validate scenario folders and expose a catalogue. | none (pure data) |
| `primitive` | Define the primitive interface and registry, and implement the generic primitives (container, file, http, wait, assert). | `runtime` |
| `product` | Define what a product must provide (R-SCOPE-2) and register products. Product packages (`product/vault`, `product/boundary`) also supply their own primitives. | `runtime`, `primitive`, `settings` |
| `deploy` | Build a scenario's clusters and services by asking each product, choose versions and editions, and route revive and restart to the node's product. Implements the engine's deployer. | `engine`, `product`, `release`, `runtime` |
| `runtime` | Describe containers, networks, execs and logs as Go types and an interface. | none |
| `runtime/podman` | Implement the runtime over the Podman API. | stdlib |
| `release` | Fetch, verify (R-RUN-6) and cache binaries. | stdlib |
| `settings` | Read and write settings and licenses (R-SET-1 to R-SET-5). | stdlib |
| `report` | Define the report data and store, and render it. | none |
| `logstream` | Open live log sources for containers and files and fan them out to viewers. | `runtime` |
| `terminal` | Bridge a container exec stream to input and output byte channels. | `runtime` |

### 3.4 Calls, events and state

- The backend holds the **one run** in the engine (R-STATE-1). The frontend holds no authoritative state. It renders the snapshots and events Go sends and sends the instructor's intent back (R-UI-5).
- Long operations (deploying, applying a breakage, resetting, teardown) run as **goroutines owned by the run**. They share one `context.Context` that is cancelled on teardown, so ending a scenario stops in-flight work (R-APP-6).
- The frontend calls Go through **services**: Go structs registered with the Wails application, whose exported methods become typed TypeScript functions through the generated bindings. Go pushes progress to the frontend with **events**. Service methods return plain data, never HTML (the HTML report is the one exception, R-REPORT-9).
- Services are thin. Each method validates its input and calls the engine or another core package.

| Service | Methods (indicative) |
|---|---|
| `ScenarioService` | `List`, `Get(id)` |
| `RunService` | `Start(form)`, `Current`, `RunAction(stepID)`, `Verify(stepID)`, `Continue(stepID)`, `SaveNotes(stepID, text)`, `SetRating(stepID, n)`, `Rollback`, `End`, `NodeAction(nodeID, action)` |
| `LogService` | `Open(nodeID, stream)`, `Close(streamID)` |
| `TerminalService` | `Open(nodeID)`, `Input(id, data)`, `Resize(id, cols, rows)`, `Close(id)` |
| `ReportService` | `List`, `Get(id)`, `Delete(id)`, `Export(id, format)` (native save dialog) |
| `SettingsService` | `Get`, `Save` (licenses are write-only once saved) |
| `SystemService` | `Prereq` (Podman and machine state) |

| Event | Payload |
|---|---|
| `run:state` | Snapshot of the run: steps, states, timings, nodes and cluster view. |
| `run:progress` | Deployment and action progress lines. |
| `log:<streamID>` | Batched log lines. |
| `terminal:out:<id>`, `terminal:exit:<id>` | Terminal output bytes, and exit. |
| `system:prereq` | Podman state changes. |

Service and event names are indicative and are fixed when `internal/gui` is built.

### 3.5 Interfaces (shapes, not final signatures)

```go
// product: what a product must provide.
type Product interface {
    Name() string
    Versions(ctx context.Context) ([]Version, error)      // from releases
    Deploy(ctx context.Context, rt runtime.Runtime, spec DeploySpec) (Deployment, error)
    Describe(d Deployment) ClusterView                     // feeds the Cluster tab
    LogStreams(n Node) []LogStream                         // feeds the Logs tab
    Primitives() []primitive.Primitive                     // product-specific verbs
    SettingsFields() []settings.Field                      // e.g. the license
}

// runtime: container operations.
type Runtime interface {
    CreateNetwork(ctx context.Context, o NetworkOptions) (Network, error)
    StartContainer(ctx context.Context, o ContainerOptions) (Container, error)
    Exec(ctx context.Context, id string, cmd []string, o ExecOptions) (ExecResult, error)
    Attach(ctx context.Context, id string, cmd []string) (io.ReadWriteCloser, error) // terminals
    Logs(ctx context.Context, id string, o LogOptions) (io.ReadCloser, error)
    Copy(ctx context.Context, id, dst string, src io.Reader) error
    Stop(ctx context.Context, id string) error
    Remove(ctx context.Context, id string) error
    List(ctx context.Context, runID string) ([]Container, error)
    RemoveRun(ctx context.Context, runID string) error     // cleanup by label (R-STATE-3)
}

// primitive: one named step building block.
type Primitive interface {
    Name() string
    Run(ctx context.Context, env Env, args map[string]any) (Result, error)
}
```

### 3.6 Scenario folder (self-contained)

```
scenarios/vault/raft-lost-quorum/
  scenario.yaml           metadata, parameters, topology, steps (Section 4.4)
  steps/                  Markdown text for each step: briefing, hints, solution
    01-break.md
  templates/              config templates (e.g. vault.hcl.tmpl)
  fixtures/               files copied into containers
  scenario_test.go        optional: scenario-specific test
```

The built-in `scenarios/` tree is embedded with `go:embed`. User scenarios have the same layout in the user scenario directory (R-SCN-3).

### 3.6.1 Node and credential names

The first cluster in a topology names its nodes `node-1`, `node-2` and so on. Nodes of later clusters and services are named `<name>-node-1` (for example `dr-secondary-node-1` and `postgres-node-1`), so names are unique across a scenario. Credentials are keyed by the cluster or service name, so `{{ .Creds.primary.rootToken }}` and `{{ .Creds.dr_secondary.rootToken }}` never collide. Inside a template a dash in a name is written as an underscore, because Go templates cannot parse a dash (`dr-secondary` is `dr_secondary` and `node-1` is `node_1`). A node's `Cluster` is the name from the topology, and its Cluster tab section is that name as a title (`primary` becomes "Primary cluster").

### 3.7 Extending the project

| To add | Do this | Core changes |
|---|---|---|
| A scenario | Add a folder under `scenarios/`, write its YAML and Markdown, run `s1s scenario validate`. | None |
| A primitive | Add a file in `internal/primitive` with tests and a docs entry, register it. | Small, isolated |
| A step kind | Add the kind in `scenario` and `engine`, with a template fragment and docs. | Yes (reviewed as a `feat/` change) |
| A product | Add `internal/product/<name>` implementing `Product` and register it. | None outside the new package and its registration line |
| A renderer (e.g. PDF) | Add a package under `report/` that renders report data. | None |

### 3.8 Wails and frontend conventions

- **Scaffold.** Start from the Wails v3 Svelte TypeScript template (`wails3 init`). The exact template name and layout are confirmed in spike W1. Wails v3.0.0 is the stable release.
- **Svelte 5 and Vite, no SvelteKit.** One window and no URL routing. Tabs and pages are view state.
- **Dev loop.** `task dev` runs `wails3 dev`: Go rebuilds and the Vite dev server hot-reloads the frontend.
- **Bindings.** `wails3 generate bindings` writes TypeScript bindings into `frontend/bindings/`. They are committed. CI regenerates them and fails on any difference.
- **Build.** `task build` runs `npm ci`, the Vite build and the Wails packaging step, producing `Sev1 Situations.app`. `frontend/dist/` is embedded by the root `embed.go`.
- **Window.** One main window with a sensible minimum size, a native macOS menu (About, Settings, Quit) and a confirmation when quitting during a run.
- **Native dialogs** (file save for exports, quit confirmation) are opened by Go.
- **The core never imports Wails** (R-LANG-2). `internal/gui` adapts the core to Wails.
- **Offline.** Vite bundles every asset, font and icon (R-UI-7).

## 4. Domain Model

### 4.1 Scenario

A **scenario** is a defined set of:

1. **Configurable parameters** — e.g. product, edition (community/Enterprise), version, node count, which supporting services to deploy.
2. **An automated deployment** — builds the environment from the parameters, with no manual steps.
3. **An ordered series of steps** — changes and verifications performed against that deployment as the trainee works.

Scenarios cover a range of steps, breakages and configurations across products.

**Worked example (Vault):** a scenario for a broken database secrets engine plugin.
- *Deployment:* a 3-node Vault Enterprise cluster, plus a pod running Postgres. The database is configured and set up in Vault through the database secrets engine.
- *Step — break:* the connection between Vault and the database is broken.
- *Step — diagnose:* the trainee tells the instructor what to check and what configuration to change, and the instructor carries it out in the environment.
- *Step — verify:* the instructor presses a button that verifies the connection works. Once it passes, the step can be completed and the scenario moves to the next step.
- *Completion:* after the final step, the scenario is complete.

### 4.2 Step

Every step has:
- A title and written **guidance for the instructor**: briefing, hints, solution and docs links (R-SCN-7).
- A **notes** section where the instructor records notes against the step, for example how the trainee approached the problem. Notes appear in the report with the step's time frame (R-REPORT-5).
- Automatically recorded times (start, end, duration).
- An optional **rating** from 1 to 5 set by the instructor, recorded in the report (R-REPORT-5).
- A state: `locked`, `active`, `applying`, `applied` or `complete` (4.6).
- An optional **reset point** flag (R-SCN-6).

**Step kinds.** The first release has three kinds. More are added when scenarios need them (R-SCN-12).

| Kind | What the instructor sees | Behaviour |
|------|--------------------------|-----------|
| **Start** | The parameter form (R-SCN-11) and a **Deploy** button, then live deployment progress. | Implicit first step of every scenario. Submitting it deploys the environment. It is complete when the deployment is healthy. |
| **Breakage** | A button to apply the breaking change. | Pressing it runs the scenario's action against the deployment, causing the breakage. A verification then confirms the environment is in the expected broken state. Only once that is verified does **Continue** become clickable. |
| **Verification** | A button to check the fix. | Pressing it runs the verification. The result is shown as **red** ("still broken") or **green** ("please continue"). Only green enables **Continue**. The check can be run as often as needed. |

A step is **never skippable**. Steps proceed strictly in order, and a step's **Continue** is enabled only by a successful verification (R-SCN-9).

### 4.3 Scenario run

- **One scenario runs at a time** (R-SCN-8).
- A run begins at the scenario's **Start scenario** step (R-SCN-11), where the instructor enters the **instructor's name** and the **trainee's name** and sets the scenario's parameters. Starting then triggers the automated deployment.
- During a run the instructor can:
  - **Roll back** to a reset point, if the scenario defines one (R-SCN-6).
  - **End the scenario early.** Ending early destroys everything the run created (R-STATE-2), saves the report (R-REPORT-6), and returns the instructor to scenario selection, ready to start another.
- A run finishes when the final step completes. Its report is always saved (R-REPORT-6).

### 4.4 Scenario file (sketch)

Illustrative only. The final schema is defined when the `scenario` package is built, and must cover the Scenario Standard (5.4). The **Start** step is implicit (R-SCN-11), so it is not listed under `steps`. The scenario-level `guide` for each step is a Markdown file in `steps/`.

```yaml
id: vault-raft-lost-quorum
name: Raft Lost Quorum
summary: Recover a Vault cluster that has lost Raft quorum.
product: vault
editions: [community, enterprise]
difficulty: intermediate
estimatedMinutes: 20
tags: [vault, raft, quorum]
objectives:
  - Recognise the symptoms of lost quorum
  - Recover a single node with peers.json
parameters:
  - id: version
    type: version          # drop-down fed by the product's available versions
    product: vault
    label: Vault version
topology:
  clusters:
    - name: Primary
      product: vault
      nodes: 3
      storage: raft
steps:
  - id: break
    kind: breakage
    title: Remove two nodes
    guide: steps/01-break.md
    action: [{ primitive: container.remove, args: { nodes: [node-2, node-3] } }]
    verify: [{ primitive: vault.expect-no-leader, args: { node: node-1 } }]
  - id: recover
    kind: verification
    title: Cluster writable again
    guide: steps/02-recover.md
    resetPoint: true
    verify: [{ primitive: vault.raft-peers, args: { node: node-1, voters: 1 } }]
```

Parameter types are added **as scenarios need them**. The first release needs a version drop-down, an edition drop-down and a bounded whole number (node count in the sandbox).

### 4.5 Primitives

A **primitive** is a named, tested building block that a scenario composes in YAML (R-SCN-2). Primitives are the only way scenario files act on the world.

**Contract**

| Aspect | Rule |
|---|---|
| Name | Dotted and lower-case, grouped by owner: `container.*`, `file.*`, `http.*`, `wait.*`, `assert.*`, and `<product>.*` such as `vault.*` and `boundary.*`. |
| Arguments | Typed and declared by the primitive. They are **validated when the scenario loads**, so a typo fails `s1s scenario validate`, not a live run. |
| Result | `status` (`pass` or `fail`), a short `message`, optional `outputs` (named values later steps can use) and `evidence` (captured command output for the instructor). |
| Error versus fail | **Fail** means it ran and the condition was not met (for example the cluster is still broken). **Error** means it could not run (for example the container is gone). They look different in the UI. |
| Timeout | Every primitive has a default timeout and accepts `timeout`. It never blocks forever. |
| Context | It receives the run's `context.Context`, so ending the scenario cancels it. |
| Side effects | A primitive that changes the environment says so in its documentation, and is safe to run again unless documented otherwise. |
| Secrets | Evidence and messages are redacted of known credentials before they are shown or stored (R-QUAL-3). |

**Templating.** String arguments can use values from the run, written `{{ ... }}`: parameters (`.Params.version`), nodes (`.Nodes.node_1.IP`, `.Nodes.node_1.Name`), credentials, keyed by the cluster or service name in the topology (`.Creds.primary.rootToken`) and outputs of earlier primitives. An unknown value fails validation.

**Initial catalogue** (first release). Each is documented, with tests, in the Contributing docs.

| Group | Primitives |
|---|---|
| `container` | `exec` (command, node, expected exit code, expected output), `stop`, `start`, `remove`, `kill-process`, `copy-in`. |
| `file` | `render` (a template from the scenario's `templates/` into a node), `write`. |
| `http` | `request` (method, URL, headers, body, expected status or body match). |
| `wait` | `until` (retry a check at an interval until it passes or times out), `sleep` (short, documented use only). |
| `assert` | `equals`, `contains`, `matches`, `json-path`. |
| `vault` | `cli` (run a Vault command with the token and address injected through the environment, with expectations), `status` (sealed, initialised, active), `raft-peers` (server and voter counts), `expect-no-leader`, `kv-put`, `kv-get`, `unseal`. `replication-status` arrives with the replication scenarios (M7). |
| `boundary` | `cli` (run a Boundary command, optionally authenticated as a named user), `authorize-session`, `session-info`, `target-info`, `recordings-list`. |
| `minio` | `mc` (run an `mc` command with injected credentials). |
| `softhsm` | `init-token`, `wipe-tokens`. |

Products may add primitives through `Product.Primitives()` (3.5). Scenario-specific Go actions (R-SCN-4) are registered by name in the same registry.

### 4.6 Step execution and failure handling

**Step states.** `locked` -> `active` -> (`applying` -> `applied` for a breakage) -> `complete`. A step is `active` only after the previous one is `complete`.

| Event | Behaviour |
|---|---|
| Action primitive errors | The step shows the error and the evidence, stays `active`, and offers **Retry**. Nothing advances. |
| Action primitive fails midway | The environment may be half-changed. The step offers **Retry** and **Roll back** (if a reset point exists), and the instructor can end the scenario. |
| Verification fails | Shown as red, "still broken", with the primitive's short message in the status area and the evidence collapsed for the instructor. **Continue** stays disabled. It can be run again at any time. |
| Verification errors | Shown distinctly from red, as a problem running the check, with the error. |
| Deployment fails | The Start step shows the error and logs. The instructor can **Retry** (tear down and deploy again) or **End scenario**. A failed deployment is cleaned up like any other run. |
| Verification timing | A verification may use `wait.until` to allow for settling, up to its timeout. |
| Server stops mid-step | Everything is destroyed (R-STATE-2). The report is saved as ended early if possible (R-REPORT-6). |

### 4.7 Reset points

A step flagged `resetPoint: true` marks a state the instructor can **roll back** to (R-SCN-6).

- **Default behaviour:** destroy the environment, redeploy it from the scenario's deployment, then **replay the actions of the steps up to and including the reset point**. Fixes the trainee made are not replayed, so the **step after the last replayed action becomes the active one**: rolling back to a reset point on a verification step returns the trainee to the step after the breakage, and the steps in between are done again. This is deterministic and needs no snapshots.
- **Override:** a step may declare its own `reset` primitives instead, for a faster or more precise revert.
- Rolling back keeps the report: step times and notes are retained, and the roll back is recorded as an event in the report.
- Snapshot-based rollback and drift detection are roadmap items (Section 11).

*TBD — the exact YAML field names and the full argument list of each primitive, settled as the primitives are built.*

## 5. Features & Scenarios

### 5.1 Features

| ID | Feature |
|---|---|
| F-1 | Scenario catalogue grouped by product, with difficulty, estimated time and tags. |
| F-2 | **Start scenario** step with per-scenario parameters, names of instructor and trainee, and automated deployment with live progress. |
| F-3 | Step flow: breakage and verification steps, guidance, notes, 1-5 rating, automatic timing, no skipping. |
| F-4 | **Cluster tab**: grouped nodes, IPs, status, credentials, node actions and in-app terminals. |
| F-5 | **Logs tab**: live-streamed operational and audit logs per node. |
| F-6 | Roll back to reset points, and end a scenario early with full teardown. |
| F-7 | **Reports tab**: every run is saved as data and viewable as HTML. |
| F-8 | **Settings**: product licenses, instructor name. |
| F-9 | Automatic download, verification and caching of release binaries, for community and Enterprise builds. |
| F-10 | Start-up checks: Podman installed, Podman machine running, clear errors otherwise. |
| F-11 | Clean-up of leftovers from a previous crashed run. |
| F-12 | Headless CLI: `version`, `prereq`, `scenario list`, `scenario validate` and `scenario new`. |

### 5.2 First-release scenario catalogue

Every scenario below ships in the first release. Each is a folder under `scenarios/` (3.6). The sandboxes have no incident and finish when the instructor ends them.

| Product | Scenario | Summary |
|---|---|---|
| Vault | **Sandbox** | A Vault cluster of 1 to 9 nodes (community or Enterprise) to explore or practise on. |
| Vault | **Raft Lost Quorum** | Two of three Raft voters disappear. Recover the survivor with `peers.json`, verify writes, optionally restore three nodes. |
| Vault | **DR Failover and Failback** | The primary cluster's Vault processes die. Generate a DR operation token, promote the DR secondary, verify data, then fail back. |
| Vault | **DR Break Glass Promotion** | The primary is gone and no batch DR token exists. Use break glass to generate a DR operation token from unseal keys, promote, and close the exposure window. |
| Vault | **Broken Performance Replication** | A performance secondary stops replicating because the primary's cluster address is wrong. Fix the address, mint a new token and `update-primary`. |
| Vault | **HSM Auto-Unseal Failure** | The SoftHSM PKCS#11 token is wiped. Diagnose the seal failure, re-initialise the token, update the seal stanza and migrate with recovery keys. |
| Vault | **Broken KMS** | An auto-unseal KMS credential is revoked and the cluster cannot unseal after a restart. *(Proposed definition, 5.3.7.)* |
| Boundary | **Sandbox** | A Boundary deployment (controllers, worker, database, target) to explore or practise on. |
| Boundary | **Authorize-Session 403** | A user can see a target but cannot connect. Find and add the missing `authorize-session` grant. |
| Boundary | **Session Limits** | Connections are refused or cut off. Find and fix `session_connection_limit` and `session_max_seconds`. |
| Boundary | **Session Recording Storage** (Enterprise) | The worker loses access to the MinIO bucket. Repair both MinIO and Boundary, then prove a new recording. |

### 5.3 Per-scenario definitions

Each definition follows the Scenario Standard (5.4). Steps use the kinds in 4.2. "Guidance" is the text shown to the instructor to help guide the trainee (R-SCN-7). The instructor performs the changes the trainee asks for, using terminals (R-VIEW-2) and node actions (R-VIEW-6).

#### 5.3.1 Vault: Raft Lost Quorum

| Field | Value |
|---|---|
| **ID** | `vault-raft-lost-quorum` |
| **Product / editions** | Vault, community or Enterprise |
| **Difficulty / time** | Intermediate to Advanced, about 15 to 20 minutes |
| **Tags** | `vault`, `raft`, `integrated-storage`, `quorum`, `disaster-recovery`, `peers.json` |

**Summary.** Two of the three nodes in a Vault Raft cluster disappear. The cluster loses quorum and every API call fails. The trainee guides the instructor through manual recovery of the surviving node.

**Learning objectives**
1. Explain Raft quorum (floor(N/2) + 1) and why two of three nodes lost means no leader.
2. Recognise lost quorum from `vault status`, the HTTP error and the Raft logs.
3. Recover with `peers.json`: where it goes, its fields, and why Vault must be stopped first.
4. Verify recovery: active leader, one voter, writes succeed.
5. Re-expand to three nodes safely with fresh nodes.

**Prerequisites.** Vault concepts (init, unseal, KV). Raft integrated storage configuration. Basic container commands.

**Parameters (Start step).** Vault version (drop-down, default the newest available). Edition (drop-down).

**Initial state.** One network. Three Vault containers (`node-1` to `node-3`) on Raft integrated storage, with `retry_join` to each other by container IP, TLS disabled, `disable_mlock`, audit logging enabled. Initialised with 5 key shares and a threshold of 3, all nodes unsealed, `node-1` leader. No KV engine mounted. The Cluster tab shows IPs, node IDs, root token and unseal keys.

**Failure created.** The Vault process on `node-2` and `node-3` is stopped and their containers removed. Recoverable by `peers.json`.

**Symptoms.** `vault status` on `node-1`: HA Mode `standby`, HA Cluster `n/a`, Active Node Address `<none>`. `/v1/sys/health` returns 429. API calls return 500 `local node not active but active cluster node not found`. Raft log shows repeated `Election timeout reached, restarting election`.

**Steps**

| # | Kind | Title | Action | Verification | Reset point |
|---|---|---|---|---|---|
| 1 | start | Start scenario | Deploy (above) | Cluster healthy: 3 voters, leader present | no |
| 2 | breakage | Cause the outage | Stop and remove `node-2` and `node-3` | `node-1` has no active node and API returns the 500 error | **yes** (broken state) |
| 3 | verification | Quorum recovered | none | `node-1` unsealed, HA mode `active`, `list-peers` shows exactly one voter, a KV mount can be enabled and a secret written | no |
| 4 | verification | Restore three nodes (extension) | Instructor revives `node-2` and `node-3` as fresh nodes (R-VIEW-6) | `list-peers` shows three voters after autopilot promotion | no |

**Guidance (instructor).**
- *Step 2 briefing:* present the customer's report: "Vault is returning 500 on everything". Do not reveal the cause. Let the trainee ask for `vault status`, logs and `list-peers`.
- *Step 3 hints:* `peers.json` is only read at startup after unseal. It goes in `<storage path>/raft/`, not the storage root. The `id` must match `node_id` exactly. The address is the container IP and the **cluster** port 8201. `non_voter` is `false`. Vault must be fully stopped before the file is placed.
- *Step 3 solution:* stop Vault in `node-1` -> create `peers.json` with the node's ID and `<ip>:8201` -> copy it into `/opt/vault/data/raft/` -> start Vault -> unseal with three key shares -> logs show `raft recovery initiated`, `election won`, `entering leader state`, and Vault deletes the file.
- *Step 4 hints:* replacement nodes must have empty data directories. Newly joined nodes show `Voter=false` until autopilot's stabilisation period passes (about 15 to 30 seconds in practice).

**Success criteria.** Steps 3 and 4 verifications pass.

**Pitfalls**

| Symptom | Cause | Fix |
|---|---|---|
| `peers.json` still present after unseal | Wrong path, or Vault not fully stopped when the file was placed | Use `<storage>/raft/peers.json`; confirm no `vault` process |
| Still standby after unseal | File not consumed, or fewer than threshold keys given | Check the file was deleted; submit three keys |
| `id` mismatch | Differs from `node_id` | Read the HCL config |
| Wrong address | API port or hostname used | Use container IP and port 8201 |
| New node fails to join | Stale Raft data | Recreate with an empty data directory |

**References.** Vault documentation: Raft lost quorum recovery tutorial, integrated storage manual recovery, Raft storage configuration, `vault operator raft`, autopilot.

---

#### 5.3.2 Vault: Sandbox

| Field | Value |
|---|---|
| **ID** | `vault-sandbox` |
| **Product / editions** | Vault, community or Enterprise |
| **Difficulty / time** | Open-ended, about 1 minute to deploy |
| **Tags** | `vault`, `sandbox`, `raft` |

**Summary.** A Vault cluster to explore or to practise on before a scenario. There is no incident. It is open-ended (R-SCN-17).

**Learning objectives.** Get comfortable with the run page, Cluster and Logs tabs, terminals and node actions. Explore Vault features freely.

**Parameters.** Vault version (drop-down). Edition (drop-down). Node count (1 to 9, default 3).

**Initial state.** One network. The chosen number of Vault nodes on Raft integrated storage, initialised (5 shares, threshold 3), unsealed, audit logging enabled. For Enterprise, the license from Settings is applied. The Cluster tab shows IPs, root token and unseal keys.

**Failure created.** None.

**Steps**

| # | Kind | Title | Action | Verification | Reset point |
|---|---|---|---|---|---|
| 1 | start | Start scenario | Deploy | Cluster healthy | no |

**Guidance (instructor).** Use the sandbox to demonstrate Vault features or to try an approach before a live scenario. Node actions (stop, start, remove, revive) are available to simulate failures by hand.

**Success criteria.** None. The instructor ends the scenario when finished.

---

#### 5.3.3 Vault: DR Failover and Failback

| Field | Value |
|---|---|
| **ID** | `vault-dr-failover` |
| **Product / editions** | Vault Enterprise |
| **Difficulty / time** | Advanced, about 30 to 40 minutes |
| **Tags** | `vault`, `enterprise`, `replication`, `disaster-recovery`, `dr`, `raft`, `failover`, `failback` |

**Summary.** The primary cluster's Vault processes are killed. A DR secondary stands by. The trainee guides the full failover to the secondary and the failback to the original primary.

**Learning objectives**
1. Explain DR replication: primary and secondary roles, and why a DR secondary serves no traffic until promoted.
2. Explain why batch DR operation tokens are created before a disaster.
3. Generate the token, promote the secondary and redirect clients.
4. Read `sys/replication/dr/status` at each stage.
5. Demote and re-point the original primary to avoid split-brain.
6. Fail back, and confirm data written before and during failover is present.

**Prerequisites.** Vault Enterprise replication concepts. Raft. Unsealing.

**Parameters.** Vault Enterprise version (drop-down).

**Initial state.** Cluster A: three Raft nodes, initialised (5/3), unsealed, license applied, DR primary replication enabled, seeded with a KV secret. Cluster B: one node, DR secondary enabled and synced (warm standby). Linking replaces the secondary's data, so it shares Cluster A's root token and unseal keys; the keys it was initialised with no longer work. A **batch DR operation token** was created on Cluster A before the failure and appears on the Cluster tab. Both clusters' credentials appear on the Cluster tab.

**Failure created.** The Vault process on all three Cluster A nodes is killed. Containers keep running. Recoverable by DR failover.

**Symptoms.** Cluster A's API does not respond. Cluster B reports it is a DR secondary and refuses reads and writes.

**Steps**

| # | Kind | Title | Action | Verification | Reset point |
|---|---|---|---|---|---|
| 1 | start | Start scenario | Deploy both clusters, seed data | DR status `stream-wals` on B | no |
| 2 | breakage | Cause the outage | Kill Vault on all Cluster A nodes | Cluster A API unreachable | **yes** |
| 3 | verification | Cluster B promoted | none | B reports mode `primary`; seeded secret readable on B; the check then writes `secret/during-failover` on B | no |
| 4 | verification | Cluster A recovered | none | A nodes running and unsealed | no |
| 5 | verification | Cluster A demoted | none | A reports DR secondary mode | no |
| 6 | verification | Cluster A re-pointed | none | A replicates from B (`connection_state` ready) | no |
| 7 | verification | Failback complete | none | A is primary, B is DR secondary, and a secret written during failover is present on A | **yes** |

**Guidance (instructor).** Customer story: "our primary cluster is down, and the business needs Vault now". Hints: use the batch DR operation token to promote B; promotion needs `sys/replication/dr/secondary/promote`; demote A before re-pointing it; the new primary issues a secondary token for A. Solution: promote B with the token, verify data, restart and unseal A, demote A, generate a secondary token on B, update A's primary, then promote A and re-point B.

**Success criteria.** Step 7 verification passes.

**Pitfalls.** Promoting without demoting the old primary causes a split-brain condition. A DR secondary uses the primary's root token and unseal keys once linked. Only the active node of a cluster serves most replication endpoints, so `vault.dr-status` asks every node of the cluster.

**References.** Vault Enterprise DR replication documentation and tutorials.

---

#### 5.3.4 Vault: DR Break Glass Promotion

| Field | Value |
|---|---|
| **ID** | `vault-dr-break-glass` |
| **Product / editions** | Vault Enterprise 2.0.0 or newer |
| **Difficulty / time** | Advanced, about 20 to 30 minutes |
| **Tags** | `vault`, `enterprise`, `replication`, `disaster-recovery`, `dr`, `break-glass`, `generate-root`, `promotion` |

**Summary.** The primary cluster is permanently lost and no batch DR token exists. The trainee guides a break glass promotion of the DR secondary using only unseal key shares.

**Learning objectives**
1. Know when break glass is required and why a prepared batch token is preferred.
2. Know that break glass removes the token requirement, not the unseal key requirement.
3. Apply `enable_unauthenticated_access` to a running server with `SIGHUP` (not `sys/config/reload`).
4. Generate a DR operation token with `vault operator generate-root -dr-token` (`-init`, key shares, `-decode`).
5. Promote the secondary and verify with `sys/replication/dr/status`.
6. Close the exposure window and prove the API returns 403 again.
7. Know why the `force` promote parameter must not be used.

**Parameters.** Vault Enterprise version (drop-down, 2.0.0 or newer).

**Initial state.** Cluster A: three Raft nodes (5/3), unsealed, license applied, DR primary replication enabled, a `userpass` user and a `dr-admin` policy that replicate to B. Cluster B: one node (5/3), unsealed, DR secondary synced, started with `-config=/etc/vault.d` so extra `.hcl` files there are loaded. Linking replaced Cluster B's data, so it shares Cluster A's unseal keys and root token, and they appear on the Cluster tab with the `dr-admin` password. **No batch DR operation token exists.**

**Failure created.** Cluster A's containers are force-removed. Its data is destroyed.

**Symptoms.** Cluster A is gone. Cluster B is a DR secondary and cannot be promoted without a DR operation token, and generating one needs a Vault token the trainee does not have.

**Steps**

| # | Kind | Title | Action | Verification | Reset point |
|---|---|---|---|---|---|
| 1 | start | Start scenario | Deploy both clusters | DR status `stream-wals` on B | no |
| 2 | breakage | Destroy the primary | Force-remove Cluster A containers | Cluster A unreachable; B still a DR secondary | **yes** |
| 3 | verification | Break glass enabled | none | An unauthenticated read of `generate-operation-token/attempt` on B answers instead of returning 403 | no |
| 4 | verification | Cluster B promoted | none | B reports mode `primary`; the replicated `userpass` login works (the check logs in as `dr-admin` with the password from the Cluster tab) | no |
| 5 | verification | Exposure window closed | none | The break glass config is gone and an unauthenticated `generate-operation-token` call returns 403 | no |

**Guidance (instructor).** Customer story: "the primary site is gone and we never made the batch token". Hints: the extra config file goes in `/etc/vault.d/`; Vault must receive `SIGHUP`; the key shares are the threshold-many of the shared keys on the Cluster tab (the primary's, which B shares). Solution: add `break-glass.hcl` with `enable_unauthenticated_access = ["generate-operation-token"]`, `SIGHUP`, run the `generate-root -dr-token` flow with three key shares, decode, promote, remove the file, `SIGHUP`, confirm 403.

**Success criteria.** Steps 4 and 5 verifications pass.

**Pitfalls.** `sys/config/reload` does not apply the setting. Leaving the config in place leaves the API open. A blocked generation attempt must be cancelled before retrying. Never use `force` on promote.

**References.** Vault documentation: DR operation tokens, generate-root DR token, `enable_unauthenticated_access`.

---

#### 5.3.5 Vault: Broken Performance Replication

| Field | Value |
|---|---|
| **ID** | `vault-broken-replication` |
| **Product / editions** | Vault Enterprise |
| **Difficulty / time** | Intermediate to Advanced, about 25 to 35 minutes |
| **Tags** | `vault`, `enterprise`, `replication`, `performance-replication`, `raft`, `networking`, `cluster_addr`, `update-primary` |

**Summary.** A healthy performance replication relationship stops because the primary's advertised cluster address was changed to an unreachable value and the secondary was revoked. The API stays healthy, so the fault is easy to misdiagnose as a token or licence problem.

**Learning objectives**
1. Know which addresses replication uses: the API port (8200) unwraps the activation token, and the cluster port (8201) carries mutually authenticated replication traffic.
2. Read `sys/replication/performance/status` on both clusters.
3. Separate a network fault from a token, licence or auth fault using status, logs and a TCP test from a secondary node.
4. Understand the `primary_cluster_addr` override and that tokens embed it when minted.
5. Know that a secondary ID stays activated until revoked, and a new token needs `revoke-secondary` first.
6. Reconnect with `update-primary` without wiping the secondary.
7. Know what replicates (policies, auth methods, shared mounts) and what does not (tokens, local mounts), and that enabling a secondary replaces its seal configuration and keys with the primary's.

**Parameters.** Vault Enterprise version (drop-down).

**Initial state.** Cluster A (performance primary) and Cluster B (performance secondary), three Raft nodes each, correct `api_addr` and `cluster_addr`, unsealed, license applied. Replicating configuration: a `superpolicy` policy, a `userpass` user `superuser`, a KV v2 mount `replicatedkv/` with a seeded secret, and a local KV mount `localkv/` (not replicated). Enabling the secondary replaces B's data, so B uses A's unseal keys, and its other nodes come back sealed until they are unsealed with them. Root tokens do not replicate, so A's root token is refused on B. B instead has a login token from the replicated `superuser` user. Credentials appear on the Cluster tab.

**Failure created.** The primary's `primary_cluster_addr` is set to an address that does not exist, then the secondary is revoked. All six containers keep running and Vault is unsealed everywhere. Recoverable without wiping B.

**Symptoms.** B's `connection_state` is `transient_failure`. A's `known_secondaries` is empty or disconnected. B's log shows repeated `dial tcp <unused-ip>:8201: no route to host`.

**Steps**

| # | Kind | Title | Action | Verification | Reset point |
|---|---|---|---|---|---|
| 1 | start | Start scenario | Deploy both clusters, seed data | Replication `ready`; seeded secret on B | no |
| 2 | breakage | Break replication | Set unreachable `primary_cluster_addr`, revoke the secondary | B's `connection_state` is not `ready` | **yes** |
| 3 | verification | Primary address corrected | none | A's `primary_cluster_addr` is a reachable cluster address | no |
| 4 | verification | Secondary reconnected | none | B reports `connection_state` `ready` | no |
| 5 | verification | Data replicates again | none | The check writes a secret on A and waits for it on B | **yes** |

**Guidance (instructor).** Customer story: "replication stopped yesterday; nothing changed on our side". Hints: compare `primary_cluster_addr` on A with the actual node cluster address; check reachability on 8201 from a B node; a fresh token needs the old one revoked. Solution: re-run `primary/enable` with the correct address, mint a new secondary token for the same ID, `update-primary` on B, confirm status and data.

**Success criteria.** Steps 3, 4 and 5 verifications pass.

**Pitfalls.** Minting a token before correcting the address embeds the wrong address. A secondary token cannot be reused. A's root token is refused on B because tokens never replicate; log in on B with the replicated `superuser` login. `generate-root` is also refused on a performance secondary.

**References.** Vault Enterprise performance replication documentation and tutorial.

---

#### 5.3.6 Vault: HSM Auto-Unseal Failure

| Field | Value |
|---|---|
| **ID** | `vault-hsm-autounseal` |
| **Product / editions** | Vault Enterprise 1.19 or newer (PKCS#11 seal) |
| **Difficulty / time** | Intermediate, about 20 to 25 minutes |
| **Tags** | `vault`, `hsm`, `pkcs11`, `softhsm`, `auto-unseal`, `seal-wrap`, `fips`, `enterprise` |

**Summary.** A Vault node auto-unseals through a SoftHSMv2 PKCS#11 token. The HSM is replaced and comes back empty, and Vault will not start. The trainee guides diagnosis and recovery from the HSM backup.

**Learning objectives**
1. Diagnose a PKCS#11 seal failure from the Vault log and from the state of the HSM.
2. Know that the root key is protected by a key that lives only in the HSM, and that recovery keys cannot replace it.
3. Restore a SoftHSMv2 token from backup and confirm the slot matches the seal stanza.
4. Start Vault and confirm it unseals by itself, without keys.
5. Explain why initialising a new token does not help, and what it costs.

**Parameters.** Vault Enterprise version (drop-down).

**Initial state.** One Vault Enterprise container with SoftHSMv2 and OpenSC installed. A token `vault-hsm-token` initialised. A `seal "pkcs11"` stanza configured with the slot, PIN and key label. Vault is initialised with recovery keys and unsealed, with a KV v2 mount at `secret/` containing `hsm-test`. Auto-unseal was proved by a restart, and a backup of the token is kept in `/var/backups/softhsm-tokens`. Recovery keys, root token, slot, PIN and the backup location appear on the Cluster tab.

**Failure created.** Vault is stopped, the SoftHSMv2 token directory is emptied, and Vault is started again. Recoverable from the backup.

**Symptoms.** Vault does not start. `vault status` reports connection refused. The log ends with `error parsing Seal configuration: error creating HSM context: slot not found`. `softhsm2-util --show-slots` shows no initialised token.

**Steps**

| # | Kind | Title | Action | Verification | Reset point |
|---|---|---|---|---|---|
| 1 | start | Start scenario | Deploy, seed secret, prove auto-unseal | Vault unsealed; `hsm-test` readable | no |
| 2 | breakage | Lose the HSM token | Wipe the token directory, restart Vault | Vault not running and the slot error in the log | **yes** |
| 3 | verification | Vault unsealed again | none | Vault unsealed and active, and `hsm-test` readable | no |
| 4 | verification | Auto-unseal restored | none | The check restarts Vault, which then unseals without keys | **yes** |

**Guidance (instructor).** Customer story: "we replaced the HSM hardware and Vault will not start". Hints: Vault is not running, so read the log; the stanza names a slot that no longer exists; ask the customer whether the old HSM was backed up. Solution: read the log and `softhsm2-util --show-slots`, restore the token with `cp -a /var/backups/softhsm-tokens/. /var/lib/softhsm/tokens/`, start Vault, read the secret.

**Success criteria.** Steps 3 and 4 verifications pass.

**Pitfalls.** Initialising a new token and pointing the stanza at its slot: Vault starts but stays sealed with `CKR_SIGNATURE_INVALID`, because the new token holds a new key and the old one is gone. Roll back is the way out. Trying the recovery keys: with an HSM seal they cannot unseal Vault. Trying `unseal -migrate`: refused with "no migration seal found".

**Design note.** The first draft had the trainee re-initialise the token and unseal with recovery keys. Playing it on real Vault Enterprise showed that cannot work: once the token is gone the HSM key is gone, and the root key cannot be decrypted. The scenario therefore recovers from the backup, which is also what happens in practice.

**References.** Vault seal and HSM documentation, seal migration.

---

#### 5.3.7 Vault: Broken KMS (proposed definition)

> This scenario has no earlier definition. The design below was proposed, then played on real Vault, and corrected to match what Vault does.

| Field | Value |
|---|---|
| **ID** | `vault-broken-kms` |
| **Product / editions** | Vault, community or Enterprise |
| **Difficulty / time** | Intermediate, about 20 minutes |
| **Tags** | `vault`, `auto-unseal`, `transit-seal`, `kms`, `seal` |

**Summary.** A Vault cluster auto-unseals through the transit secrets engine of a separate "KMS" Vault. The credential the cluster uses to reach the KMS is revoked. After a restart Vault cannot start. The trainee guides diagnosis and repair.

**Learning objectives**
1. Recognise a failed auto-unseal and read the seal error in the log.
2. Explain how a transit seal works and what it needs from the KMS (a reachable address, a valid token with a suitable policy, and the key).
3. Create a replacement token on the KMS Vault with the minimal transit policy.
4. Update the seal stanza (or its environment variable) on every node and restart.
5. Confirm auto-unseal works on a later restart.

**Parameters.** Vault version (drop-down). Edition (drop-down).

**Initial state.** A one-node KMS Vault with a transit key and a policy permitting encrypt and decrypt on it. A three-node workload cluster on Raft whose `seal "transit"` stanza points at the KMS Vault with a token. Workload cluster initialised, unsealed, with a seeded KV secret. Credentials of both Vaults appear on the Cluster tab.

**Failure created.** The token the workload cluster uses is revoked on the KMS Vault, then Vault is stopped and started on the workload nodes. Recoverable.

**Symptoms.** Vault does not start on the workload nodes, so `vault status` reports connection refused. The log ends with `error parsing Seal configuration` and a 403 `permission denied`, `invalid token` from the KMS address. Vault checks the transit seal at startup and exits if it cannot use it, so the nodes are not running, not merely sealed.

**Steps**

| # | Kind | Title | Action | Verification | Reset point |
|---|---|---|---|---|---|
| 1 | start | Start scenario | Deploy KMS Vault and workload cluster, seed secret | Workload unsealed; secret readable | no |
| 2 | breakage | Revoke the seal token | Revoke the token, then stop and start Vault on the workload nodes | Vault is not running on any workload node and the log shows the seal error | **yes** |
| 3 | verification | Cluster unsealed | none | All three nodes unsealed, a leader exists, the secret is readable | no |
| 4 | verification | Auto-unseal proven | none | The check restarts a node, which then unseals itself | no |

**Guidance (instructor).** Customer story: "after a maintenance window our Vault will not unseal". Hints: Vault is not running, so read the end of the log; the error names the KMS address and a permission problem; the key still exists on the KMS. Solution: create a new token on the KMS Vault with the transit policy, put it in each node's seal config, start Vault, confirm, restart one node to prove auto-unseal.

**Success criteria.** Steps 3 and 4 verifications pass.

**Pitfalls.** Forgetting a node (the step stays red until all three run). Creating a token without the policy. Editing the config but not starting Vault again.

**References.** Vault documentation: transit seal, auto-unseal, token and policy concepts.

---

#### 5.3.8 Boundary: Sandbox

| Field | Value |
|---|---|
| **ID** | `boundary-sandbox` |
| **Product / editions** | Boundary, community or Enterprise |
| **Difficulty / time** | Open-ended, about 1 minute to deploy |
| **Tags** | `boundary`, `sandbox` |

**Summary.** A self-managed Boundary deployment to explore or practise on. No incident. It is open-ended (R-SCN-17).

**Learning objectives.** Get comfortable with the run page and terminals. Explore Boundary freely.

**Parameters.** Boundary version (drop-down). Edition (drop-down).

**Initial state.** One network. One PostgreSQL database. Three controllers behind an HAProxy load balancer. One worker. One target host running OpenSSH. The database is initialised with `boundary database init`. An `admin` account exists with global admin grants. A resource tree is seeded: an org scope, a project scope, a static host catalog with a host and host set, and a TCP SSH target. The Cluster tab shows addresses, ports and the generated admin credentials.

**Failure created.** None.

**Steps**

| # | Kind | Title | Action | Verification | Reset point |
|---|---|---|---|---|---|
| 1 | start | Start scenario | Deploy | Controllers and worker healthy | no |

**Guidance (instructor).** Use it to demonstrate Boundary or to prepare. Node actions are available.

**Success criteria.** None. The instructor ends the scenario when finished.

---

#### 5.3.9 Boundary: Authorize-Session 403

| Field | Value |
|---|---|
| **ID** | `boundary-authorize-session-403` |
| **Product / editions** | Boundary, community or Enterprise |
| **Difficulty / time** | Beginner, about 10 to 15 minutes |
| **Tags** | `boundary`, `iam`, `roles`, `grants`, `authorize-session`, `403`, `permission-denied` |

**Summary.** A user can authenticate and see a target but cannot connect. Every attempt fails with `PermissionDenied` (403).

**Learning objectives**
1. Read a 403 from `authorize-session` as an authorisation failure, distinct from an authentication failure or a 404.
2. Trace a permission from user to roles to grants and grant scopes.
3. Write grant strings such as `ids=<ttcp_id>;actions=authorize-session`.
4. Understand that grant scopes decide where a role applies.
5. Verify the fix with a session token and `boundary connect`.

**Parameters.** Boundary version (drop-down). Edition (drop-down).

**Initial state.** The Boundary sandbox (5.3.8).

**Failure created.** A user `jsmith` and a role `ssh-readonly` are created. The role grants `list` and `read` on targets in the project scope and no `authorize-session`. Recoverable by adding that grant to a role that applies to `jsmith`.

**Symptoms.** `authorize-session` as `jsmith` returns `PermissionDenied`, status 403.

**Steps**

| # | Kind | Title | Action | Verification | Reset point |
|---|---|---|---|---|---|
| 1 | start | Start scenario | Deploy the sandbox | Cluster healthy | no |
| 2 | breakage | Create the permission problem | Create `jsmith` and `ssh-readonly` | `authorize-session` as `jsmith` returns 403 | **yes** |
| 3 | verification | Access restored | none | `authorize-session` as `jsmith` returns a session token | no |

**Guidance (instructor).** Customer story: "jsmith can see the SSH target but cannot connect". Hints: read the roles that apply to jsmith, and their grants and grant scopes. Solution: add `ids=<target id>;actions=authorize-session` to the role (or a new role) with the project in its grant scopes, then connect.

**Success criteria.** Step 3 verification passes.

**Pitfalls.** Granting in the wrong scope. Confusing `read` with `authorize-session`. Not adding `jsmith` as a principal.

**References.** Boundary documentation: roles, grants, targets, sessions.

---

#### 5.3.10 Boundary: Session Limits

| Field | Value |
|---|---|
| **ID** | `boundary-session-limits` |
| **Product / editions** | Boundary, community or Enterprise |
| **Difficulty / time** | Beginner, about 10 to 15 minutes |
| **Tags** | `boundary`, `targets`, `sessions`, `session-connection-limit`, `session-max-seconds`, `termination-reason` |

**Summary.** Connections to a target are refused after the first, and a connection that stays open is dropped after about a minute. Neither is a fault in the controllers, worker or network.

**Learning objectives**
1. Separate the two settings and their symptoms.
2. Find `session_connection_limit` and `session_max_seconds` with `boundary targets read`.
3. Read a terminated session and its termination reason with `sessions list -include-terminated` and `sessions read`.
4. Update a TCP target with `-session-connection-limit` and `-session-max-seconds`, knowing `-1` means unlimited.
5. Know that limits are copied onto a session when it is authorised, so the fix is proved only with a new session.

**Parameters.** Boundary version (drop-down). Edition (drop-down).

**Initial state.** The Boundary sandbox (5.3.8).

**Failure created.** The target is set to one connection per session and a 60-second lifetime. Recoverable.

**Symptoms.** A second connection is closed immediately and the proxy shows `Connections Left: 0`. An open connection is closed after 60 seconds with `Reason: Session has expired`.

**Steps**

| # | Kind | Title | Action | Verification | Reset point |
|---|---|---|---|---|---|
| 1 | start | Start scenario | Deploy the sandbox | Cluster healthy | no |
| 2 | breakage | Apply the limits | Set the target limits | The target shows connection limit 1 and max 60 seconds | **yes** |
| 3 | verification | Limits corrected | none | A new session shows a connection limit above 1 (or -1) and a lifetime above 60 seconds | no |

**Guidance (instructor).** Customer story: "connections work once, then get cut off". Hints: read the target and the terminated session. Solution: update the target with realistic limits, start a new session and confirm.

**Success criteria.** Step 3 verification passes.

**Pitfalls.** Existing sessions keep their old limits. Terminated sessions are hidden from a plain `list`.

**References.** Boundary documentation: targets, sessions, session limits.

---

#### 5.3.11 Boundary: Session Recording Storage

| Field | Value |
|---|---|
| **ID** | `boundary-session-recording-storage` |
| **Product / editions** | Boundary **Enterprise** only |
| **Difficulty / time** | Intermediate to Advanced, about 25 to 35 minutes |
| **Tags** | `boundary`, `enterprise`, `session-recording`, `storage-bucket`, `minio`, `s3`, `workers`, `bsr` |

**Summary.** Sessions on a recorded target work, and recordings are stored in a MinIO bucket. The MinIO user the worker records as is deleted. Boundary still holds that user's keys, so recorded sessions stop working. Fixing only one side leaves the worker failing.

**Learning objectives**
1. Explain how recording is wired: a `bsr` KMS key on controllers, a recording directory and tag on the worker, a storage bucket whose worker filter matches that tag, and a target with recording enabled and injected credentials.
2. Read the failure and see which system rejected which credential, including the worker's `ValidatePermissions` error events.
3. Read a storage bucket and know secrets are write-only (an HMAC is shown).
4. Find the cause on the store with `mc admin user list` and `mc admin policy list`.
5. Repair both sides: recreate the MinIO user with the recorder policy and update the storage bucket's secrets in Boundary.
6. Prove it: record a session, find it with `session-recordings list` (state `available`, storage `synced`), play a channel back and see it in the bucket.

**Prerequisites.** A Boundary Enterprise license in Settings.

**Parameters.** Boundary Enterprise version (drop-down, only versions with an Enterprise build).

**Initial state.** The Boundary sandbox (5.3.8) plus a MinIO server (pinned release, S3 on 9000, console on 9001), a bucket, a user `boundary-recorder` with a recorder policy, a `bsr` KMS key on every controller, a worker with a recording storage path and the tag `type = ["minio-worker"]`, a storage bucket in Boundary whose worker filter matches that tag, and an SSH target with recording enabled. At least one session has been recorded successfully.

**Failure created.** `mc admin user remove` deletes `boundary-recorder` in MinIO. Recoverable by recreating the user, reattaching the policy and updating the storage bucket secrets.

**Symptoms.** Controllers, database, worker registration and authentication are healthy. A new session to the recorded target is refused when authorised, with an error from the controller. The worker's log shows permission validation errors.

**Steps**

| # | Kind | Title | Action | Verification | Reset point |
|---|---|---|---|---|---|
| 1 | start | Start scenario | Deploy sandbox and MinIO, record one session | One recording is `available` and `synced` | no |
| 2 | breakage | Remove the recorder user | Remove the MinIO user | A new recorded session is refused | **yes** |
| 3 | verification | Access restored | none | MinIO has a valid recorder user and Boundary's bucket secrets match it | no |
| 4 | verification | Recording proven | none | A new session is recorded, `available`, `synced`, and present in the bucket | no |

**Guidance (instructor).** Customer story: "recorded sessions stopped working after we cleaned up access keys". Hints: two systems each hold the credential; Boundary never shows a secret, only an HMAC. Solution: create a new MinIO user with the recorder policy, update the storage bucket secrets in Boundary, record a new session, list and play it back.

**Success criteria.** Steps 3 and 4 verifications pass.

**Pitfalls.** Fixing MinIO only, or Boundary only. A policy that does not allow writes to the bucket. Checking too early, before the recording syncs.

**References.** Boundary Enterprise session recording documentation, MinIO `mc admin` documentation.

### 5.4 Scenario Standard

Every scenario, built-in or user-made, defines the following. The `scenario validate` command checks them.

| Field | Description |
|---|---|
| ID / slug | Stable, unique, kebab-case. |
| Name and summary | Shown in the catalogue. |
| Product(s) and editions | Community, Enterprise or both. |
| Difficulty | Beginner, Intermediate or Advanced. |
| Estimated time | In minutes. |
| Tags | For grouping. |
| Learning objectives | What the trainee should gain. |
| Prerequisites | Knowledge and tools needed. |
| Parameters | What the Start step lets the instructor choose (R-SCN-11). |
| Initial state | What the deployment provides before step one. |
| Failure created | What is broken, the symptoms, and whether it is recoverable. |
| Steps | Ordered, with guidance (R-SCN-7). |
| Success criteria | What "fixed" means. |
| Pitfalls | Common mistakes and symptoms. |
| References | Product documentation links. |

### 5.5 Product minimums for the Cluster and Logs tabs

Each product provides these for every deployment (R-VIEW-7, R-VIEW-8). Credentials are shown only on this tab (R-VIEW-5).

**Vault**

| Level | Minimum |
|---|---|
| Per cluster | Name and role (primary, secondary, DR, performance); API address reachable from the Mac (if published); root token; unseal keys (or recovery keys) and the threshold; license status for Enterprise. |
| Per node | Name; IP address; node ID; container status; Vault state (sealed or unsealed); HA role (leader, standby, performance standby, DR secondary); version and edition; terminal button; node actions. |
| Log streams | Operational log; audit log (on by default). |

**Boundary**

| Level | Minimum |
|---|---|
| Per deployment | Cluster URL (load balancer) and controller addresses reachable from the Mac; admin login name and password; auth method ID; org scope and project scope IDs; target ID; license status for Enterprise. |
| Per node | Name; IP address; role (controller, worker, load balancer, database, target host); container status; version and edition; terminal button; node actions. |
| Log streams | Controller or worker operational log; database log. |

**Supporting services (for example Postgres, MinIO, SoftHSM)**

| Level | Minimum |
|---|---|
| Per service | Address and port; credentials needed to use it; container status; terminal button; node actions; the service's main log. |

A scenario adds to these. For example, DR scenarios add the replication mode and the batch DR token, and the HSM scenario adds the slot number and PIN.

---

## 6. Frontend & UX

### 6.1 Information architecture

Top-level navigation (Carbon header and side navigation):

| Area | Content |
|---|---|
| **Scenarios** | Catalogue grouped by product, filter by product, difficulty, edition. Starting one opens the run. While a run is active, other scenarios are blocked with a clear message (R-SCN-8). |
| **Current run** | Visible only while a run exists. Holds the step view and the Cluster and Logs tabs. |
| **Reports** | List of all saved reports (R-REPORT-7). |
| **Settings** | Licenses and instructor name. |

### 6.2 The run page

1. **Header strip:** scenario name, instructor, trainee, elapsed time, **Roll back** (when a reset point is available) and **End scenario**.
2. **Tabs:** *Steps* (default), *Cluster*, *Logs*. Terminals open from the Cluster tab (R-VIEW-2).
3. **Steps tab:**
   - A progress indicator for all steps.
   - The active step shows: briefing, hints and solution (collapsed, for the instructor), the action or verify button, a status area (red or green, R-SCN-11 and 4.2), the **Continue** button, the notes field and the 1-5 rating.
   - Completed steps are collapsed and read-only. Future steps are locked.
4. **Cluster tab:** sections as supplied by the scenario (R-VIEW-1), node cards with status, IPs, credentials (with copy buttons and masking), terminal button and node actions.
5. **Logs tab:** node selector, stream selector (operational, audit), live view with pause and scroll-lock. The component takes a filter later without redesign (R-VIEW-3).

### 6.3 Interaction patterns

- Buttons that start work show a loading state, and progress arrives as Wails events. Errors appear as Carbon inline notifications with plain-language text (R-QUAL-1).
- Destructive actions (End scenario, Roll back, node Remove) ask for confirmation in a Carbon modal that states what will be destroyed.
- Pages are usable by keyboard and screen reader (R-UI-6).

### 6.4 Wireframes (text)

**Scenarios page**

```
+------------------------------------------------------------------+
| s1s            Scenarios | Current run | Reports | Settings        |
+------------------------------------------------------------------+
| Filter: [Product v] [Difficulty v] [Edition v]                   |
| VAULT                                                            |
| [ Raft Lost Quorum   | Intermediate | 20 min | Community/Ent ]     |
| [ DR Failover        | Advanced     | 40 min | Enterprise    ]     |
| BOUNDARY                                                         |
| [ Authorize-Session 403 | Beginner  | 15 min | Community/Ent ]    |
+------------------------------------------------------------------+
```

**Run page, Steps tab**

```
+------------------------------------------------------------------+
| Raft Lost Quorum   Instructor: A  Trainee: B   00:14:32            |
|                                   [Roll back] [End scenario]      |
+------------------------------------------------------------------+
| Steps | Cluster | Logs                                            |
+------------------------------------------------------------------+
| 1 Start  [done]   2 Cause the outage [done]   3 Quorum recovered   |
|                                                                   |
|  Step 3: Quorum recovered                                         |
|  Briefing / Hints (collapsed) / Solution (collapsed)              |
|  [ Verify ]   status: RED  Still broken       [ Continue (off) ]   |
|  Notes: [__________________________]   Rating: * * * - -           |
+------------------------------------------------------------------+
```

**Cluster tab**

```
| PRIMARY CLUSTER   root token [****] [copy]   keys [show]          |
|  node-1  10.89.0.2  unsealed  leader   [Terminal] [Stop] [Remove] |
|  node-2  10.89.0.3  (removed)                       [Revive]       |
| SUPPORTING SERVICES                                                |
|  postgres  10.89.0.9:5432   user/pass [show]       [Terminal]      |
```

**Logs tab**

```
| Node [node-1 v]  Stream [audit v]   [Pause] [Scroll lock]          |
| live log lines ...                                                 |
```

### 6.5 Carbon mapping (to confirm with the Carbon MCP)

| UI element | `carbon-components-svelte` component |
|---|---|
| Header and navigation | `Header`, `SideNav` (UI shell) |
| Scenario cards | `ClickableTile` with `Tag` for difficulty and edition |
| Step progress | `ProgressIndicator` |
| Tabs | `Tabs`, `Tab`, `TabContent` |
| Buttons | `Button` (primary, secondary, danger) |
| Status red and green | `InlineNotification` or a status icon with text (not colour alone, R-UI-6) |
| Notes | `TextArea` |
| Rating | `RadioButtonGroup`, or a Carbon-styled rating of five |
| Parameters form | `Form`, `Select`, `NumberInput` |
| Credentials | `CodeSnippet` with copy, with masking |
| Confirmations | `Modal` |
| Reports list | `DataTable` with toolbar batch actions for delete and export |
| Settings | `Form`, `PasswordInput` or `TextArea` for licenses |
| Logs | `CodeSnippet` (multi-line) or a plain scrolling `pre` block |

The exact components, tokens and props are looked up through the Carbon MCP when each page is built (R-UI-3), and this table is updated. Carbon styles are imported by Vite, so nothing is vendored.

### 6.6 Empty and error states

| Place | State | Message and action |
|---|---|---|
| Start-up | Podman not installed or machine stopped | Plain explanation and the command to run. |
| Scenarios | Another run active | Blocking notice with a link to the current run. |
| Settings | Enterprise scenario with no license | The scenario card says a license is needed and links to Settings. |
| Reports | No reports yet | Empty state explaining that reports appear after a scenario ends. |
| Run | Deployment failed | Error, logs, **Retry** and **End scenario** (4.6). |
| Cluster | A node stopped or removed | Greyed card with the correct action (Start or Revive). |
| Logs | Stream not available | Explanation, for example that the node is stopped. |

### 6.7 Window and native behaviour

- One main window. Exports use a native save dialog and quitting during a run uses a native confirmation, both opened by Go.
- The HTML report is shown inside the app in a sandboxed frame (R-REPORT-9).
- Light or dark theme follows the macOS appearance by default, with a manual override in Settings.

---

## 7. Container Runtime

- **Access:** the app talks to Podman's REST API over its unix socket using `net/http`. It does not parse CLI output and needs no extra library. On macOS the socket comes from `podman machine`; the app finds it, and explains clearly if the machine is missing or stopped (R-APP-7).
- **Naming and labels:** every object the run creates carries labels for the app, the run ID and the scenario. Names include the run ID. Cleanup selects by label only (R-RUN-4, R-STATE-3).
- **Network:** one network per run. Containers reach each other by IP on that network. Ports exposed to the host bind to `127.0.0.1` only.
- **Images:** HashiCorp products use the base image (R-RUN-10) plus an installed binary (R-RUN-5). Supporting services (Postgres, MinIO, SoftHSM and so on) use their own images or packages. Images follow normal Podman pull and cache behaviour (R-RUN-9, R-STATE-5).
- **Binary install flow:** `release` ensures the binary is cached (download, verify, cache) -> the runtime starts the base container -> the binary and generated config are copied in -> the product process is started and initialised by the product package.
- **Terminals and logs:** terminals use the API's exec with attach, bridged to the frontend (R-TERM-2). Logs come from container log streams and from log files inside containers, exposed as `LogStream`s by the product.
- **Node actions:** stop, start, remove and **revive** (recreate a removed node from its recorded topology with the same configuration, joining it back to its cluster). Which actions are offered is declared per node by the product and scenario (R-VIEW-6).
- **Teardown:** stop and remove every labelled object for the run, then the network, in dependency order. Teardown is idempotent so it can be repeated after a crash.

**Readiness and health.** Starting a container is not readiness. Each product reports readiness through a check (for example Vault answering `sys/health`, Boundary controllers answering their health endpoint) and the deployment waits on it with a timeout. Readiness failures name the container and show its last log lines.

**Ports.** The app allocates host ports for published services from a high range (for example 19000 upward), checking each is free, and binds to `127.0.0.1` only. Allocated ports appear on the Cluster tab.

**Cleanup edge cases.**
- Teardown continues past a failed removal, records what failed, and reports it. It never leaves the instructor guessing.
- At start-up the app removes labelled leftovers from earlier runs (R-STATE-3) and reports what it removed.
- If Podman itself is stopped, the app says so and offers to retry. It does not try to start the Podman machine on its own.
- Images are never removed (R-STATE-5).

**Podman machine.** On macOS the app checks that `podman machine` exists and is running, and shows the exact command to fix it if not. Memory and CPU for large deployments are the user's choice of Podman machine size, and the docs give guidance for 7 to 8 container scenarios.

---

## 8. Documentation & Marketing Site

The Hugo site in `docs/` (R-DOC-1 to R-DOC-7) has these sections:

| Section | Audience | Content |
|---|---|---|
| **Home / marketing** | Newcomers | What the project is, who it is for, screenshots, how it works, a call to install. |
| **Getting started** | Instructors | Prerequisites (Podman on macOS), install, first run, a quickstart scenario. |
| **User docs** | Instructors | How the UI works, the run page, Cluster and Logs, terminals, reports, settings and licenses, the CLI, troubleshooting. |
| **Scenarios** | Instructors, contributors | One page per scenario, **generated from the scenario folder** (R-SCN-14) so the app and the docs cannot drift apart. Grouped by product. |
| **Contributing** | Contributors | Architecture overview, how to add a scenario, a primitive, a step kind or a product, the Scenario Standard, branch and PR rules, the done checklist, testing and manual-test expectations. |
| **Updates** | Everyone | Release notes. |

Every page carries a feedback link. Docs are part of the definition of done (R-DOC-4).

**Navigation.** Top bar: Home, Getting started, Docs, Scenarios, Contributing, Updates, with search. Each section has a side menu. The Scenarios menu groups by product.

**Theme.** One custom Hugo theme in `docs/themes/s1s/`, styled with Carbon tokens (R-DOC-6), light and dark. Layouts: home (marketing), docs page, scenario page, contributing page, updates list. Shortcodes: callouts, command blocks with copy, step lists, tables.

**Search.** Client-side and offline, built from a Hugo-generated index. No external service.

**Docs generator.** A Go tool (`tools/docgen`) reads the `scenarios/` folders and writes the Markdown for the Scenarios section (R-SCN-14). `task docs` runs the generator and then `hugo`. CI fails if the generated pages are out of date.

**Screenshots.** Produced by the Playwright suite against the fake runtime, so they can be regenerated when the UI changes.

---

## 9. Testing

| Layer | Tooling | What it covers |
|---|---|---|
| Unit (Go) | `go test` | Packages in isolation; the runtime and release layers through fakes (R-TEST-2). |
| Scenario validation | `s1s scenario validate`, run in CI against every built-in scenario | Schema, references to steps and templates, primitives exist, required Standard fields (5.4). |
| Engine | `go test` with a fake runtime and fake product | Step state machine, no-skip rule, rollback, teardown, timing. |
| Report | `go test` | Report data to HTML golden files. |
| Frontend unit | Vitest and Testing Library for Svelte | Components and stores with mocked bindings. |
| End-to-end UI | Playwright (R-TEST-1) | Main path of every UI feature (R-TEST-4) against the Wails dev server with a **fake runtime**, so no containers are needed. |
| Packaged smoke test | Manual checklist on the built `.app` | Window, menu, native dialogs and the real webview, which Playwright does not cover. |
| Scenario end-to-end | Manual, by a maintainer, with real Podman | Required before any scenario is merged (R-CONTRIB-4). |

- Playwright lives in `tests/e2e/`, uses page objects, runs with one command, and fails on browser console errors. How it attaches to the Wails dev server is settled in spike W4.
- A **fake runtime** (an in-memory implementation of the runtime interface with scriptable responses) is a first-class test tool. It is selected by an explicit build tag or environment variable, never by default.
- CI runs: `go vet`, `go test`, scenario validation, `govulncheck`, `svelte-check`, Vitest, the bindings check and the Playwright suite.

**Fixtures.** Scenario test data lives beside the scenario (`fixtures/`). Report golden files live beside the report tests. Playwright fixtures start the app's dev server with a temporary data directory and the fake runtime.

**Fake runtime.** An in-memory implementation of the runtime interface (3.5). It holds containers, networks and files, answers `Exec` from a script keyed by command, and can be told to fail. Engine and UI tests drive scenarios through it, so a whole scenario runs in milliseconds with no Podman.

**Scenario test.** A built-in scenario's optional `scenario_test.go` runs its steps against the fake runtime and asserts the step flow: each verification is red before the fix and green after.

**Screenshot baselines.** Used sparingly, for stable pages only, and regenerated with one command.

---

## 10. Build, Release & Distribution

- **Build:** `task build` runs `npm ci`, the Vite build and Wails packaging on a Mac, producing `Sev1 Situations.app` for Apple Silicon and Intel (R-APP-7). The shell needs cgo (R-LANG-2), so builds run on macOS and cross-compiling is not used. Version, commit and date are stamped with `-ldflags`.
- **Task runner:** a `Taskfile.yml` provides `dev`, `build`, `run`, `test`, `lint`, `vuln`, `test:e2e` and `docs`.
- **Release:** a tag-triggered GitHub Actions workflow on macOS runners builds the bundles, zips them, writes SHA-256 checksums and publishes a GitHub release. A plain workflow around `task release:package` is used, not GoReleaser (spike W1).
- **Install:** download the archive from GitHub releases, unzip it and move `Sev1 Situations.app` to Applications. The repository is access-controlled, so the download needs access to it. `go install` is not supported, because the build needs the frontend and cgo.
- **Signing:** the first releases are **not signed or notarised**. macOS Gatekeeper blocks an unsigned app until the user clears the quarantine attribute (`xattr -cr "Sev1 Situations.app"`), and Getting started documents this. Signing and notarisation are a roadmap item (Section 11).
- **CLI use:** the CLI subcommands run from inside the bundle (`"Sev1 Situations.app/Contents/MacOS/s1s" scenario validate`). The docs show how to alias it. `s1s prereq` runs the Podman checks without opening the window.
- **CI (GitHub Actions):** on every PR and on `main`, on a macOS runner: vet, Go tests, scenario validation, `govulncheck`, `svelte-check`, Vitest, the bindings check and Playwright. A separate workflow releases on tags. Dependabot keeps Go modules, npm packages and actions current.
- **Versioning:** semantic versioning. Release notes feed the docs site Updates section.

---

## 11. Roadmap & Future Goals

### Planned for after the first release

| Item | Notes |
|---|---|
| **Rollback to a known state** | Roll back to the initial or a named checkpoint without redeploying. Decide between Podman snapshots and re-seeding from the scenario definition. The first release offers scenario-defined reset points (R-SCN-6). |
| **More products** | Consul and Nomad first, then others, through the product abstraction (R-SCOPE-2). |
| **Early-access and non-public products** | Products whose binaries are not on `releases.hashicorp.com` and are supplied by the user. Needs a rule for user-supplied binaries, per-product version bumping and a "last verified against" record per scenario. |
| **PDF reports** | A new renderer (R-REPORT-3). |
| **Scenario authoring helpers** | Richer scaffolding and AI-assisted skills for defining, building, branching and opening PRs for scenarios (see 1.12). |
| **Drift detection** | A Drift tab that compares the running environment with how it was when deployment finished, using facts each product reports, so the instructor and the report can show what the trainee changed. |
| **Signed and notarised builds** | Developer ID signing and Apple notarisation so Gatekeeper accepts the app without the `xattr` step. |
| **In-app updates** | Check for and install new releases from inside the app. |

### Not goals

- Windows and Linux support (R-APP-7).
- Formal certification, badges or scoring (Section 2).
- Multiple concurrent scenarios (R-SCN-8).
- Remote or multi-user hosting (R-QUAL-4).

---

## 12. Open Questions

Items that need a decision before the related section can be finalised.

Decisions already made are written into the rules in Section 1 and the sections that follow. Only the open items remain:

- [ ] **Vault Broken KMS** — Confirmed for the first release. A proposed definition is in 5.3.7 and needs your review.
- [ ] **Licence** — None for now (R-SCOPE-6). Choose one before the repository is opened to a wider audience.
- [x] **Organisation path** — Deferred. The project will be moved to the company organisation later, and the module path, install instructions and docs will be updated then.
- [ ] **YAML field names and primitive arguments** — Settled while the `scenario` and `primitive` packages are built (4.4, 4.5).
- [ ] **Terminal transport** — Wails events versus a loopback WebSocket (R-TERM-2). Settled by spike W2.
- [ ] **Terminal library** — ghostty-web or the xterm.js fallback (R-TERM-1). Settled by spike W2.
- [ ] **Carbon with Svelte 5** — Confirm that the chosen `carbon-components-svelte` release supports every component the UI needs (R-UI-2). Settled by spike W3.
- [ ] **Playwright against Wails** — How the suite attaches to the dev server and the fake runtime (Section 9). Settled by spike W4.
- [ ] **Scaffold, single instance and release tooling** — The template name, whether Wails provides single-instance behaviour (R-APP-8), the root `embed.go` layout and GoReleaser versus a plain workflow. Settled by spike W1.
- [ ] **Scenario terminology** — The project is named Sev1 Situations, while the domain objects are still called scenarios. Keep, or rename them to situations everywhere (code, YAML, docs).
