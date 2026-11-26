import { describe, it, expect } from "vitest";
import { isWebLink, renderMarkdown } from "./markdown";

describe("renderMarkdown", () => {
  it("renders the formatting a guide uses", () => {
    const html = renderMarkdown("# Title\n\nSome **bold** text and `code`.\n\n- one\n- two\n\n```sh\nvault status\n```\n");
    expect(html).toContain("<h1>Title</h1>");
    expect(html).toContain("<strong>bold</strong>");
    expect(html).toContain("<code>code</code>");
    expect(html).toContain("<li>one</li>");
    expect(html).toContain("<pre><code");
  });

  it("shows raw HTML as text instead of running it", () => {
    const html = renderMarkdown('<script>alert(1)</script><img src=x onerror="alert(2)"><b>hi</b>');
    expect(html).not.toContain("<script");
    expect(html).not.toContain("<img");
    expect(html).not.toContain("<b>");
    expect(html).toContain("&lt;script&gt;");
  });

  it.each(["javascript:alert(1)", "JaVaScRiPt:alert(1)", "vbscript:x", "data:text/html,<script>alert(1)</script>"])(
    "does not turn %s into a link",
    (href) => {
      expect(renderMarkdown(`[click](${href})`)).not.toMatch(/<a /);
    },
  );

  it("keeps ordinary links and marks them noopener", () => {
    const html = renderMarkdown("[Raft](https://developer.hashicorp.com/vault)");
    expect(html).toContain('href="https://developer.hashicorp.com/vault"');
    expect(html).toContain('rel="noopener noreferrer"');
  });

  it("links bare addresses", () => {
    expect(renderMarkdown("see https://example.com now")).toContain('<a href="https://example.com"');
  });
});

describe("isWebLink", () => {
  it.each([
    ["https://example.com/a", true],
    ["http://example.com", true],
    ["javascript:alert(1)", false],
    ["file:///etc/passwd", false],
    ["mailto:a@b.c", false],
    ["/relative", false],
    ["", false],
  ])("%s -> %s", (href, want) => expect(isWebLink(href)).toBe(want));
});
