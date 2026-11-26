<script lang="ts">
  import { Button, DataTable, InlineNotification, OverflowMenu, OverflowMenuItem, Tag } from "carbon-components-svelte";
  import { Delete, Export, List } from "../../bindings/github.com/pixambi/sev1situations/internal/gui/reportservice.js";
  import type { ReportList } from "../../bindings/github.com/pixambi/sev1situations/internal/gui/models.js";
  import type { Summary } from "../../bindings/github.com/pixambi/sev1situations/internal/report/models.js";
  import ConfirmModal from "./ConfirmModal.svelte";
  import { describeReport, whenShort } from "./reports.js";

  interface Props {
    /** Called to open a report in the viewer. */
    onopen: (id: string) => void;
  }
  let { onopen }: Props = $props();

  let list = $state<ReportList | null>(null);
  let loadError = $state("");
  let notice = $state<{ kind: "success" | "error" | "info"; title: string; text: string } | null>(null);
  let deleting = $state<Summary | null>(null);
  let busy = $state(false);

  async function load() {
    try {
      list = await List();
      loadError = "";
    } catch (err) {
      loadError = String(err);
    }
  }
  $effect(() => {
    void load();
  });

  // The actions column has no data of its own, but the table wants a field for every column.
  const rows = $derived((list?.Reports ?? []).map((r) => ({ ...r, actions: "" })));
  const byId = (id: string) => (list?.Reports ?? []).find((r) => r.id === id);

  async function exportAs(id: string, format: "html" | "data") {
    notice = null;
    try {
      const res = await Export(id, format);
      if (!res.Cancelled) notice = { kind: "success", title: "Saved", text: `The report was saved to ${res.Path}.` };
    } catch (err) {
      notice = { kind: "error", title: "Not saved", text: String(err) };
    }
  }

  async function confirmDelete() {
    if (!deleting) return;
    busy = true;
    try {
      await Delete(deleting.id);
      notice = { kind: "success", title: "Deleted", text: `${describeReport(deleting)} was deleted.` };
      deleting = null;
      await load();
    } catch (err) {
      notice = { kind: "error", title: "Not deleted", text: String(err) };
      deleting = null;
      await load(); // it may have been removed already, so the list is read again
    } finally {
      busy = false;
    }
  }

  const headers = [
    { key: "scenarioName", value: "Scenario" },
    { key: "instructor", value: "Instructor" },
    { key: "trainee", value: "Trainee" },
    { key: "startedAt", value: "Date" },
    { key: "result", value: "Outcome" },
    { key: "actions", value: "Actions" },
  ] as const;
</script>

<section aria-labelledby="reports-heading">
  <h2 id="reports-heading">Reports</h2>

  <div role="status" aria-live="polite">
    {#if notice}
      <InlineNotification lowContrast kind={notice.kind} title={notice.title} subtitle={notice.text} on:close={() => (notice = null)} />
    {/if}
  </div>

  {#if loadError}
    <InlineNotification lowContrast hideCloseButton kind="error" title="The reports could not be listed" subtitle={loadError}>
      <svelte:fragment slot="actions">
        <Button size="small" kind="ghost" on:click={load}>Try again</Button>
      </svelte:fragment>
    </InlineNotification>
  {:else if list === null}
    <p>Loading the reports.</p>
  {:else}
    {#each list.Problems ?? [] as problem (problem)}
      <InlineNotification lowContrast kind="warning" title="A report could not be read" subtitle={problem} />
    {/each}

    {#if rows.length === 0}
      <p>There are no reports yet. A report is saved when a scenario ends, even one ended early.</p>
    {:else}
      <DataTable {headers} {rows} size="medium">
        <svelte:fragment slot="cell" let:row let:cell>
          {#if cell.key === "startedAt"}
            {whenShort(cell.value)}
          {:else if cell.key === "result"}
            <Tag type={cell.value === "completed" ? "green" : "gray"}>{cell.value === "completed" ? "Completed" : "Ended early"}</Tag>
          {:else if cell.key === "actions"}
            <div class="actions">
              <Button size="small" kind="tertiary" aria-label="Open the report for {byId(row.id)?.trainee}, {byId(row.id)?.scenarioName}" on:click={() => onopen(row.id)}>Open</Button>
              <OverflowMenu flipped iconDescription="More for {byId(row.id)?.trainee}, {byId(row.id)?.scenarioName}">
                <OverflowMenuItem text="Export the page" on:click={() => exportAs(row.id, "html")} />
                <OverflowMenuItem text="Export the raw data" on:click={() => exportAs(row.id, "data")} />
                <OverflowMenuItem danger text="Delete" on:click={() => (deleting = byId(row.id) ?? null)} />
              </OverflowMenu>
            </div>
          {:else}
            {cell.value}
          {/if}
        </svelte:fragment>
      </DataTable>
    {/if}
  {/if}
</section>

{#if deleting}
  <ConfirmModal
    open
    heading="Delete this report?"
    confirmLabel="Delete report"
    {busy}
    onconfirm={confirmDelete}
    oncancel={() => (deleting = null)}
  >
    <p>This deletes {describeReport(deleting)}. It cannot be undone. Export it first if you want to keep a copy.</p>
  </ConfirmModal>
{/if}

<style>
  .actions {
    display: flex;
    gap: 0.5rem;
    align-items: center;
    justify-content: flex-end;
  }
</style>
