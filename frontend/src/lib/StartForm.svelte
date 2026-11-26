<script lang="ts">
  import { Button, Form, InlineLoading, InlineNotification, NumberInput, Select, SelectItem, TextInput } from "carbon-components-svelte";
  import { Get, Versions } from "../../bindings/github.com/pixambi/sev1situations/internal/gui/scenarioservice.js";
  import { Start } from "../../bindings/github.com/pixambi/sev1situations/internal/gui/runservice.js";
  import type { ScenarioDetail } from "../../bindings/github.com/pixambi/sev1situations/internal/gui/models.js";
  import { run, runActive } from "./run.svelte.js";
  import { editionFor, initialValues, maxNameLength, paramsToSend, validate, type Values } from "./startform.js";
  import { productLabel } from "./catalogue.js";

  interface Props {
    scenarioId: string;
    /** Called when a run exists, including one whose deployment failed. The run page takes over. */
    onstarted: () => void;
    oncancel: () => void;
    onopensettings: () => void;
  }
  let { scenarioId, onstarted, oncancel, onopensettings }: Props = $props();

  let detail = $state<ScenarioDetail | null>(null);
  let loadError = $state("");
  let instructor = $state("");
  let trainee = $state("");
  let values: Values = $state({});

  let versions: string[] = $state([]);
  let versionsError = $state("");
  let versionsLoading = $state(false);

  let errors: Record<string, string> = $state({});
  let deploying = $state(false);
  let startError = $state("");

  const params = $derived(detail?.Parameters ?? []);
  const versionParam = $derived(params.find((p) => p.Type === "version"));
  const edition = $derived(detail ? editionFor(params, values, detail.Editions ?? []) : "");
  // Only an enterprise edition needs the license, and only when none is saved.
  const missingLicense = $derived(detail !== null && !detail.Licensed && edition === "enterprise");

  async function load() {
    loadError = "";
    try {
      const d = await Get(scenarioId);
      instructor = d.Instructor;
      values = initialValues(d.Parameters ?? []);
      detail = d;
    } catch (err) {
      loadError = String(err);
    }
  }

  // The versions depend on the edition, so they are listed again when it changes. An answer
  // for an edition that is no longer chosen is ignored.
  let ticket = 0;
  async function loadVersions(product: string, forEdition: string) {
    const mine = ++ticket;
    versionsLoading = true;
    versionsError = "";
    try {
      const list = (await Versions(product, forEdition)) ?? [];
      if (mine !== ticket) return;
      versions = list;
      if (versionParam && !list.includes(String(values[versionParam.ID]))) values[versionParam.ID] = list[0] ?? "";
    } catch (err) {
      if (mine !== ticket) return;
      versions = [];
      versionsError = String(err);
    } finally {
      if (mine === ticket) versionsLoading = false;
    }
  }

  $effect(() => {
    void load();
  });
  $effect(() => {
    if (versionParam && edition) void loadVersions(versionParam.Product, edition);
  });

  const progress = $derived(
    (run.snapshot?.Events ?? []).filter((e) => e.Kind === "deploy-progress").map((e) => e.Message),
  );

  async function submit(e: Event) {
    e.preventDefault();
    if (!detail || deploying) return;
    errors = validate({ instructor, trainee }, params, values);
    if (Object.keys(errors).length > 0 || missingLicense) return;

    deploying = true;
    startError = "";
    try {
      await Start({ ScenarioID: detail.ID, Instructor: instructor.trim(), Trainee: trainee.trim(), Params: paramsToSend(values) });
    } catch (err) {
      startError = String(err);
    } finally {
      deploying = false;
    }
    // A deployment that failed leaves its run so it can be retried or ended, and the run page shows that.
    if (runActive(run.snapshot)) onstarted();
  }
</script>

<section aria-labelledby="start-heading">
  {#if loadError}
    <InlineNotification lowContrast hideCloseButton kind="error" title="This scenario could not be opened" subtitle={loadError} />
    <Button kind="secondary" on:click={oncancel}>Back to scenarios</Button>
  {:else if detail === null}
    <InlineLoading description="Loading the scenario" />
  {:else}
    <h2 id="start-heading">Start {detail.Name}</h2>
    <p>{detail.Summary}</p>

    {#if (detail.Objectives ?? []).length > 0}
      <h3>Objectives</h3>
      <ul class="plain">
        {#each detail.Objectives ?? [] as o (o)}<li>{o}</li>{/each}
      </ul>
    {/if}

    <Form on:submit={submit} novalidate>
      <fieldset disabled={deploying}>
        <legend>Names</legend>
        <TextInput
          labelText="Instructor's name"
          bind:value={instructor}
          maxlength={maxNameLength}
          invalid={!!errors.instructor}
          invalidText={errors.instructor}
        />
        <TextInput
          labelText="Trainee's name"
          bind:value={trainee}
          maxlength={maxNameLength}
          invalid={!!errors.trainee}
          invalidText={errors.trainee}
        />
      </fieldset>

      {#if params.length > 0}
        <fieldset disabled={deploying}>
          <legend>Settings</legend>
          {#each params as p (p.ID)}
            {#if p.Type === "int"}
              <NumberInput
                labelText={p.Label}
                helperText={p.Help}
                min={p.Min}
                max={p.Max || undefined}
                bind:value={values[p.ID] as number}
                invalid={!!errors[p.ID]}
                invalidText={errors[p.ID]}
              />
            {:else if p.Type === "edition"}
              <Select labelText={p.Label} helperText={p.Help} bind:selected={values[p.ID] as string} invalid={!!errors[p.ID]} invalidText={errors[p.ID]}>
                {#each p.Options ?? [] as o (o)}<SelectItem value={o} text={productLabel(o)} />{/each}
              </Select>
            {:else}
              <Select
                labelText={p.Label}
                helperText={versionsLoading ? "Loading versions" : p.Help}
                disabled={versionsLoading || versions.length === 0}
                bind:selected={values[p.ID] as string}
                invalid={!!errors[p.ID]}
                invalidText={errors[p.ID]}
              >
                {#each versions as v (v)}<SelectItem value={v} text={v} />{/each}
              </Select>
              {#if versionsError}
                <InlineNotification lowContrast hideCloseButton kind="error" title="No versions" subtitle={versionsError}>
                  <svelte:fragment slot="actions">
                    <Button size="small" kind="ghost" on:click={() => loadVersions(p.Product, edition)}>Try again</Button>
                  </svelte:fragment>
                </InlineNotification>
              {/if}
            {/if}
          {/each}
        </fieldset>
      {/if}

      {#if missingLicense}
        <InlineNotification lowContrast hideCloseButton kind="warning" title="A license is needed" subtitle="The Enterprise edition needs a license, and none is saved.">
          <svelte:fragment slot="actions">
            <Button size="small" kind="ghost" on:click={onopensettings}>Add it in Settings</Button>
          </svelte:fragment>
        </InlineNotification>
      {/if}

      {#if startError}
        <InlineNotification lowContrast hideCloseButton kind="error" title="The scenario could not be started" subtitle={startError} />
      {/if}

      {#if deploying}
        <InlineLoading description={progress.length > 0 ? "Deploying" : "Starting the deployment"} />
        {#if progress.length > 0}
          <ol class="plain" aria-label="Deployment progress">
            {#each progress as line, i (i)}<li>{line}</li>{/each}
          </ol>
        {/if}
      {/if}

      <div class="actions">
        <Button type="submit" disabled={deploying || missingLicense || (versionParam !== undefined && versions.length === 0)}>Deploy</Button>
        <Button kind="secondary" disabled={deploying} on:click={oncancel}>Cancel</Button>
      </div>
    </Form>
  {/if}
</section>

<style>
  fieldset {
    border: 0;
    padding: 0;
    margin: 0 0 1.5rem;
    max-width: 28rem;
  }
  legend {
    font-weight: 600;
    margin-bottom: 0.75rem;
  }
  fieldset :global(.bx--form-item) {
    margin-bottom: 1rem;
  }
  .plain {
    list-style: none;
    padding: 0;
    margin: 0.5rem 0 1.5rem;
  }
  .actions {
    display: flex;
    gap: 1rem;
    margin-top: 1rem;
  }
</style>
