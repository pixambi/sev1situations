<script lang="ts">
  // A stand-in for TerminalPane in tests of the tab that holds it.
  let { id, visible }: { id: string; node: string; visible: boolean; onclose: () => void } = $props();
</script>

<div data-testid="pane" data-id={id} data-visible={visible}></div>
