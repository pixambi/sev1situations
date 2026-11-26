<script lang="ts">
  import { Button, ContentSwitcher, Switch } from "carbon-components-svelte";
  import TerminalPane from "./TerminalPane.svelte";

  export interface Session {
    id: string;
    node: string;
  }

  interface Props {
    sessions: Session[];
    /** The ID of the terminal on show. */
    selected: string;
    /** True while this tab is on screen. */
    visible: boolean;
    onselect: (id: string) => void;
    onclose: (id: string) => void;
  }
  let { sessions, selected, visible, onselect, onclose }: Props = $props();

  // Two terminals on one node are told apart by a number.
  const labels = $derived(
    sessions.map((s, i) => {
      const same = sessions.filter((o) => o.node === s.node);
      return same.length > 1 ? `${s.node} (${same.indexOf(s) + 1})` : s.node;
    }),
  );
  const index = $derived(Math.max(0, sessions.findIndex((s) => s.id === selected)));
</script>

<section aria-labelledby="terminals-heading">
  <h3 id="terminals-heading" class="visually-hidden">Terminals</h3>

  {#if sessions.length === 0}
    <p>Open a terminal from a node's card on the Cluster tab.</p>
  {:else}
    <div class="bar">
      <nav aria-label="Open terminals">
        <ContentSwitcher selectedIndex={index} on:change={(e) => onselect(sessions[e.detail].id)}>
          {#each labels as label (label)}<Switch text={label} />{/each}
        </ContentSwitcher>
      </nav>
      <Button size="small" kind="danger-tertiary" on:click={() => onclose(sessions[index].id)}>Close terminal</Button>
    </div>
  {/if}

  <!-- Every terminal stays in the page while another is on show, so its session and its screen are kept. -->
  {#each sessions as s (s.id)}
    <div hidden={s.id !== sessions[index]?.id}>
      <TerminalPane id={s.id} node={s.node} visible={visible && s.id === sessions[index]?.id} onclose={() => onclose(s.id)} />
    </div>
  {/each}
</section>

<style>
  .bar {
    display: flex;
    gap: 1rem;
    align-items: center;
    margin-bottom: 0.75rem;
    flex-wrap: wrap;
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
