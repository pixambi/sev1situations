// Fails if the built frontend would fetch anything from the network when it runs (R-UI-7).
// It scans the stylesheets and the HTML in dist for remote URLs, and every file for the CDN hosts
// Carbon is known to use. Links the user clicks, in the page text, are not loads and are ignored.
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, extname } from "node:path";

const root = process.argv[2] ?? "dist";
const knownCDNs = ["s81c.com", "fonts.googleapis.com", "fonts.gstatic.com", "cdnjs.cloudflare.com", "unpkg.com", "cdn.jsdelivr.net"];
const loads = [
  { name: "CSS url()", re: /url\(\s*['"]?\s*(?:https?:)?\/\/[^)\s'"]+/gi, exts: [".css", ".html"] },
  { name: "CSS @import", re: /@import\s+(?:url\()?\s*['"]?(?:https?:)?\/\/[^)\s'";]+/gi, exts: [".css", ".html"] },
  { name: "script, link or img from the network", re: /<(?:script|link|img|iframe)\b[^>]*\b(?:src|href)=["'](?:https?:)?\/\/[^"']+/gi, exts: [".html"] },
];

function* files(dir) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) yield* files(p);
    else yield p;
  }
}

const problems = [];
let scanned = 0;
for (const file of files(root)) {
  const ext = extname(file);
  if (![".css", ".html", ".js", ".mjs"].includes(ext)) continue;
  scanned++;
  const text = readFileSync(file, "utf8");
  for (const { name, re, exts } of loads) {
    if (!exts.includes(ext)) continue;
    for (const m of text.matchAll(re)) problems.push(`${file}: ${name}: ${m[0].slice(0, 100)}`);
  }
  for (const host of knownCDNs) {
    if (text.includes(host)) problems.push(`${file}: refers to ${host}`);
  }
}

if (scanned === 0) {
  console.error(`check-offline: nothing to scan in ${root}. Build the frontend first.`);
  process.exit(2);
}
if (problems.length > 0) {
  console.error(`check-offline: the build would use the network (${problems.length} problem(s)):`);
  for (const p of problems) console.error("  " + p);
  process.exit(1);
}
console.log(`check-offline: ${scanned} files scanned, nothing is loaded from the network.`);
