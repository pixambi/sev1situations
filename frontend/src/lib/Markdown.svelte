<script lang="ts">
  import { Browser } from "@wailsio/runtime";
  import { isWebLink, renderMarkdown } from "./markdown.js";

  interface Props {
    source: string;
  }
  let { source }: Props = $props();

  // renderMarkdown never lets raw HTML or a script address through, so its output is safe to insert.
  const html = $derived(renderMarkdown(source));

  // A link must not take the window away from the app, so web links open in the browser and others do nothing.
  function open(e: MouseEvent) {
    const a = (e.target as Element).closest("a");
    if (!a) return;
    e.preventDefault();
    const href = a.getAttribute("href") ?? "";
    if (isWebLink(href)) void Browser.OpenURL(href);
  }
</script>

<!-- eslint-disable-next-line svelte/no-at-html-tags -->
<div class="markdown" onclick={open} role="presentation">{@html html}</div>

<style>
  .markdown :global(pre) {
    overflow-x: auto;
    padding: 0.75rem;
    background: var(--cds-layer-02, #e0e0e0);
  }
  .markdown :global(code) {
    font-family: "IBM Plex Mono", monospace;
  }
  .markdown :global(h1),
  .markdown :global(h2),
  .markdown :global(h3) {
    margin: 1rem 0 0.5rem;
  }
  .markdown :global(p),
  .markdown :global(ul),
  .markdown :global(ol) {
    margin-bottom: 0.75rem;
  }
  .markdown :global(ul),
  .markdown :global(ol) {
    padding-left: 1.5rem;
  }
</style>
