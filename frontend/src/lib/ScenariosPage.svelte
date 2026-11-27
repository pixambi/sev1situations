<script lang="ts">
  import { Button, Dropdown, InlineNotification, Link, Tag, Tile } from "carbon-components-svelte";
  import type { ScenarioList } from "../../bindings/github.com/pixambi/sev1situations/internal/gui/models.js";
  import { difficulties, distinct, groupByProduct, matches, noFilters, productLabel, type Filters } from "./catalogue.js";

  interface Props {
    catalogue: ScenarioList;
    /** True while a run exists. Starting another is blocked (R-SCN-8). */
    runActive: boolean;
    /** False while Podman is not ready. */
    podmanReady: boolean;
    onstart: (id: string) => void;
    onopenrun: () => void;
    onopensettings: () => void;
  }
  let { catalogue, runActive, podmanReady, onstart, onopenrun, onopensettings }: Props = $props();

  let filters: Filters = $state({ ...noFilters });

  const all = { id: "", text: "All" };
  const choices = (values: string[], label: (v: string) => string = (v) => v) => [all, ...values.map((v) => ({ id: v, text: label(v) }))];

  // Go sends an empty list as null when it was never filled, so both are treated as empty.
  const scenarios = $derived(catalogue.Scenarios ?? []);
  const problems = $derived(catalogue.Problems ?? []);

  const products = $derived(choices(distinct(scenarios, (s) => s.Product), productLabel));
  const levels = $derived(choices(difficulties.filter((d) => scenarios.some((s) => s.Difficulty === d)), productLabel));
  const editions = $derived(choices(distinct(scenarios, (s) => s.Editions), productLabel));

  const shown = $derived(scenarios.filter((s) => matches(s, filters)));
  const groups = $derived(groupByProduct(shown));

  function whyNot(needsLicense: boolean): string {
    if (runActive) return "A scenario is already running";
    if (!podmanReady) return "Podman is not ready";
    if (needsLicense) return "A license is needed";
    return "";
  }
</script>

<section aria-labelledby="scenarios-heading">
  <h2 id="scenarios-heading">Scenarios</h2>

  {#if runActive}
    <InlineNotification lowContrast hideCloseButton kind="warning" title="A scenario is running" subtitle="Only one scenario can run at a time. End it to start another.">
      <svelte:fragment slot="actions">
        <Link href="#run" on:click={(e) => { e.preventDefault(); onopenrun(); }}>Go to the current run</Link>
      </svelte:fragment>
    </InlineNotification>
  {/if}

  {#each problems as problem (problem)}
    <InlineNotification lowContrast kind="warning" title="A scenario could not be loaded" subtitle={problem} />
  {/each}

  <div class="filters">
    <Dropdown size="sm" labelText="Product" aria-label="Product" items={products} bind:selectedId={filters.product} />
    <Dropdown size="sm" labelText="Difficulty" aria-label="Difficulty" items={levels} bind:selectedId={filters.difficulty} />
    <Dropdown size="sm" labelText="Edition" aria-label="Edition" items={editions} bind:selectedId={filters.edition} />
  </div>

  {#if scenarios.length === 0}
    <p>No scenarios were found.</p>
  {:else if shown.length === 0}
    <p>No scenario matches these filters.</p>
  {/if}

  {#each groups as group (group.product)}
    <h3>{productLabel(group.product)}</h3>
    <ul class="cards">
      {#each group.scenarios as s (s.ID)}
        {@const blocked = whyNot(s.NeedsLicense)}
        <li>
          <Tile>
            <h4>{s.Name}</h4>
            <p>{s.Summary}</p>
            <div class="foot">
            <div class="tags">
              <Tag type="blue">{productLabel(s.Difficulty)}</Tag>
              {#each s.Editions as edition (edition)}
                <Tag type="gray">{productLabel(edition)}</Tag>
              {/each}
              <Tag type="outline">{s.EstimatedMinutes} min</Tag>
              {#if s.Source === "user"}<Tag type="purple">Yours</Tag>{/if}
              {#if s.NeedsLicense}<Tag type="red">License needed</Tag>{/if}
            </div>
            {#if s.NeedsLicense}
              <p class="hint">
                This scenario needs a {productLabel(s.Product)} Enterprise license.
                <Link href="#settings" on:click={(e) => { e.preventDefault(); onopensettings(); }}>Add it in Settings</Link>.
              </p>
            {/if}
            <Button size="small" disabled={blocked !== ""} title={blocked} on:click={() => onstart(s.ID)}>Start</Button>
            </div>
          </Tile>
        </li>
      {/each}
    </ul>
  {/each}
</section>

<style>
  .filters {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(12rem, 16rem));
    gap: 1rem;
    margin: 1rem 0 2rem;
  }
  .cards {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(18rem, 1fr));
    gap: 1rem;
    list-style: none;
    padding: 0;
    margin: 0 0 2rem;
  }
  /* Every card in a row is as tall as the tallest, with its tags and Start button at the bottom, so a row lines up. */
  .cards li {
    display: flex;
  }
  .cards li :global(.bx--tile) {
    display: flex;
    flex: 1;
    flex-direction: column;
  }
  .foot {
    margin-top: auto;
  }
  .tags {
    margin: 0.75rem 0;
  }
  .hint {
    margin-bottom: 0.75rem;
  }
</style>
