import { defineConfig } from "vitest/config";
import { svelte } from "@sveltejs/vite-plugin-svelte";

// Separate from vite.config.ts so unit tests don't load the Wails binding plugin.
export default defineConfig({
  plugins: [svelte()],
  resolve: {
    // Svelte 5 resolves to its server build under Node unless told otherwise.
    conditions: ["browser"],
  },
  test: {
    environment: "jsdom",
    include: ["src/**/*.test.ts", "tools/**/*.test.ts"],
    setupFiles: ["./vitest.setup.ts"],
  },
});
