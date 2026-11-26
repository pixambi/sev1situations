<script lang="ts">
  import { Button, Checkbox, InlineNotification, Search, Select, SelectItem } from "carbon-components-svelte";
  import { Events } from "@wailsio/runtime";
  import { tick } from "svelte";
  import { Close, Open, Streams } from "../../bindings/github.com/pixambi/sev1situations/internal/gui/logservice.js";
  import type { StreamInfo } from "../../bindings/github.com/pixambi/sev1situations/internal/gui/models.js";
  import type { NodeView } from "../../bindings/github.com/pixambi/sev1situations/internal/engine/models.js";
  import { LogBuffer, type LogBatch } from "./logbuffer.js";
  import { compile, levelOf, type LevelFilter } from "./logfilter.js";

  interface Props {
    nodes: NodeView[];
  }
  let { nodes }: Props = $props();

  // A removed node has no container, so it has no log. A stopped one still has what it wrote.
  const available = $derived(nodes.filter((n) => n.State !== "removed"));

  let node = $state("");
  let streams = $state<StreamInfo[]>([]);
  let stream = $state("");
  let error = $state("");
  let endedReason = $state<string | null>(null);

  const buffer = new LogBuffer();
  let lines = $state<readonly string[]>([]);
  let paused = $state(false);
  let pending = $state(0);
  let dropped = $state(0);
  const sync = () => {
    lines = buffer.lines;
    pending = buffer.pending;
    dropped = buffer.dropped;
  };

  let text = $state("");
  let regex = $state(false);
  let level = $state<LevelFilter>("all");
  const matcher = $derived(compile({ text, regex, level }));
  const visible = $derived(lines.filter((l) => matcher.keeps(l)));

  // Follow the end of the log until the instructor scrolls up, which locks the view where it is.
  let view: HTMLElement | undefined = $state();
  let follow = $state(true);
  function onscroll() {
    if (view) follow = view.scrollHeight - view.scrollTop - view.clientHeight < 8;
  }
  $effect(() => {
    void visible.length;
    if (follow && view) {
      void tick().then(() => view && (view.scrollTop = view.scrollHeight));
    }
  });

  // Choose a node when there is none or the chosen one has gone.
  $effect(() => {
    if (!available.some((n) => n.Name === node)) node = available[0]?.Name ?? "";
  });

  // Each node offers its own logs.
  $effect(() => {
    const chosen = node;
    streams = [];
    stream = "";
    if (!chosen) return;
    let current = true;
    Streams(chosen)
      .then((list) => {
        if (!current) return;
        streams = list ?? [];
        stream = streams[0]?.ID ?? "";
      })
      .catch((err) => current && (error = String(err)));
    return () => (current = false);
  });

  // Follow one log. The listener is in place before the stream opens, so no line is missed, and both
  // go when the node or the log changes.
  $effect(() => {
    const n = node;
    const s = stream;
    buffer.clear();
    buffer.resume();
    paused = false;
    endedReason = null;
    error = "";
    sync();
    if (!n || !s) return;

    const id = `L${Math.random().toString(36).slice(2, 12)}`;
    const off = Events.On(`log:${id}`, (e) => {
      const batch = e.data as LogBatch;
      if (batch.Lines?.length) buffer.push(batch.Lines);
      if (batch.Ended) endedReason = batch.Reason ?? "";
      sync();
    });
    Open(id, n, s).catch((err) => (error = String(err)));
    return () => {
      off();
      void Close(id);
    };
  });

  function togglePause() {
    if (buffer.paused) buffer.resume();
    else buffer.pause();
    paused = buffer.paused;
    sync();
  }
  function clear() {
    buffer.clear();
    sync();
  }
  function toggleFollow() {
    follow = !follow;
    if (follow && view) view.scrollTop = view.scrollHeight;
  }
</script>

<section aria-labelledby="logs-heading">
  <h3 id="logs-heading" class="visually-hidden">Logs</h3>

  {#if available.length === 0}
    <p>There is no node with a log to show.</p>
  {:else}
    <div class="pickers">
      <Select labelText="Node" bind:selected={node}>
        {#each available as n (n.Name)}<SelectItem value={n.Name} text={n.State === "running" ? n.Name : `${n.Name} (${n.State})`} />{/each}
      </Select>
      <Select labelText="Log" bind:selected={stream} disabled={streams.length === 0}>
        {#each streams as s (s.ID)}<SelectItem value={s.ID} text={s.Label} />{/each}
      </Select>
    </div>

    <div class="filters">
      <Search size="sm" labelText="Filter" placeholder="Filter the lines" bind:value={text} />
      <Checkbox labelText="Regular expression" bind:checked={regex} />
      <Select labelText="Level" bind:selected={level} inline>
        <SelectItem value="all" text="All" />
        <SelectItem value="warnings" text="Warnings and errors" />
        <SelectItem value="errors" text="Errors only" />
      </Select>
    </div>
    {#if matcher.error}
      <InlineNotification lowContrast hideCloseButton kind="warning" title="The filter is not used" subtitle={matcher.error} />
    {/if}

    <div class="controls">
      <Button size="small" kind="tertiary" on:click={togglePause}>{paused ? "Resume" : "Pause"}</Button>
      <Button size="small" kind="tertiary" aria-pressed={!follow} on:click={toggleFollow}>{follow ? "Lock scroll" : "Follow the end"}</Button>
      <Button size="small" kind="ghost" on:click={clear}>Clear</Button>
      <span class="count" role="status">
        {visible.length} of {lines.length} lines{#if paused && pending > 0}, {pending} waiting{/if}{#if dropped > 0}, {dropped} older dropped{/if}
      </span>
    </div>

    {#if error}
      <InlineNotification lowContrast hideCloseButton kind="error" title="This log could not be read" subtitle={error} />
    {/if}
    {#if endedReason !== null}
      <InlineNotification
        lowContrast
        hideCloseButton
        kind="info"
        title="This log has ended"
        subtitle={endedReason || "The node stopped writing, for example because it stopped or was removed."}
      />
    {/if}

    <!-- The log scrolls, so a keyboard user must be able to focus it to scroll it. -->
    <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
    <div class="log" role="log" aria-label="Log lines" tabindex="0" bind:this={view} {onscroll}>
      {#each visible as line, i (i)}
        <div class="line {levelOf(line)}">{#each matcher.parts(line) as part, j (j)}{#if part.match}<mark>{part.text}</mark>{:else}{part.text}{/if}{/each}</div>
      {:else}
        <div class="empty">{lines.length === 0 ? "Waiting for the first line." : "No line matches the filter."}</div>
      {/each}
    </div>
  {/if}
</section>

<style>
  .pickers,
  .filters {
    display: flex;
    flex-wrap: wrap;
    gap: 1rem;
    align-items: flex-end;
    margin-bottom: 1rem;
  }
  .filters :global(.bx--search) {
    min-width: 16rem;
  }
  .controls {
    display: flex;
    gap: 0.5rem;
    align-items: center;
    margin-bottom: 0.75rem;
    flex-wrap: wrap;
  }
  .count {
    font-size: 0.875rem;
    color: var(--cds-text-secondary, #525252);
    margin-left: 0.5rem;
  }
  .log {
    height: 26rem;
    overflow: auto;
    background: var(--cds-layer-01, #f4f4f4);
    padding: 0.5rem 0.75rem;
    font-family: "IBM Plex Mono", monospace;
    font-size: 0.8125rem;
    line-height: 1.4;
  }
  .line {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  /* Level is shown by a marker as well as by colour, and a warning keeps the normal text colour, whose contrast is known. */
  .line.error,
  .line.warn {
    border-left: 3px solid;
    padding-left: 0.5rem;
  }
  .line.error {
    color: var(--cds-support-error, #da1e28);
    border-color: var(--cds-support-error, #da1e28);
  }
  .line.warn {
    border-color: var(--cds-support-warning, #f1c21b);
  }
  .empty {
    color: var(--cds-text-secondary, #525252);
    font-family: "IBM Plex Sans", sans-serif;
  }
  mark {
    background: var(--cds-highlight, #d0e2ff);
    color: inherit;
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
