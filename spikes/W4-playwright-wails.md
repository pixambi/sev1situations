# W4: Playwright against Wails

**Result: go with changes.** Playwright drives the real Go services and events with nothing mocked, through the server build. The fake runtime still has to be wired in when the runtime exists.

Tested 2026-11-26 with Wails v3.0.0, Playwright (Chromium) and Go 1.27.4, in a scratch copy of the repository (not committed). The test clicked a button that calls a Go service method through the generated bindings, then checked the returned value and a Go event.

## What was proved

- The suite runs against the **server build**: `go run -tags server,production ./cmd/s1s` (see `frontend/playwright.config.ts`). It serves the compiled frontend and the Go services on `127.0.0.1:8080`. This is what `task test:e2e` does.
- **Service calls** from the browser reach Go with no mock: the generated binding `RunService.Start({...})` posts to `/wails/runtime`, and the typed result came back (`run-vault-sandbox`).
- **Events** from Go reach the browser: `application.Get().Event.Emit("log", LogLine{...})` in the service arrived through `Events.On("log", ...)` over the WebSocket, with the typed payload (`started vault-sandbox`).
- The browser console had **no errors** during the test.
- The test was stable on the first run (about 0.3 s).

## Findings that change the definition

- **Use the server build, not `task dev`.** The Vite dev server on port 9245 serves only the frontend files. Opened in a normal browser it has no Wails service bindings. The server build is the supported way to give a browser the Go side.
- **The fake runtime is chosen by configuration at start-up.** The Playwright `webServer` block can set an environment variable (for example `S1S_RUNTIME=fake`), which the wiring in `internal/app` reads to pick the fake runtime (R-TEST-2). This does not exist yet: it is part of T-011 and the shell work in M5. The spike proves the transport only.
- **Server mode never opens a native window,** so menus, the updater window, native dialogs and the tray are not covered. They need a manual pass (Section 9). The `//go:build !server` split for the updater already follows this.
- **Services should hold the `*application.App`** (passed in at construction) rather than call `application.Get()`. The spike used `Get()` only for brevity.
- **Browser access limits:** server mode checks the request `Host` and `Origin` (loopback only, same origin). Playwright's `baseURL` of `http://127.0.0.1:8080` passes. A test that uses another hostname would get HTTP 403.
- **Loopback listener in tests only (R-APP-3).** The server build opens a listener on `127.0.0.1:8080`. This is allowed because it exists only in the test build, but the e2e job must not run two suites at once on the same port.

## Decision and the rule or section it updates

Section 9 (testing) holds as written: Playwright runs against the Wails server build with the fake runtime, and native shell features are tested by hand. Add `S1S_RUNTIME=fake` (or equivalent) to the plan for T-011.
