<script lang="ts">
  import { Modal } from "carbon-components-svelte";
  import type { Snippet } from "svelte";

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
