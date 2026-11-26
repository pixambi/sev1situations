<script lang="ts">
  import { Button, InlineNotification, PasswordInput, Select, SelectItem, Tag, TextInput } from "carbon-components-svelte";
  import { Get, SaveField, SaveInstructor } from "../../bindings/github.com/pixambi/sev1situations/internal/gui/settingsservice.js";
  import type { SettingField, SettingsView } from "../../bindings/github.com/pixambi/sev1situations/internal/gui/models.js";
  import { maxNameLength } from "./startform.js";

  interface Props {
    /** Called after something was saved, so the rest of the page can pick it up: the theme, and which scenarios need a license. */
    onchanged: () => void;
  }
  let { onchanged }: Props = $props();

  const themeKey = "app.theme";

  let view = $state<SettingsView | null>(null);
  let loadError = $state("");
  let instructor = $state("");
  // What is typed into a field that is not saved yet, by key. A license is never filled in from what is saved.
  let drafts: Record<string, string> = $state({});
  let notice = $state<{ kind: "success" | "error"; text: string } | null>(null);
  let saving = $state("");

  async function load() {
    try {
      view = await Get();
      instructor = view.Instructor;
      loadError = "";
    } catch (err) {
      loadError = String(err);
    }
  }
  $effect(() => {
    void load();
  });

  /** Runs a save, and says how it went in words. */
  async function save(label: string, what: string, op: () => Promise<SettingsView>) {
    saving = label;
    notice = null;
    try {
      view = await op();
      notice = { kind: "success", text: `${what} saved.` };
      onchanged();
    } catch (err) {
      notice = { kind: "error", text: String(err) };
    } finally {
      saving = "";
    }
  }

  const saveInstructor = () => save("instructor", "The instructor's name", () => SaveInstructor(instructor));

  function saveField(f: SettingField) {
    const value = drafts[f.Key] ?? "";
    void save(f.Key, f.Label, async () => {
      const v = await SaveField(f.Key, value);
      drafts[f.Key] = ""; // what was typed is not kept once it is saved
      return v;
    });
  }
  const clearField = (f: SettingField) => save(f.Key, `${f.Label} cleared. It`, () => SaveField(f.Key, ""));

  const theme = $derived(view?.Fields?.find((f) => f.Key === themeKey));
  const others = $derived((view?.Fields ?? []).filter((f) => f.Key !== themeKey));
</script>

<section aria-labelledby="settings-heading">
  <h2 id="settings-heading">Settings</h2>

  {#if loadError}
    <InlineNotification lowContrast hideCloseButton kind="error" title="The settings could not be read" subtitle={loadError} />
  {:else if view === null}
    <p>Loading the settings.</p>
  {:else}
    <div role="status" aria-live="polite">
      {#if notice}
        <InlineNotification lowContrast kind={notice.kind} title={notice.kind === "success" ? "Saved" : "Not saved"} subtitle={notice.text} on:close={() => (notice = null)} />
      {/if}
    </div>

    <form class="group" onsubmit={(e) => (e.preventDefault(), void saveInstructor())}>
      <h3>Instructor</h3>
      <TextInput labelText="Instructor's name" helperText="Fills in the Start form for the next scenario." maxlength={maxNameLength} bind:value={instructor} />
      <Button type="submit" size="small" disabled={saving !== ""}>Save name</Button>
    </form>

    <div class="group">
      <h3>Appearance</h3>
      {#if theme}
        <Select
          labelText={theme.Label}
          helperText={theme.Help}
          selected={theme.Display}
          on:change={(e) => void save(theme.Key, theme.Label, () => SaveField(theme.Key, (e.target as HTMLSelectElement).value))}
        >
          <SelectItem value="" text="Follow the system" />
          <SelectItem value="light" text="Light" />
          <SelectItem value="dark" text="Dark" />
        </Select>
      {/if}
    </div>

    {#if others.length > 0}
      <div class="group">
        <h3>Licenses and other settings</h3>
        {#each others as f (f.Key)}
          <form class="field" onsubmit={(e) => (e.preventDefault(), saveField(f))}>
            {#if f.Secret}
              <PasswordInput
                labelText={f.Label}
                helperText={f.Help}
                placeholder={f.Set ? "Saved. Enter a new one to replace it." : "Paste it here"}
                autocomplete="off"
                bind:value={drafts[f.Key]}
              />
              <div class="state">
                <Tag type={f.Set ? "green" : "gray"}>{f.Set ? "Saved" : "Not set"}</Tag>
                <Button type="submit" size="small" disabled={saving !== "" || !(drafts[f.Key] ?? "").trim()}>Save</Button>
                {#if f.Set}
                  <Button type="button" size="small" kind="danger-ghost" disabled={saving !== ""} on:click={() => clearField(f)}>Remove</Button>
                {/if}
              </div>
            {:else}
              <TextInput labelText={f.Label} helperText={f.Help} placeholder={f.Display} bind:value={drafts[f.Key]} />
              <div class="state">
                <Button type="submit" size="small" disabled={saving !== ""}>Save</Button>
              </div>
            {/if}
          </form>
        {/each}
      </div>
    {/if}
  {/if}
</section>

<style>
  .group {
    max-width: 32rem;
    margin: 1.5rem 0 2rem;
  }
  .group h3 {
    margin-bottom: 0.75rem;
  }
  .field {
    margin-bottom: 1.5rem;
  }
  .state {
    display: flex;
    gap: 0.75rem;
    align-items: center;
    margin-top: 0.75rem;
  }
</style>
