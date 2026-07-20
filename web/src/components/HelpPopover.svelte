<script lang="ts">
  // The keyboard map, straight from lib/shortcuts so it cannot drift.
  import Overlay from "./Overlay.svelte";
  import { SHORTCUTS } from "../lib/shortcuts";

  let { onClose }: { onClose: () => void } = $props();
</script>

<Overlay title="keyboard" {onClose}>
  <dl>
    {#each SHORTCUTS as s (s.keys)}
      <div class="row">
        <dt>{s.keys}</dt>
        <dd>{s.does}</dd>
      </div>
    {/each}
  </dl>
</Overlay>

<style>
  dl {
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: 0.45rem;
  }
  .row {
    display: flex;
    align-items: baseline;
    gap: 1rem;
  }
  dt {
    flex: none;
    width: 9rem;
    font-size: var(--text-sm);
    font-weight: 600;
    color: var(--live-ink);
  }
  dd {
    margin: 0;
    font-size: var(--text-sm);
    color: var(--ink-muted);
  }
</style>
