<script lang="ts">
  import { Button, InlineLoading, InlineNotification } from "carbon-components-svelte";
  import { Export, Page } from "../../bindings/github.com/pixambi/sev1situations/internal/gui/reportservice.js";

  interface Props {
    id: string;
    onback: () => void;
  }
  let { id, onback }: Props = $props();

  let src = $state("");
  let error = $state("");
  let notice = $state<{ kind: "success" | "error"; text: string } | null>(null);

  $effect(() => {
    const mine = id;
    src = "";
    error = "";
    Page(mine)
      .then((address) => mine === id && (src = address))
      .catch((err) => mine === id && (error = String(err)));
  });

  async function exportAs(format: "html" | "data") {
    notice = null;
    try {
      const res = await Export(id, format);
      if (!res.Cancelled) notice = { kind: "success", text: `Saved to ${res.Path}.` };
    } catch (err) {
      notice = { kind: "error", text: String(err) };
    }
  }
</script>

<section aria-labelledby="viewer-heading">
  <h2 id="viewer-heading" class="visually-hidden">Report</h2>
  <div class="bar">
    <Button kind="ghost" size="small" on:click={onback}>Back to the reports</Button>
    <Button kind="tertiary" size="small" disabled={error !== ""} on:click={() => exportAs("html")}>Export the page</Button>
    <Button kind="tertiary" size="small" disabled={error !== ""} on:click={() => exportAs("data")}>Export the raw data</Button>
  </div>

  <div role="status" aria-live="polite">
    {#if notice}
      <InlineNotification lowContrast kind={notice.kind} title={notice.kind === "success" ? "Saved" : "Not saved"} subtitle={notice.text} on:close={() => (notice = null)} />
    {/if}
  </div>

  {#if error}
    <InlineNotification lowContrast hideCloseButton kind="error" title="This report cannot be shown" subtitle={error} />
  {:else if src === ""}
    <InlineLoading description="Opening the report" />
  {:else}
    <!-- The report is its own page with its own strict policy. The frame has no permissions at all: no script,
         no forms, no access to the application (R-REPORT-9, R-QUAL-5). -->
    <iframe class="frame" title="The report" sandbox="" {src}></iframe>
  {/if}
</section>

<style>
  .bar {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
    margin-bottom: 1rem;
  }
  .frame {
    width: 100%;
    height: 70vh;
    min-height: 24rem;
    border: 1px solid var(--cds-border-subtle, #c6c6c6);
    background: #fff;
  }
  .visually-hidden {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    white-space: nowrap;
  }
</style>
