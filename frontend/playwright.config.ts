import { defineConfig } from "@playwright/test";

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
  },
});
