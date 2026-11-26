import { defineConfig } from "@playwright/test";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

// The tests save settings and may start runs, so the server keeps its data in a folder made for the
// run and never touches the real one. REAL_DEPLOY runs bring their own folder, which holds a cached
// Vault binary, so a run does not download it again.
const dataDir = process.env.S1S_DATA_DIR ?? mkdtempSync(join(tmpdir(), "s1s-e2e-"));
process.env.S1S_DATA_DIR = dataDir;

// Runs against a server build (-tags server), which serves the frontend and the
// real Go services over HTTP. It does not drive the native WebView window.
export default defineConfig({
  testDir: "./e2e",
  use: {
    baseURL: "http://127.0.0.1:8080",
    viewport: { width: 800, height: 600 },
  },
  webServer: {
    command: "go run -tags server,production ./cmd/s1s",
    cwd: "..",
    url: "http://127.0.0.1:8080",
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
    env: { S1S_DATA_DIR: dataDir },
  },
});
