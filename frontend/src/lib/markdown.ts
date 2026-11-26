import MarkdownIt from "markdown-it";

// Guidance is written by scenario authors, including users, so it is rendered as untrusted
// text (R-QUAL-5). Raw HTML in it is shown as text, not run, and markdown-it refuses links
// that use javascript:, vbscript: and most data: addresses.
const md = new MarkdownIt({ html: false, linkify: true, breaks: false });

// Every link opens outside the app, and says so to the browser engine.
const defaultLink = md.renderer.rules.link_open ?? ((tokens, i, options, _env, self) => self.renderToken(tokens, i, options));
md.renderer.rules.link_open = (tokens, i, options, env, self) => {
  tokens[i].attrSet("rel", "noopener noreferrer");
  return defaultLink(tokens, i, options, env, self);
};

/** Renders Markdown to HTML that is safe to insert into the page. */
export function renderMarkdown(source: string): string {
  return md.render(source);
}

/** Whether a link may be opened in the user's browser. Only web addresses are. */
export function isWebLink(href: string): boolean {
  try {
    const u = new URL(href);
    return u.protocol === "https:" || u.protocol === "http:";
  } catch {
    return false;
  }
}
