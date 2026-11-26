<script lang="ts">
  import "carbon-components-svelte/css/all.css";
  // Latin and Latin extended cover English and names with accents. Other scripts fall back to the system font.
  import "@fontsource/ibm-plex-sans/latin-400.css";
  import "@fontsource/ibm-plex-sans/latin-ext-400.css";
  import "@fontsource/ibm-plex-sans/latin-600.css";
  import "@fontsource/ibm-plex-sans/latin-ext-600.css";
  import "@fontsource/ibm-plex-mono/latin-400.css";
  import "@fontsource/ibm-plex-mono/latin-ext-400.css";
  import { Content, Header, Theme } from "carbon-components-svelte";
  import { onMount } from "svelte";

  // The theme follows the system setting. A user override arrives with the Settings page.
  const dark = window.matchMedia("(prefers-color-scheme: dark)");
  let theme: "white" | "g100" = $state(dark.matches ? "g100" : "white");

  onMount(() => {
    const follow = (e: MediaQueryListEvent) => (theme = e.matches ? "g100" : "white");
    dark.addEventListener("change", follow);
    return () => dark.removeEventListener("change", follow);
  });
</script>

<Theme bind:theme />

<Header>
  <svelte:fragment slot="company">Sev1</svelte:fragment>
  <svelte:fragment slot="platform">Situations</svelte:fragment>
</Header>

<Content>
  <h1>Sev1 Situations</h1>
  <p>No scenario is running.</p>
</Content>
