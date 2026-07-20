<script lang="ts">
  // Toast stack: extension notify + app errors. Top-right, quiet, dismissable.
  import { toasts } from "../lib/toasts.svelte";
</script>

<div class="stack" aria-live="polite">
  {#each toasts.items as t (t.id)}
    <button type="button" class="toast {t.level}" onclick={() => toasts.dismiss(t.id)}>
      <span class="dot" aria-hidden="true"></span>
      <span class="msg">{t.message}</span>
    </button>
  {/each}
</div>

<style>
  .stack {
    position: fixed;
    top: 0.8rem;
    right: 0.8rem;
    z-index: 80;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    max-width: min(24rem, calc(100vw - 1.6rem));
  }
  .toast {
    display: flex;
    align-items: baseline;
    gap: 0.6em;
    padding: 0.55rem 0.8rem;
    font-size: var(--text-sm);
    text-align: left;
    background: var(--surface);
    border: 1px solid var(--border-strong);
    border-radius: var(--r-md);
    box-shadow: var(--shadow);
  }
  .dot {
    flex: none;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--live);
  }
  .toast.warning .dot {
    background: var(--warn);
  }
  .toast.error .dot {
    background: var(--err);
  }
  .toast.error {
    border-color: var(--err);
  }
  .msg {
    word-break: break-word;
  }
</style>
