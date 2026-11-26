<script lang="ts">
  import "carbon-components-svelte/css/all.css";
  // Latin and Latin extended cover English and names with accents. Other scripts fall back to the system font.
  import "@fontsource/ibm-plex-sans/latin-400.css";
  import "@fontsource/ibm-plex-sans/latin-ext-400.css";
  import "@fontsource/ibm-plex-sans/latin-600.css";
  import "@fontsource/ibm-plex-sans/latin-ext-600.css";
  import "@fontsource/ibm-plex-mono/latin-400.css";
  import "@fontsource/ibm-plex-mono/latin-ext-400.css";
  import { Button, Content, ContentSwitcher, Header, HeaderGlobalAction, HeaderUtilities, InlineNotification, Switch, Theme } from "carbon-components-svelte";
  import { onMount } from "svelte";
  import { Events } from "@wailsio/runtime";
  import { Prereq } from "../bindings/github.com/pixambi/sev1situations/internal/gui/systemservice.js";
  import type { PrereqStatus } from "../bindings/github.com/pixambi/sev1situations/internal/gui/models.js";
  import SystemStatus from "./lib/SystemStatus.svelte";
  import ScenariosPage from "./lib/ScenariosPage.svelte";
  import StartForm from "./lib/StartForm.svelte";
  import RunPage from "./lib/RunPage.svelte";
  import ReportsPage from "./lib/ReportsPage.svelte";
  import ReportViewer from "./lib/ReportViewer.svelte";
  import SettingsPage from "./lib/SettingsPage.svelte";
  import SettingsIcon from "carbon-icons-svelte/lib/Settings.svelte";
  import { Get as GetSettings } from "../bindings/github.com/pixambi/sev1situations/internal/gui/settingsservice.js";
  import { List } from "../bindings/github.com/pixambi/sev1situations/internal/gui/scenarioservice.js";
  import type { ScenarioList } from "../bindings/github.com/pixambi/sev1situations/internal/gui/models.js";
  import { followRun, run, runActive } from "./lib/run.svelte.js";

  // The theme follows the system unless Settings says light or dark.
  const dark = window.matchMedia("(prefers-color-scheme: dark)");
  let systemTheme: "white" | "g100" = $state(dark.matches ? "g100" : "white");
  let override = $state("");
  const theme = $derived<"white" | "g100">(override === "light" ? "white" : override === "dark" ? "g100" : systemTheme);

  async function loadSettings() {
    try {
      const v = await GetSettings();
      override = v.Fields?.find((f) => f.Key === "app.theme")?.Display ?? "";
    } catch {
      override = ""; // without the settings, the system's theme is used
    }
  }

  // Whether Podman is ready. The first check connects to it and cleans up after an earlier launch.
  let prereq: PrereqStatus | null = $state(null);
  let checking = $state(false);

  // The page shown while no run exists is the catalogue or the Start form. A run takes over, and the
  // instructor can still look at the catalogue to see why starting another is blocked.
  let page = $state<"scenarios" | "start" | "run" | "settings" | "reports" | "report">("scenarios");
  let openReport = $state("");
  let chosen = $state("");
  const active = $derived(runActive(run.snapshot));
  $effect(() => {
    // A run beginning shows the run, and a run ending returns to the catalogue.
    page = active ? "run" : "scenarios";
  });

  // The switcher shows the catalogue, the reports and, while one exists, the run.
  type PageName = "scenarios" | "reports" | "run";
  const pages = $derived<PageName[]>(active ? ["scenarios", "reports", "run"] : ["scenarios", "reports"]);
  const labels = { scenarios: "Scenarios", reports: "Reports", run: "Current run" } as const;
  const shownPage = $derived(Math.max(0, pages.indexOf(page === "report" ? "reports" : page === "start" ? "scenarios" : (page as PageName))));
  function switchPage(index: number) {
    page = pages[index] ?? "scenarios";
  }

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
    const follow = (e: MediaQueryListEvent) => (systemTheme = e.matches ? "g100" : "white");
    dark.addEventListener("change", follow);
    const stopPrereq = Events.On("system:prereq", (e) => (prereq = e.data as PrereqStatus));
    // The application menu's Settings item (Cmd+,) is chosen in Go, and tells the page.
    const stopMenu = Events.On("menu:settings", () => (page = "settings"));
    const stopRun = followRun();
    void check();
    void loadCatalogue();
    void loadSettings();
    return () => {
      stopRun();
      dark.removeEventListener("change", follow);
      stopPrereq();
      stopMenu();
    };
  });
</script>

<Theme {theme} />

<Header>
  <svelte:fragment slot="company">Sev1</svelte:fragment>
  <svelte:fragment slot="platform">Situations</svelte:fragment>
  <HeaderUtilities>
    <HeaderGlobalAction aria-label="Settings" icon={SettingsIcon} isActive={page === "settings"} on:click={() => (page = "settings")} />
  </HeaderUtilities>
</Header>

<Content>
  <h1>Sev1 Situations</h1>
  {#if page !== "settings"}
    <nav aria-label="Pages" class="pages">
      {#key pages.length}
        <ContentSwitcher selectedIndex={shownPage} on:change={(e) => switchPage(e.detail)}>
          {#each pages as p (p)}<Switch text={labels[p]} />{/each}
        </ContentSwitcher>
      {/key}
    </nav>
  {/if}
  <SystemStatus status={prereq} {checking} onretry={check} />
  {#if page === "settings"}
    <Button kind="ghost" size="small" on:click={() => (page = active ? "run" : "scenarios")}>Back</Button>
    <SettingsPage
      onchanged={() => {
        void loadSettings();
        void loadCatalogue(); // a saved license changes which scenarios can be started
      }}
    />
  {:else if page === "reports"}
    <ReportsPage
      onopen={(id) => {
        openReport = id;
        page = "report";
      }}
    />
  {:else if page === "report"}
    <ReportViewer id={openReport} onback={() => (page = "reports")} />
  {:else if catalogueError}
    <InlineNotification lowContrast hideCloseButton kind="error" title="The scenarios could not be loaded" subtitle={catalogueError} />
  {:else if catalogue}
    {#if page === "run" && run.snapshot}
      <RunPage snapshot={run.snapshot} />
    {:else if page === "start" && !active}
      <StartForm
        scenarioId={chosen}
        onstarted={() => (page = "run")}
        oncancel={() => (page = "scenarios")}
        onopensettings={() => (page = "settings")}
      />
    {:else}
      <ScenariosPage
        {catalogue}
        runActive={active}
        podmanReady={prereq?.Ready ?? false}
        onstart={(id) => {
          chosen = id;
          page = "start";
        }}
        onopenrun={() => (page = "run")}
        onopensettings={() => (page = "settings")}
      />
    {/if}
  {/if}
</Content>

<style>
  h1 {
    margin-bottom: 1.5rem;
  }
  .pages {
    max-width: 24rem;
    margin-bottom: 1.5rem;
  }
</style>
