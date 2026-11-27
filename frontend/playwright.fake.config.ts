import { defineConfig } from "@playwright/test";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

// The same server build as the main suite, but on a fake runtime, a fake product and a folder of test scenarios, so the run
// page can be covered without containers. The fake exists only in the server build, never in a release.
const dataDir = mkdtempSync(join(tmpdir(), "s1s-e2e-fake-"));
const fixtures = resolve(process.cwd(), "e2e/fixtures");

export default defineConfig({
  testDir: "./e2e/fake",
  workers: 1,
  use: { baseURL: "http://127.0.0.1:8080", viewport: { width: 1000, height: 800 } },
  webServer: {
    command: "go run -tags server,production ./cmd/s1s",
    cwd: "..",
    url: "http://127.0.0.1:8080",
    reuseExistingServer: false,
    timeout: 120_000,
    env: { S1S_DATA_DIR: dataDir, S1S_E2E_FIXTURES: fixtures },
  },
});
