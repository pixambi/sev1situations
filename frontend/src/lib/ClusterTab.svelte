<script lang="ts">
  import { Button, InlineLoading, InlineNotification, Tag, Tile } from "carbon-components-svelte";
  import { Get } from "../../bindings/github.com/pixambi/sev1situations/internal/gui/clusterservice.js";
  import { NodeAction } from "../../bindings/github.com/pixambi/sev1situations/internal/gui/runservice.js";
  import type { ClusterState, NodeCard } from "../../bindings/github.com/pixambi/sev1situations/internal/gui/models.js";
  import ConfirmModal from "./ConfirmModal.svelte";
  import FactRow from "./FactRow.svelte";
  import { actionLabel, nodeTagType } from "./cluster.js";

  interface Props {
    /** Changes whenever the run does, which is when the tab reads the deployment again. */
    version: number;
  }
  let { version }: Props = $props();

  let cluster = $state<ClusterState | null>(null);
  let error = $state("");
  let loading = $state(false);
  let acting = $state("");
  let confirmRemove = $state<NodeCard | null>(null);

  // Only the newest read is kept, so a slow answer cannot replace a newer one.
  let ticket = 0;
  async function load() {
    const mine = ++ticket;
    loading = true;
    try {
      const next = await Get();
      if (mine !== ticket) return;
      cluster = next;
      error = "";
    } catch (err) {
      if (mine === ticket) error = String(err);
    } finally {
      if (mine === ticket) loading = false;
    }
  }

  $effect(() => {
    void version;
    void load();
  });

  async function act(node: string, action: string) {
    acting = `${node}:${action}`;
    error = "";
    try {
      await NodeAction(node, action);
    } catch (err) {
      error = String(err);
    } finally {
      acting = "";
    }
  }

  async function removeConfirmed() {
    const n = confirmRemove;
    if (!n) return;
    await act(n.Name, "remove");
    confirmRemove = null;
  }
</script>

<section aria-labelledby="cluster-heading">
  <h3 id="cluster-heading" class="visually-hidden">Cluster</h3>

  {#if error}
    <InlineNotification lowContrast kind="error" title="The cluster could not be read" subtitle={error} on:close={() => (error = "")}>
      <svelte:fragment slot="actions">
        <Button size="small" kind="ghost" on:click={load}>Try again</Button>
      </svelte:fragment>
    </InlineNotification>
  {/if}

  {#if cluster === null}
    <InlineLoading description="Reading the cluster" />
  {:else if (cluster.Sections ?? []).length === 0}
    <p>There is nothing deployed to show yet.</p>
  {/if}

  {#each cluster?.Sections ?? [] as section (section.Title)}
    <h3>{section.Title}</h3>
    {#if (section.Facts ?? []).length > 0}
      <dl class="facts">
        {#each section.Facts ?? [] as f (f.Label)}
          <FactRow label={f.Label} value={f.Value} secret={f.Secret} copyable={f.Label === "API address"} />
        {/each}
      </dl>
    {/if}

    <ul class="nodes">
      {#each section.Nodes ?? [] as node (node.Name)}
        <li>
          <Tile>
            <div class="inactive" class:grey={node.State !== "running"}>
              <div class="title">
                <h4>{node.Name}</h4>
                <Tag type={nodeTagType(node.State)}>{node.State}</Tag>
              </div>
              <dl class="facts">
                {#if node.IP}<FactRow label="Address" value={node.IP} copyable />{/if}
                {#each node.Details ?? [] as d (d.Label)}
                  <FactRow label={d.Label} value={d.Value} secret={d.Secret} />
                {/each}
              </dl>
            </div>
            {#if (node.Actions ?? []).length > 0}
              <div class="actions">
                {#each node.Actions ?? [] as a (a)}
                  <Button
                    size="small"
                    kind={a === "remove" ? "danger-tertiary" : "tertiary"}
                    disabled={acting !== ""}
                    aria-label="{actionLabel(a)} {node.Name}"
                    on:click={() => (a === "remove" ? (confirmRemove = node) : act(node.Name, a))}
                  >
                    {actionLabel(a)}
                  </Button>
                {/each}
              </div>
            {/if}
            {#if acting.startsWith(node.Name + ":")}<InlineLoading description="Working on {node.Name}" />{/if}
          </Tile>
        </li>
      {/each}
    </ul>
  {/each}

  <Button size="small" kind="ghost" disabled={loading} on:click={load}>Refresh</Button>
</section>

{#if confirmRemove}
  <ConfirmModal
    open
    heading="Remove {confirmRemove.Name}?"
    confirmLabel="Remove node"
    busy={acting !== ""}
    onconfirm={removeConfirmed}
    oncancel={() => (confirmRemove = null)}
  >
    <p>This stops {confirmRemove.Name} and removes its container. Its data is lost. It can be revived afterwards, which recreates it from its configuration.</p>
  </ConfirmModal>
{/if}

<style>
  .facts {
    margin: 0 0 1rem;
  }
  .nodes {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(20rem, 1fr));
    gap: 1rem;
    list-style: none;
    padding: 0;
    margin: 0 0 2rem;
  }
  .title {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 0.5rem;
  }
  .grey {
    opacity: 0.6;
  }
  .actions {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
    margin-top: 0.75rem;
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
