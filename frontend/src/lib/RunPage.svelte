<script lang="ts">
  import { Accordion, AccordionItem, Button, InlineLoading, InlineNotification, ProgressIndicator, ProgressStep, Select, SelectItem, Tab, TabContent, Tabs } from "carbon-components-svelte";
  import { Continue, End, Retry, Rollback, RunAction, SaveNotes, SetRating, Verify } from "../../bindings/github.com/pixambi/sev1situations/internal/gui/runservice.js";
  import type { Snapshot } from "../../bindings/github.com/pixambi/sev1situations/internal/engine/models.js";
  import ConfirmModal from "./ConfirmModal.svelte";
  import RunHeader from "./RunHeader.svelte";
  import StepCard from "./StepCard.svelte";
  import ClusterTab from "./ClusterTab.svelte";
  import LogsTab from "./LogsTab.svelte";

  interface Props {
    snapshot: Snapshot;
  }
  let { snapshot }: Props = $props();

  // What is under way here, for the buttons. Go also reports a step that is applying, in the snapshot.
  let pending = $state("");
  let failure = $state("");
  const busy = $derived(pending !== "" || snapshot.Status === "deploying");

  const steps = $derived(snapshot.Steps ?? []);
  const deploying = $derived(snapshot.Status === "deploying");
  const deployFailed = $derived(snapshot.Status === "deploy-failed");
  const finished = $derived(snapshot.Status === "finished");
  const sandbox = $derived(steps.length === 0);
  const progress = $derived((snapshot.Events ?? []).filter((e) => e.Kind === "deploy-progress").map((e) => e.Message));
  const active = $derived(snapshot.ActiveStep >= 0 ? steps[snapshot.ActiveStep] : undefined);
  const done = $derived(steps.filter((s) => s.State === "complete"));
  const rollbackTargets = $derived(steps.filter((s) => s.CanRollBack));

  /** Runs one operation, shows what went wrong in plain words, and always clears the busy state. */
  async function perform(label: string, op: () => Promise<unknown>) {
    pending = label;
    failure = "";
    try {
      await op();
    } catch (err) {
      failure = String(err);
    } finally {
      pending = "";
    }
  }

  let confirming: "end" | "rollback" | "" = $state("");
  let rollbackTo = $state("");
  function askRollback() {
    rollbackTo = rollbackTargets.at(-1)?.ID ?? "";
    confirming = "rollback";
  }
  async function confirm() {
    const what = confirming;
    await perform(what === "end" ? "ending" : "rolling back", () => (what === "end" ? End() : Rollback(rollbackTo)));
    if (failure === "") confirming = "";
  }
  const target = $derived(steps.find((s) => s.ID === rollbackTo));
</script>

<section aria-labelledby="run-heading">
  <h2 id="run-heading" class="visually-hidden">Current run</h2>
  <RunHeader {snapshot} canRollBack={rollbackTargets.length > 0} {busy} onrollback={askRollback} onend={() => (confirming = "end")} />

  {#if failure}
    <InlineNotification lowContrast kind="error" title="That did not work" subtitle={failure} on:close={() => (failure = "")} />
  {/if}

  {#if deploying}
    <InlineLoading description={progress.length > 0 ? "Deploying" : "Starting the deployment"} />
    {#if progress.length > 0}
      <ol class="plain" aria-label="Deployment progress">
        {#each progress as line, i (i)}<li>{line}</li>{/each}
      </ol>
    {/if}
  {:else if deployFailed}
    <InlineNotification lowContrast hideCloseButton kind="error" title="The deployment failed" subtitle={snapshot.DeployError}>
      <svelte:fragment slot="actions">
        <Button size="small" disabled={busy} on:click={() => perform("retrying", () => Retry())}>Retry</Button>
      </svelte:fragment>
    </InlineNotification>
    <p>Retry builds the environment again. End scenario removes whatever was created.</p>
  {:else}
    <Tabs>
      <Tab label="Steps" />
      <Tab label="Cluster" />
      <Tab label="Logs" />
      <svelte:fragment slot="content">
        <TabContent>
            {#if sandbox}
              <InlineNotification lowContrast hideCloseButton kind="info" title="This is an open sandbox" subtitle="It has no steps. Explore, then end the scenario when you are done." />
            {:else}
              <ProgressIndicator spaceEqually preventChangeOnClick>
                <ProgressStep complete label="Start" />
                {#each steps as s (s.ID)}
                  <ProgressStep
                    complete={s.State === "complete"}
                    current={s.State === "active" || s.State === "applying" || s.State === "applied"}
                    disabled={s.State === "locked"}
                    label={s.Title}
                  />
                {/each}
              </ProgressIndicator>

              {#if finished}
                <InlineNotification lowContrast hideCloseButton kind="success" title="Every step is complete" subtitle="End the scenario to finish." />
              {/if}

              {#if active}
                {#key active.ID}
                  <StepCard
                    step={active}
                    {busy}
                    onapply={() => perform("applying", () => RunAction(active.ID))}
                    onverify={() => perform("verifying", () => Verify(active.ID))}
                    oncontinue={() => perform("continuing", () => Continue(active.ID))}
                    onnotes={(text) => perform("saving", () => SaveNotes(active.ID, text))}
                    onrating={(n) => perform("saving", () => SetRating(active.ID, n))}
                  />
                {/key}
              {/if}

              {#if done.length > 0}
                <h3>Completed steps</h3>
                <Accordion>
                  {#each done as s (s.ID)}
                    <AccordionItem title={s.Title}>
                      <p>{s.Last?.Message ?? "Complete"}</p>
                      {#if s.Notes}<p><strong>Notes:</strong> {s.Notes}</p>{/if}
                      {#if s.Rating}<p><strong>Rating:</strong> {s.Rating} of 5</p>{/if}
                    </AccordionItem>
                  {/each}
                </Accordion>
              {/if}
            {/if}
        </TabContent>
        <TabContent>
          <!-- The tab reads the deployment again whenever the run changes, which covers a node action finishing. -->
          <ClusterTab version={(snapshot.Events ?? []).length} />
        </TabContent>
        <TabContent>
          <LogsTab nodes={snapshot.Nodes ?? []} />
        </TabContent>
      </svelte:fragment>
    </Tabs>
  {/if}
</section>

<!-- A modal is only in the page while it is open, so a closed one leaves no hidden buttons for a screen reader. -->
{#if confirming === "end"}
  <ConfirmModal
    open
    heading="End this scenario?"
    confirmLabel="End scenario"
    busy={pending === "ending"}
    onconfirm={confirm}
    oncancel={() => (confirming = "")}
  >
    <p>This stops the scenario and removes its containers and network. It cannot be undone.</p>
  </ConfirmModal>
{:else if confirming === "rollback"}
  <ConfirmModal
    open
    heading="Roll back?"
    confirmLabel="Roll back"
    busy={pending === "rolling back"}
    confirmDisabled={rollbackTo === ""}
    onconfirm={confirm}
    oncancel={() => (confirming = "")}
  >
    <Select labelText="Roll back to the start of" bind:selected={rollbackTo}>
      {#each rollbackTargets as s (s.ID)}<SelectItem value={s.ID} text={s.Title} />{/each}
    </Select>
    <p>
      The environment is rebuilt{target ? ` and the steps before "${target.Title}" are replayed` : ""}. Anything the trainee changed since is lost.
    </p>
  </ConfirmModal>
{/if}

<style>
  .plain {
    list-style: none;
    padding: 0;
    margin: 0.5rem 0 1.5rem;
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
