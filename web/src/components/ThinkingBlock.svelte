<script lang="ts">
  // A reasoning block: quiet, collapsible, open by default while it streams
  // so the operator can watch the agent think.
  import Prose from "./Prose.svelte";

  let { thinking, streaming }: { thinking: string; streaming: boolean } = $props();

  // open by default only when the block mounts mid-stream; after that the
  // operator owns the toggle
  // svelte-ignore state_referenced_locally
  let open = $state(streaming);
</script>

<div class="thinking" class:open>
  <button type="button" class="head" aria-expanded={open} onclick={() => (open = !open)}>
    <span class="marker" class:live={streaming}></span>
    <span class="label think-label">thinking{streaming ? "…" : ""}</span>
    <span class="chev">{open ? "▾" : "▸"}</span>
  </button>
  {#if open}
    <div class="body">
      <Prose content={thinking} muted />
    </div>
  {/if}
</div>

<style>
  .thinking {
    margin: 0.6rem 0;
  }
  .head {
    display: inline-flex;
    align-items: center;
    gap: 0.5em;
  }
  .marker {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--think);
    opacity: 0.5;
  }
  .marker.live {
    animation: breathe 1.6s ease-in-out infinite;
    opacity: 1;
  }
  .think-label {
    color: var(--think);
  }
  .chev {
    font-size: var(--text-xs);
    color: var(--ink-faint);
  }
  .body {
    margin-top: 0.35rem;
    padding-left: 0.85rem;
    border-left: 2px solid var(--think-soft);
    font-style: italic;
  }
</style>
