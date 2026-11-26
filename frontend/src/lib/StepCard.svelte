<script lang="ts">
  import { Accordion, AccordionItem, Button, InlineLoading, InlineNotification, RadioButton, RadioButtonGroup, TextArea } from "carbon-components-svelte";
  import { Guide } from "../../bindings/github.com/pixambi/sev1situations/internal/gui/runservice.js";
  import type { StepGuide } from "../../bindings/github.com/pixambi/sev1situations/internal/gui/models.js";
  import type { StepView } from "../../bindings/github.com/pixambi/sev1situations/internal/engine/models.js";
  import Markdown from "./Markdown.svelte";

  interface Props {
    step: StepView;
    /** True while any operation on the run is under way, which turns the buttons off. */
    busy: boolean;
    onapply: () => void;
    onverify: () => void;
    oncontinue: () => void;
    onnotes: (text: string) => void;
    onrating: (rating: number) => void;
  }
  let { step, busy, onapply, onverify, oncontinue, onnotes, onrating }: Props = $props();

  // The page keys this component by the step, so the notes start from the saved text once per step
  // and a snapshot that arrives while the instructor is typing does not overwrite it.
  // svelte-ignore state_referenced_locally
  let notes = $state(step.Notes);

  let guide = $state<StepGuide | null>(null);
  let guideError = $state("");
  $effect(() => {
    const id = step.ID;
    Guide(id)
      .then((g) => (guide = g))
      .catch((err) => (guideError = String(err)));
  });

  const applying = $derived(step.State === "applying");
  const last = $derived(step.Last);
  const isBreakage = $derived(step.Kind === "breakage");
</script>

<article aria-labelledby="step-{step.ID}">
  <h3 id="step-{step.ID}">{step.Title}</h3>

  {#if guideError}
    <InlineNotification lowContrast hideCloseButton kind="warning" title="No guidance" subtitle={guideError} />
  {:else if guide}
    {#if guide.Briefing}<Markdown source={guide.Briefing} />{/if}
    {#if guide.Hints || guide.Solution || guide.References}
      <Accordion>
        {#if guide.Hints}<AccordionItem title="Hints"><Markdown source={guide.Hints} /></AccordionItem>{/if}
        {#if guide.Solution}<AccordionItem title="Solution"><Markdown source={guide.Solution} /></AccordionItem>{/if}
        {#if guide.References}<AccordionItem title="Documentation"><Markdown source={guide.References} /></AccordionItem>{/if}
      </Accordion>
    {/if}
  {/if}

  <div class="actions">
    {#if isBreakage}
      <Button disabled={!step.CanApply || busy} on:click={onapply}>Apply breakage</Button>
    {/if}
    <Button kind={isBreakage ? "tertiary" : "primary"} disabled={!step.CanVerify || busy} on:click={onverify}>Verify</Button>
    {#if applying}<InlineLoading description="Applying the breakage" />{/if}
  </div>

  <!-- Red means the check ran and the condition was not met. Yellow means it could not run at all (4.6). -->
  <div class="status" role="status" aria-live="polite">
    {#if last}
      {#if last.Status === "pass"}
        <InlineNotification lowContrast hideCloseButton kind="success" title="Passed" subtitle={last.Message} />
      {:else if last.Status === "fail"}
        <InlineNotification lowContrast hideCloseButton kind="error" title="Not as expected" subtitle={last.Message} />
      {:else}
        <InlineNotification lowContrast hideCloseButton kind="warning" title="It could not run" subtitle={last.Message} />
      {/if}
      {#if last.Evidence}
        <Accordion>
          <AccordionItem title="Output"><pre class="evidence">{last.Evidence}</pre></AccordionItem>
        </Accordion>
      {/if}
    {/if}
  </div>

  <div class="actions">
    <Button disabled={!step.CanContinue || busy} on:click={oncontinue}>Continue</Button>
  </div>

  <TextArea
    labelText="Notes"
    helperText="Kept with the step and shown in the report."
    rows={4}
    bind:value={notes}
    on:blur={() => notes !== step.Notes && onnotes(notes)}
  />

  <RadioButtonGroup legendText="Rating" selected={step.Rating ? String(step.Rating) : undefined} on:change={(e) => onrating(Number(e.detail))}>
    {#each [1, 2, 3, 4, 5] as n (n)}
      <RadioButton labelText={String(n)} value={String(n)} />
    {/each}
  </RadioButtonGroup>
  {#if step.Rating}
    <Button size="small" kind="ghost" on:click={() => onrating(0)}>Clear rating</Button>
  {/if}
</article>

<style>
  .actions {
    display: flex;
    gap: 1rem;
    align-items: center;
    margin: 1.5rem 0;
  }
  .status {
    margin-bottom: 1rem;
  }
  .evidence {
    white-space: pre-wrap;
    font-family: "IBM Plex Mono", monospace;
    font-size: 0.875rem;
  }
</style>
