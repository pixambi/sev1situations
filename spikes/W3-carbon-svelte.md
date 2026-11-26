# W3: Carbon in Svelte

**Result: go with changes.** Every component in 6.5 works with Svelte 5 and Vite, both themes switch, and an axe audit is clean. Two things need work: the fonts load from IBM's CDN, and a few components need a different usage than the Carbon docs show.

Tested 2026-11-26 with `carbon-components-svelte` 0.114.7, `carbon-icons-svelte` 13.18.2, Svelte 5.59.2, Vite 8, `@axe-core/playwright` 4.14.1 and Playwright Chromium, in a scratch copy of `frontend/` (not committed). The Carbon MCP server was not available in this session, so the usage was checked against the installed package source instead.

## What was proved

A page with one of each component, rendered in the `white` (light) and `g100` (dark) themes. Each theme was checked by a Playwright test that opens the modal, waits for its fade-in, and runs axe.

| Component (6.5) | Carbon component | Result |
|---|---|---|
| Header | `Header`, `HeaderNav`, `HeaderNavItem`, `SkipToContent` | renders, skip link present |
| Side nav | `SideNav`, `SideNavItems`, `SideNavLink` | renders, labelled with `aria-label` |
| Tabs | `Tabs`, `Tab`, `TabContent` | renders, switching tabs works |
| Tiles | `Tile`, `ClickableTile` | renders |
| Data table | `DataTable` | renders |
| Modal | `Modal` | opens, has `role="dialog"` |
| Code snippet | `CodeSnippet` | renders, copy button present |
| Progress indicator | `ProgressIndicator`, `ProgressStep` | renders |

- **Themes:** `<Theme bind:theme />` sets the `theme` attribute on `<html>`. Switching between `white` and `g100` at run time worked, and the screenshot of `g100` shows the dark layout.
- **No console errors or warnings** in either theme, and no page errors.
- **axe:** no violations in either theme.
- `svelte-check`: 0 errors and 0 warnings once the usage below is followed.

## Findings that change the definition

- **Fonts come from IBM's CDN.** The Carbon CSS (`css/all.css`) contains `@font-face` rules that point at `https://1.www.s81c.com/common/carbon/plex/...` (IBM Plex Sans and Mono). That breaks R-LANG-3 (everything embedded, no runtime files) and a strict CSP, and offline the app falls back to a system font without any error. Decision needed: self-host IBM Plex with an npm font package such as `@fontsource/ibm-plex-sans` and `@fontsource/ibm-plex-mono` (both SIL OFL), and remove or override Carbon's `@font-face` rules. Verify with the network blocked.
- **Audit after animations finish.** axe run while the modal was still fading in reported colour-contrast failures on the footer buttons (for example `#6399fe` on white). With a 1 s wait the same page had none. The UI tests must wait for the modal to settle before running axe, or they will report false failures.
- **Carbon is written in Svelte 4 style (slots and `on:` events).** It works inside Svelte 5 components that use runes, but follow these rules:
  - Pass header text with slots, not props: `<svelte:fragment slot="company">` and `slot="platform"`. The `company` prop collides with the slot of the same name in the Svelte 5 types and `svelte-check` reports an error.
  - The skip-link slot is named `skipToContent` (camelCase), not `skip-to-content`. With the wrong name nothing renders and nothing warns.
  - Give `SideNav` an `aria-label`, otherwise the page has an unnamed second `navigation` landmark.
- **Size:** the production build with `all.css` is 828 KB of CSS and 332 KB of JS (about 1.1 MB). Acceptable inside a macOS app. Importing only the needed theme CSS instead of `all.css` would reduce it. Not measured.
- **Pin exact versions** (R-UI-4). Carbon has not reached 1.0 (0.114.7), so minor releases can break things.

## Not tested

- **WKWebView.** The test used Chromium. The app runs in the macOS system webview (Safari engine), so the same page must be looked at in the real window during M5.
- **Light and dark following the macOS setting automatically** (the page switched by button only).
- **The icons package in a real screen** beyond installing it.

## Decision and the rule or section it updates

D4 is confirmed for Carbon with Svelte 5, with the font change above. `DEPENDENCIES.md` moves `carbon-components-svelte` to Accepted and adds the font packages as Proposed. Section 6.5 should note the slot usage rules and the self-hosted fonts.
