<script lang="ts">
  import type { Snippet } from "svelte";

  // Modal scaffold shared by every overlay: scrim, titled panel, Esc/scrim
  // close, initial focus into the panel.
  let {
    title,
    onClose,
    wide = false,
    children,
  }: {
    title: string;
    onClose: () => void;
    wide?: boolean;
    children: Snippet;
  } = $props();

  let panel = $state<HTMLElement | null>(null);

  $effect(() => {
    panel?.focus();
  });

  function onKeydown(e: KeyboardEvent): void {
    if (e.key === "Escape") {
      e.stopPropagation();
      onClose();
    }
  }
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<div class="scrim" onclick={(e) => e.target === e.currentTarget && onClose()} onkeydown={onKeydown} role="presentation">
  <div class="panel" class:wide role="dialog" aria-modal="true" aria-label={title} tabindex="-1" bind:this={panel}>
    <header>
      <span class="label">{title}</span>
      <button type="button" class="close" aria-label="Close" onclick={onClose}>✕</button>
    </header>
    <div class="body">
      {@render children()}
    </div>
  </div>
</div>

<style>
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 60;
    display: flex;
    align-items: flex-start;
    justify-content: center;
    padding: 8vh 1rem 1rem;
    background: var(--scrim);
  }
  .panel {
    width: min(30rem, 100%);
    max-height: 84vh;
    display: flex;
    flex-direction: column;
    background: var(--surface);
    border: 1px solid var(--border-strong);
    border-radius: var(--r-lg);
    box-shadow: var(--shadow);
    outline: none;
  }
  .panel.wide {
    width: min(52rem, 100%);
  }
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.7rem 1rem;
    border-bottom: 1px solid var(--border);
  }
  .close {
    color: var(--ink-faint);
    font-size: var(--text-sm);
    padding: 0.15rem 0.35rem;
    border-radius: var(--r-sm);
  }
  .close:hover {
    color: var(--ink);
    background: var(--surface-2);
  }
  .body {
    overflow-y: auto;
    padding: 1rem;
  }
</style>
