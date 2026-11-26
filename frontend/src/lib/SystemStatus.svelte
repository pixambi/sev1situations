<script lang="ts">
  import { InlineNotification, NotificationActionButton, InlineLoading } from "carbon-components-svelte";
  import type { PrereqStatus } from "../../bindings/github.com/pixambi/sev1situations/internal/gui/models.js";

  interface Props {
    /** The last answer, or null before the first one. */
    status: PrereqStatus | null;
    /** True while a check is under way. */
    checking: boolean;
    onretry: () => void;
  }
  let { status, checking, onretry }: Props = $props();
</script>

{#if status === null || (checking && !status.Ready)}
  <InlineLoading description="Checking that Podman is ready" />
{:else if !status.Ready}
  <InlineNotification lowContrast hideCloseButton kind="error" title="Podman is not ready" subtitle={status.Message}>
    <svelte:fragment slot="actions">
      <NotificationActionButton on:click={onretry}>Retry</NotificationActionButton>
    </svelte:fragment>
  </InlineNotification>
{:else}
  {#each status.Notices ?? [] as notice (notice)}
    <InlineNotification lowContrast kind="info" title="Cleaned up" subtitle={notice} />
  {/each}
{/if}
