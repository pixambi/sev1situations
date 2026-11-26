<script lang="ts">
  import RunPage from "./RunPage.svelte";
  import { run } from "./run.svelte.js";
</script>

{#if run.snapshot}<RunPage snapshot={run.snapshot} />{/if}
