<script lang="ts">
  import { Button } from "carbon-components-svelte";
  import type { Snapshot } from "../../bindings/github.com/pixambi/sev1situations/internal/engine/models.js";
  import { formatElapsed, runElapsed } from "./elapsed.js";

  interface Props {
    snapshot: Snapshot;
    /** True when some step offers a roll back. */
    canRollBack: boolean;
    busy: boolean;
    onrollback: () => void;
    onend: () => void;
  }
  let { snapshot, canRollBack, busy, onrollback, onend }: Props = $props();

  // The clock only drives the display. The times themselves come from the run.
  let now = $state(Date.now());
  $effect(() => {
    const t = setInterval(() => (now = Date.now()), 1000);
    return () => clearInterval(t);
  });
</script>

<header class="strip">
  <div>
    <h2>{snapshot.ScenarioName}</h2>
    <dl>
      <div><dt>Instructor</dt><dd>{snapshot.Instructor}</dd></div>
      <div><dt>Trainee</dt><dd>{snapshot.Trainee}</dd></div>
      <div><dt>Elapsed</dt><dd>{formatElapsed(runElapsed(snapshot.StartedAt, snapshot.EndedAt, now))}</dd></div>
    </dl>
  </div>
  <div class="buttons">
    {#if canRollBack}
      <Button kind="tertiary" disabled={busy} on:click={onrollback}>Roll back</Button>
    {/if}
    <Button kind="danger-tertiary" disabled={busy} on:click={onend}>End scenario</Button>
  </div>
</header>

<style>
  .strip {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    gap: 1rem;
    margin-bottom: 1.5rem;
  }
  dl {
    display: flex;
    gap: 2rem;
    margin: 0.5rem 0 0;
  }
  dt {
    font-size: 0.75rem;
    color: var(--cds-text-secondary, #525252);
  }
  dd {
    margin: 0;
  }
  .buttons {
    display: flex;
    gap: 0.5rem;
  }
</style>
