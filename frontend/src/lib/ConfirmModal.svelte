<script lang="ts">
  import { Modal } from "carbon-components-svelte";
  import { onMount, type Snippet } from "svelte";

  interface Props {
    open: boolean;
    heading: string;
    confirmLabel: string;
    /** True while the confirmed action runs, which keeps the modal open and the buttons off. */
    busy?: boolean;
    /** Disables the confirm button, for a choice that has not been made yet. */
    confirmDisabled?: boolean;
    onconfirm: () => void;
    oncancel: () => void;
    children: Snippet;
  }
  let { open, heading, confirmLabel, busy = false, confirmDisabled = false, onconfirm, oncancel, children }: Props = $props();

  // When the dialog goes away, focus goes back to what opened it, so a keyboard user carries on from where they were.
  // The page only has the dialog in it while it is open, so this runs when it is created and when it is removed.
  // svelte-ignore state_referenced_locally
  const opener = document.activeElement as HTMLElement | null;
  onMount(() => () => {
    setTimeout(() => {
      if (opener?.isConnected) opener.focus();
    }, 0);
  });
</script>

<Modal
  {open}
  danger
  size="sm"
  modalHeading={heading}
  primaryButtonText={busy ? "Working" : confirmLabel}
  primaryButtonDisabled={busy || confirmDisabled}
  secondaryButtonText="Cancel"
  preventCloseOnClickOutside={busy}
  on:click:button--primary={onconfirm}
  on:click:button--secondary={oncancel}
  on:close={() => !busy && oncancel()}
>
  {@render children()}
</Modal>
