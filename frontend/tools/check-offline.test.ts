import { describe, it, expect, afterEach } from "vitest";
import { spawnSync } from "node:child_process";
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const script = join(__dirname, "check-offline.mjs");
const made: string[] = [];

function dist(files: Record<string, string>): string {
  const dir = mkdtempSync(join(tmpdir(), "offline-"));
  made.push(dir);
  for (const [name, body] of Object.entries(files)) {
    const path = join(dir, name);
    mkdirSync(join(path, ".."), { recursive: true });
    writeFileSync(path, body);
  }
  return dir;
}

const run = (dir: string) => spawnSync("node", [script, dir], { encoding: "utf8" });

afterEach(() => {
  for (const d of made.splice(0)) rmSync(d, { recursive: true, force: true });
});

describe("check-offline", () => {
  it("passes a build that loads only its own files", () => {
    const r = run(dist({
      "index.html": `<link rel="stylesheet" href="/assets/a.css"><script type="module" src="/assets/a.js"></script>`,
      "assets/a.css": `@font-face{src:url(./font.woff2)}body{background:url(data:image/png;base64,AAAA)}`,
      "assets/a.js": `console.log("a link in text: https://example.com/docs")`,
    }));
    expect(r.status).toBe(0);
    expect(r.stdout).toContain("nothing is loaded from the network");
  });

  it.each([
    ["a remote font", { "a.css": `@font-face{src:url(https://1.www.s81c.com/plex.woff2)}` }, "CSS url()"],
    ["a protocol-relative image", { "a.css": `.x{background:url(//example.com/i.png)}` }, "CSS url()"],
    ["a remote @import", { "a.css": `@import "https://fonts.example.com/x.css";` }, "CSS @import"],
    ["a remote script in the page", { "index.html": `<script src="https://cdn.example.com/x.js"></script>` }, "script, link or img"],
    ["a known CDN named in the code", { "a.js": `const u = "https://unpkg.com/x"` }, "refers to unpkg.com"],
  ])("fails on %s", (_name, files, expected) => {
    const r = run(dist(files as Record<string, string>));
    expect(r.status).toBe(1);
    expect(r.stderr).toContain(expected);
  });

  it("fails clearly when there is nothing to scan", () => {
    const r = run(dist({ "readme.txt": "x" }));
    expect(r.status).toBe(2);
    expect(r.stderr).toContain("Build the frontend first");
  });
});
