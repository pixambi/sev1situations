<script lang="ts">
  import { Button, InlineNotification } from "carbon-components-svelte";
  import { FitAddon, Ghostty, Terminal } from "ghostty-web";
  import wasmUrl from "ghostty-web/ghostty-vt.wasm?url";
  import { Events } from "@wailsio/runtime";
  import { onMount } from "svelte";
  import { Close, Input, Open, Resize } from "../../bindings/github.com/pixambi/sev1situations/internal/gui/terminalservice.js";
  import { decodeBase64, GuardedTerminal, InputQueue, type TermCore } from "./terminalglue.js";

  interface Props {
    /** The terminal's ID, chosen by the page, which Go and the events use. */
    id: string;
    node: string;
    /** True while the pane is on screen. A hidden pane has no size, so it fits again when shown. */
    visible: boolean;
    /** Called when the instructor closes a finished session. */
    onclose: () => void;
  }
  let { id, node, visible, onclose }: Props = $props();

  let host: HTMLElement | undefined = $state();
  let leaveButton: HTMLElement | undefined = $state();
  let guarded: GuardedTerminal | undefined;
  let exited = $state<string | null>(null);
  let problem = $state("");
  let cleared = $state(false);

  // The shell is told the size after the page settles on it, not at every pixel of a drag.
  let resizeTimer: ReturnType<typeof setTimeout> | undefined;
  function reportSize(cols: number, rows: number) {
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(() => void Resize(id, cols, rows).catch(() => {}), 50);
  }

  /** Builds a ghostty-web terminal in the host. The library's default loader would need a data: URL, which
   *  the page's policy forbids, so its WebAssembly is loaded from the application's own assets (R-TERM-6). */
  async function buildCore(): Promise<TermCore> {
    await document.fonts.load('14px "IBM Plex Mono"');
    const ghostty = await Ghostty.load(wasmUrl);
    const term = new Terminal({
      cols: 80,
      rows: 24,
      ghostty,
      fontSize: 14,
      fontFamily: '"IBM Plex Mono", monospace',
      cursorBlink: true,
      scrollback: 5000,
      theme: { background: "#161616", foreground: "#f4f4f4" },
    });
    // A terminal passes Tab to the shell, so a keyboard user would be stuck in it. Shift+Escape is the way out
    // (WCAG 2.1.2): it moves focus to the button below, from where Tab carries on through the page.
    term.attachCustomKeyEventHandler((e: KeyboardEvent) => {
      if (e.key === "Escape" && e.shiftKey) {
        e.preventDefault();
        leaveButton?.focus();
        return true;
      }
      return false;
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    host!.replaceChildren();
    term.open(host!);
    fit.observeResize();
    return {
      get cols() {
        return term.cols;
      },
      get rows() {
        return term.rows;
      },
      write: (d) => term.write(d),
      fit: () => fit.fit(),
      focus: () => term.focus(),
      dispose: () => {
        fit.dispose();
        term.dispose();
      },
      onData: (cb) => void term.onData(cb),
      onResize: (cb) => void term.onResize((e) => cb(e.cols, e.rows)),
    };
  }

  onMount(() => {
    // Listen before opening, so no output is missed.
    const offOut = Events.On(`terminal:out:${id}`, (e) => guarded?.write(decodeBase64(e.data as string)));
    const offExit = Events.On(`terminal:exit:${id}`, (e) => (exited = (e.data as { Reason?: string })?.Reason ?? ""));

    // What is typed goes out in order, one call at a time.
    const typed = new InputQueue(
      (d) => Input(id, d),
      (err) => (problem = String(err)),
    );
    const g = new GuardedTerminal(buildCore, {
      ondata: (d) => typed.push(d),
      onresize: reportSize,
      onrebuild: () => (cleared = true),
    });
    guarded = g;
    void (async () => {
      try {
        await g.init();
        g.fit();
        const size = g.size();
        await Open(id, node, size.cols, size.rows);
        g.focus();
      } catch (err) {
        problem = String(err);
      }
    })();

    return () => {
      clearTimeout(resizeTimer);
      offOut();
      offExit();
      g.dispose();
      guarded = undefined;
      void Close(id);
    };
  });

  // A pane that was hidden has no size, so it fits when it is shown again.
  $effect(() => {
    if (visible) {
      guarded?.fit();
      guarded?.focus();
    }
  });
</script>

<div class="pane">
  {#if problem}
    <InlineNotification lowContrast kind="error" title="The terminal has a problem" subtitle={problem} on:close={() => (problem = "")} />
  {/if}
  {#if cleared}
    <InlineNotification
      lowContrast
      kind="info"
      title="The screen was cleared"
      subtitle="The terminal had to be rebuilt after unusual output, such as a binary file. The shell in the container is not affected."
      on:close={() => (cleared = false)}
    />
  {/if}
  {#if exited !== null}
    <InlineNotification lowContrast hideCloseButton kind="info" title="The session has ended" subtitle={exited || `The shell on ${node} exited, or the node stopped.`}>
      <svelte:fragment slot="actions">
        <Button size="small" kind="ghost" on:click={onclose}>Close this terminal</Button>
      </svelte:fragment>
    </InlineNotification>
  {/if}
  <!-- ghostty-web gives its container its own role and label, so the node is named on a wrapper. -->
  <div role="group" aria-label="Terminal on {node}">
    <div class="host" bind:this={host}></div>
    <div class="leave">
      <button type="button" bind:this={leaveButton} class="bx--btn bx--btn--ghost bx--btn--sm" onclick={() => host?.focus()}>Back into the terminal</button>
      <span>Press Shift+Escape in the terminal to leave it with the keyboard. This button goes back in.</span>
    </div>
  </div>
</div>

<style>
  .leave {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    margin-top: 0.25rem;
    font-size: 0.875rem;
    color: var(--cds-text-secondary, #525252);
  }
  .host {
    height: 28rem;
    background: #161616;
    padding: 0.25rem;
  }
</style>
