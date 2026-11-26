# Dependencies

Every third-party Go module and every npm package that ships in the app needs a row here before it is added (R-DEP-1). A dependency must be actively maintained and permissively licensed (R-DEP-2). Core Go dependencies must be pure Go. The standard library or the platform must not be enough.

Status: **Proposed** means chosen on paper and not yet proved. **Accepted** means a spike or the build confirmed it. Nothing is accepted yet.

## Go modules

| Module | Used for | Why the standard library is not enough | Status |
|---|---|---|---|
| `github.com/wailsapp/wails/v3` | The native window, services, events and bindings generator (R-APP-2). Imported only by `internal/gui` and `cmd/s1s` (R-LANG-2). | The standard library has no native window or webview. Wails v3.0.0 is stable, uses cgo on macOS and is the chosen shell. Confirm in W1. Its `pkg/updater` provides self-update (checksum verified, GitHub releases provider) and pulls in `golang.org/x/mod` indirectly. | Proposed |
| `go.yaml.in/yaml/v4` | Parsing scenario files (R-SCN-1). | The standard library has no YAML support. Pure Go, Apache-2.0, strict decoding, line numbers on errors and nodes. Confirmed in S4. | Accepted |
| `github.com/coder/websocket` | **Only if** spike W2 shows Wails events cannot carry terminal throughput (R-TERM-2). | The standard library has no server-side WebSocket. | Conditional |

## npm packages (frontend, shipped)

| Package | Used for | Why the platform is not enough | Status |
|---|---|---|---|
| `svelte` | The UI framework (R-LANG-7, R-UI-1). | Chosen frontend framework. | Proposed |
| `@wailsio/runtime` | Calling Go services and receiving events from the frontend. | Required by Wails to reach the backend. | Proposed |
| `carbon-components-svelte` | Carbon components, layout and tokens (R-UI-2). | Provides the Carbon Design System for Svelte. Confirm Svelte 5 support in W3. | Proposed |
| `carbon-icons-svelte` | Carbon iconography (R-UI-2). | Provides the Carbon icon set for Svelte. | Proposed |
| `ghostty-web` | The in-app terminal (R-TERM-1). | A terminal emulator cannot be built from platform APIs. Fallback: `@xterm/xterm`. Confirm in W2. | Proposed |

## npm packages (build and test only)

| Group | Packages | Used for |
|---|---|---|
| Build | `vite`, `@sveltejs/vite-plugin-svelte`, `typescript` | Bundle and type-check the frontend (R-UI-4). |
| Lint and check | `svelte-check`, `eslint` (and plugins if needed) | Static checks in CI. |
| Unit tests | `vitest`, `jsdom`, `@testing-library/svelte`, `@testing-library/jest-dom`, `@testing-library/user-event` | Component and store tests (R-TEST-1). |
| End-to-end tests | `@playwright/test`, `@axe-core/playwright` | UI tests and accessibility audits (R-TEST-1, R-UI-6). |

## Tools (not part of the build output)

| Tool | Used for |
|---|---|
| Podman | The container runtime (R-RUN-1). Required to run the app. |
| Node and npm | Building and testing the frontend. Never needed to run the app (R-UI-4). |
| Wails v3 CLI (`wails3`) | Dev server, bindings generation and packaging. |
| Task | Runs project tasks (`Taskfile.yml`). |
| golangci-lint | Linting, run through `go run` at a pinned version. |
| govulncheck | Vulnerability scanning in CI. |
| Hugo | Builds the docs site (docs only, R-DOC-5). |
