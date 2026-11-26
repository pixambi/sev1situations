<script lang="ts">
  import "carbon-components-svelte/css/all.css";
  // Latin and Latin extended cover English and names with accents. Other scripts fall back to the system font.
  import "@fontsource/ibm-plex-sans/latin-400.css";
  import "@fontsource/ibm-plex-sans/latin-ext-400.css";
  import "@fontsource/ibm-plex-sans/latin-600.css";
  import "@fontsource/ibm-plex-sans/latin-ext-600.css";
  import "@fontsource/ibm-plex-mono/latin-400.css";
  import "@fontsource/ibm-plex-mono/latin-ext-400.css";
  import { Content, Header, InlineNotification, Theme } from "carbon-components-svelte";
  import { onMount } from "svelte";
  import { Events } from "@wailsio/runtime";
  import { Prereq } from "../bindings/github.com/pixambi/sev1situations/internal/gui/systemservice.js";
  import type { PrereqStatus } from "../bindings/github.com/pixambi/sev1situations/internal/gui/models.js";
  import SystemStatus from "./lib/SystemStatus.svelte";
  import ScenariosPage from "./lib/ScenariosPage.svelte";
  import { List } from "../bindings/github.com/pixambi/sev1situations/internal/gui/scenarioservice.js";
  import type { ScenarioList } from "../bindings/github.com/pixambi/sev1situations/internal/gui/models.js";
  import { followRun, run, runActive } from "./lib/run.svelte.js";

  // The theme follows the system setting. A user override arrives with the Settings page.
  const dark = window.matchMedia("(prefers-color-scheme: dark)");
  let theme: "white" | "g100" = $state(dark.matches ? "g100" : "white");

  // Whether Podman is ready. The first check connects to it and cleans up after an earlier launch.
  let prereq: PrereqStatus | null = $state(null);
  let checking = $state(false);

  // The catalogue loads without Podman, so it can be browsed while the machine is down.
  let catalogue: ScenarioList | null = $state(null);
  let catalogueError = $state("");

  async function loadCatalogue() {
    try {
      catalogue = await List();
      catalogueError = "";
    } catch (err) {
      catalogueError = String(err);
    }
  }

  async function check() {
    checking = true;
    try {
      prereq = await Prereq();
    } catch (err) {
      prereq = { Ready: false, Message: String(err), Notices: [] };
    } finally {
      checking = false;
    }
  }

  onMount(() => {
    const follow = (e: MediaQueryListEvent) => (theme = e.matches ? "g100" : "white");
    dark.addEventListener("change", follow);
    const stopPrereq = Events.On("system:prereq", (e) => (prereq = e.data as PrereqStatus));
    const stopRun = followRun();
    void check();
    void loadCatalogue();
    return () => {
      stopRun();
      dark.removeEventListener("change", follow);
      stopPrereq();
    };
  });
</script>

<Theme bind:theme />

<Header>
  <svelte:fragment slot="company">Sev1</svelte:fragment>
  <svelte:fragment slot="platform">Situations</svelte:fragment>
</Header>

<Content>
  <h1>Sev1 Situations</h1>
  <SystemStatus status={prereq} {checking} onretry={check} />
  {#if catalogueError}
    <InlineNotification lowContrast hideCloseButton kind="error" title="The scenarios could not be loaded" subtitle={catalogueError} />
  {:else if catalogue}
    <ScenariosPage
      {catalogue}
      runActive={runActive(run.snapshot)}
      podmanReady={prereq?.Ready ?? false}
      onstart={() => {}}
      onopenrun={() => {}}
      onopensettings={() => {}}
    />
  {/if}
</Content>

<style>
  h1 {
    margin-bottom: 1.5rem;
  }
</style>
