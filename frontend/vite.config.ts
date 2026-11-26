import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import wails from "@wailsio/runtime/plugins/vite";
import { stripRemoteFonts } from "./tools/strip-remote-fonts";

// https://vitejs.dev/config/
export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  // stripRemoteFonts drops Carbon's CDN font rules, so the app loads nothing from the network.
  plugins: [stripRemoteFonts(), svelte(), wails("./bindings")],
});
