import { describe, it, expect } from "vitest";
import { stripRemoteFontFaces } from "./strip-remote-fonts";

const remote = `@font-face{font-family:"IBM Plex Sans";src:url(https://1.www.s81c.com/common/carbon/plex/fonts/IBM-Plex-Sans/fonts/split/woff2/IBMPlexSans-Italic-Latin1.woff2) format("woff2");unicode-range:U+0100}`;
const local = `@font-face{font-family:"IBM Plex Sans";src:url(./files/ibm-plex-sans-latin-400-normal.woff2) format("woff2")}`;

describe("stripRemoteFontFaces", () => {
  it("removes a font that loads from the network", () => {
    expect(stripRemoteFontFaces(`a{color:red}${remote}b{color:blue}`)).toBe("a{color:red}b{color:blue}");
  });

  it("keeps a font that is bundled with the app", () => {
    expect(stripRemoteFontFaces(local)).toBe(local);
  });

  it("removes only the remote rules when both are present", () => {
    expect(stripRemoteFontFaces(remote + local + remote)).toBe(local);
  });

  it.each([
    ["http", `@font-face{src:url(http://example.com/a.woff2)}`],
    ["a quoted URL", `@font-face{src:url("https://example.com/a.woff2")}`],
    ["a protocol-relative URL", `@font-face{src:url(//example.com/a.woff2)}`],
    ["extra spaces", `@font-face { src: url( https://example.com/a.woff2 ) }`],
  ])("recognises %s", (_name, rule) => {
    expect(stripRemoteFontFaces(rule)).toBe("");
  });

  it("leaves other rules and an empty stylesheet alone", () => {
    expect(stripRemoteFontFaces("")).toBe("");
    expect(stripRemoteFontFaces(".x{background:url(https://example.com/i.png)}")).toBe(".x{background:url(https://example.com/i.png)}");
  });
});
