# Spikes

Short, throwaway investigations that prove or replace the riskiest assumptions before the build depends on them (TASKS.md, M1). Each spike gets one file here, named `<id>-<slug>.md`. This folder is not published (it is outside `docs/`).

Spikes with a result are linked in the table.

| ID | Spike | Status |
|---|---|---|
| S1 | Podman REST API on macOS | Done: go ([result](S1-podman-api.md)) |
| S2 | Binaries on the base image | Not started |
| S4 | YAML library | Not started |
| S7 | Release listing and verification | Done: go ([result](S7-release-listing.md)) |
| W1 | Wails v3 scaffold | Not started |
| W2 | In-app terminal | Not started |
| W3 | Carbon in Svelte | Not started |
| W4 | Playwright against Wails | Not started |

The question and the "done when" for each spike are in the M1 table of [`../TASKS.md`](../TASKS.md).

## Result template

```markdown
# <ID>: <title>

**Result: go | no-go | go with changes.** One sentence.

Tested <date> with <versions>.

## What was proved
## Findings that change the definition
## Decision and the rule or section it updates
```
