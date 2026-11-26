# W2: In-app terminal

**Result: go with changes.** ghostty-web runs in the macOS webview under a strict CSP, and Wails events carry terminal traffic with room to spare. But ghostty-web 0.4.0 has a bug that kills a terminal on certain output, so the glue must guard against it.

Tested 2026-11-26 on macOS (Apple Silicon) in a real Wails v3.0.0 window with `ghostty-web` 0.4.0 (MIT, no dependencies) and Go 1.27.4. The page measured itself and reported to Go, which printed the results and quit. The spike source is kept for reference in [`w2-terminal/`](w2-terminal) as `.txt` files, because it is not part of the build. It was run in a scratch copy of the repository.

## What was proved

**CSP (R-TERM-6).** Three policies, each a real page load in the real webview:

| Policy | Library loaded how | Result |
|---|---|---|
| `connect-src 'self'` | its default `init()`, which embeds the WASM as a `data:` URL | **Fails.** `connect-src blocked data`. The library's own error says `Failed to fetch WASM: 404`, which hides the cause. |
| `connect-src 'self' data:` | default `init()` | Works, initialised in 90 ms. |
| `connect-src 'self'` | `Ghostty.load(url)` with the WASM served as an app asset | Works, no violations. |

The strictest policy tried also worked with no violations: `default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self'; img-src 'self'; connect-src 'self'`. The terminal needs no inline styles and no `data:` access if the WASM is loaded from an asset.

**Correctness.** Colours, a 2 column wide Japanese string, cursor movement and an overwrite landed in the right cells. `onData` fired with `ls\r` when input was typed, so the input path to Go works. `resize(100, 30)` gave 100x30 and `FitAddon` gave 88x35 for a 900x500 container. The canvas painted: 18,862 non-background pixels in 41 colours, with red and blue present. **Not observed:** the window itself. A screenshot was not possible (no screen-recording permission), so fonts, cursor, selection and clipboard are unchecked.

**Events as the transport (R-TERM-2, D2).** Go emitted, the page wrote into the terminal, nothing was lost:

| Measure | Result |
|---|---|
| Typing round trip over events, 200 samples | median under 1 ms, p95 1 to 2 ms, worst 65 to 70 ms (once) |
| 4 MB streamed as text, 1 KB chunks | 99 ms, 40 MB/s |
| 4 MB, 16 KB chunks | 136 ms, 29 MB/s |
| 4 MB, 64 KB chunks | 95 ms, 42 MB/s |
| 4 MB as base64, 16 KB / 64 KB chunks | 152 ms (26 MB/s) / 98 ms (41 MB/s) |
| Longest gap between animation frames while streaming | 34 to 40 ms |
| Events received | all of them, every run |

Go finished emitting 4 MB in 2 to 15 ms, so emitting does not block. A real exec stream is far slower than this ceiling.

## The bug

Some output makes `ghostty_terminal_write` trap with `Out of bounds memory access`. After that **every write throws, even plain text**, and `reset()` does not recover it. What traps and what does not, each tried on fresh terminals:

| Input (valid or not) | Traps |
|---|---|
| Printable ASCII, C0 control characters, escape-sequence, OSC, DCS and APC soup | never |
| Latin, Greek, Cyrillic, CJK (wide), U+FFFD only, random BMP characters | never |
| One letter followed by thousands of combining marks | never |
| **Many short grapheme clusters** (a letter plus two combining marks), 1000 per write | **always, at the 47th write** |
| **ZWJ emoji sequences**, 300 per write | **always, at the 79th write** |
| Random bytes 0x80 to 0xFF, or random bytes of any value | always, after 22 to 57 writes of 4 KB |

The deterministic write counts point to a fixed pool of multi-codepoint grapheme clusters that fills up after roughly 25,000 to 50,000 clusters and is never reclaimed. Random binary is the realistic trigger, because it decodes to many accidental clusters: an instructor who runs `cat` on a binary, such as the Vault executable itself, kills the terminal within about 100 KB.

A streaming UTF-8 decoder in front of `write` does **not** help: it produces valid text, and valid text with many clusters still traps (6 of 6 trials).

**Mitigation, tested.** Catch the exception, dispose the dead terminal, build a fresh one with a new `Ghostty.load`, and retry the chunk, which a fresh instance accepts. Over 200 chunks of the worst input it rebuilt twice (31 ms and 21 ms), lost no chunk and stayed alive. The screen is cleared at a rebuild, but the shell in the container is unaffected.

## Findings that change the definition

- **R-TERM-1: go with ghostty-web 0.4.0, pinned exactly, with the rebuild guard.** xterm.js stays the documented fallback. The bug is a real defect in the library, not a reason to drop it, because the guard makes it harmless. Revisit if the guard is ever seen to lose output.
- **R-TERM-2: Wails events are the transport.** The WebSocket fallback is not needed. D2 is settled.
- **Send terminal bytes as base64.** An event payload is a JSON string, and a chunk can end in the middle of a multi-byte UTF-8 character. Base64 carries the bytes untouched and cost about 10 percent in the test.
- **R-TERM-6: the CSP is `script-src 'self' 'wasm-unsafe-eval'; connect-src 'self'; style-src 'self'`, with the WASM served as an application asset** (`import url from 'ghostty-web/ghostty-vt.wasm?url'` and `Ghostty.load(url)`). The default `init()` must not be used, because it needs `connect-src data:`.
- **Batching is still right (R-TERM-5)** but is a smoothness choice and not a throughput need.

## Not tested

- A real interactive session against a container: PTY, `htop`-style full-screen programs, resize reaching the process, and a long-running stream. That is T-070, on a running sandbox.
- Selection, copy and paste, IME input and the mouse.
- How the terminal looks. Fonts and the cursor were not seen.
- Several terminals open at once, and their memory use.

## Decision and the rule or section it updates

D2 and the terminal library question are settled as above. R-TERM-1, R-TERM-2 and R-TERM-6 are updated in the definition, and `DEPENDENCIES.md` moves `ghostty-web` to Accepted with the version pinned.
