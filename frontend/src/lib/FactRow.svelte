<script lang="ts">
  import { Button } from "carbon-components-svelte";
  import Copy from "carbon-icons-svelte/lib/Copy.svelte";
  import View from "carbon-icons-svelte/lib/View.svelte";
  import ViewOff from "carbon-icons-svelte/lib/ViewOff.svelte";
  import { Clipboard } from "@wailsio/runtime";

  interface Props {
    label: string;
    value: string;
    /** A credential: masked until the instructor reveals it. */
    secret?: boolean;
    /** Show a copy button, for values worth pasting such as an address. */
    copyable?: boolean;
  }
  let { label, value, secret = false, copyable = false }: Props = $props();

  let revealed = $state(false);
  let copied = $state(false);

  const mask = "•".repeat(12);
  const canCopy = $derived(secret || copyable);

  async function copy() {
    try {
      await Clipboard.SetText(value);
      copied = true;
      setTimeout(() => (copied = false), 1500);
    } catch {
      copied = false;
    }
  }
</script>

<div class="fact">
  <dt>{label}</dt>
  <dd>
    <!-- The value is not in the page while it is masked, so a screen reader and a selection cannot reach it. -->
    <span class="value" class:mono={secret}>{secret && !revealed ? mask : value}</span>
    {#if secret}
      <Button
        size="small"
        kind="ghost"
        icon={revealed ? ViewOff : View}
        iconDescription={`${revealed ? "Hide" : "Show"} ${label}`}
        tooltipPosition="left"
        aria-pressed={revealed}
        on:click={() => (revealed = !revealed)}
      />
    {/if}
    {#if canCopy}
      <Button size="small" kind="ghost" icon={Copy} iconDescription={`Copy ${label}`} tooltipPosition="left" on:click={copy} />
    {/if}
    {#if copied}<span class="copied" role="status">Copied</span>{/if}
  </dd>
</div>

<style>
  .fact {
    display: grid;
    grid-template-columns: 11rem 1fr;
    align-items: center;
    padding: 0.125rem 0;
  }
  dt {
    color: var(--cds-text-secondary, #525252);
    font-size: 0.875rem;
  }
  dd {
    margin: 0;
    display: flex;
    align-items: center;
    gap: 0.25rem;
    min-width: 0;
  }
  .value {
    overflow-wrap: anywhere;
  }
  .mono {
    font-family: "IBM Plex Mono", monospace;
  }
  .copied {
    font-size: 0.75rem;
    color: var(--cds-support-success, #198038);
  }
</style>
