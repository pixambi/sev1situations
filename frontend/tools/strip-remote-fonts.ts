import type { Plugin } from "vite";

// Carbon's stylesheets declare IBM Plex with @font-face rules that load from IBM's CDN. The app
// must work offline and under a strict Content-Security-Policy (R-UI-7, R-LANG-3), so those rules
// are removed at build time and the fonts come from @fontsource, bundled into the app.
//
// A @font-face block holds no nested braces, so a block is the text from "@font-face" to the next "}".
const fontFace = /@font-face\s*\{[^}]*\}/g;
const remoteUrl = /url\(\s*['"]?\s*(?:https?:)?\/\//i;

/** Removes every @font-face rule that loads a font from the network. Local ones are kept. */
export function stripRemoteFontFaces(css: string): string {
  return css.replace(fontFace, (block) => (remoteUrl.test(block) ? "" : block));
}

/** Vite plugin applying stripRemoteFontFaces to Carbon's stylesheets. */
export function stripRemoteFonts(): Plugin {
  return {
    name: "sev1-strip-remote-fonts",
    enforce: "pre",
    transform(code, id) {
      const file = id.split("?")[0];
      if (!file.endsWith(".css") || !file.includes("carbon-components-svelte")) return null;
      return { code: stripRemoteFontFaces(code), map: null };
    },
  };
}
