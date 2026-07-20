<script lang="ts">
  // Persistent banner when the installed pi is behind and pi-web could not
  // upgrade it (or it lacks --approve). Dismiss lasts for this browser
  // session only — version skew should stay annoying until it is gone.
  import { api, type PiStatus } from "../lib/api";
  import { toasts } from "../lib/toasts.svelte";

  const DISMISS_KEY = "pi-web:pi-banner-dismissed";

  let status = $state<PiStatus | null>(null);
  let dismissed = $state(sessionStorage.getItem(DISMISS_KEY) === "1");
  let busy = $state(false);

  async function load(): Promise<void> {
    try {
      status = await api.pi();
    } catch {
      /* no status, no banner */
    }
  }

  $effect(() => {
    load();
    const t = setInterval(load, 5 * 60 * 1000);
    return () => clearInterval(t);
  });

  const problem = $derived.by(() => {
    if (!status) return null;
    if (status.error) {
      return `pi ${status.current ?? "?"} is outdated — upgrade failed: ${status.error}`;
    }
    if (status.approveSupported === false) {
      return `pi ${status.current ?? "?"} predates --approve; sessions run without project trust`;
    }
    return null;
  });

  function dismiss(): void {
    dismissed = true;
    try {
      sessionStorage.setItem(DISMISS_KEY, "1");
    } catch {
      /* best effort */
    }
  }

  async function upgrade(): Promise<void> {
    busy = true;
    try {
      status = await api.updatePi();
      if (!status.error) toasts.show("pi upgraded.");
    } catch (err) {
      toasts.error(err instanceof Error ? err.message : String(err));
    } finally {
      busy = false;
    }
  }
</script>

{#if problem && !dismissed}
  <div class="banner" role="status">
    <span class="msg">{problem}</span>
    <button type="button" class="act" disabled={busy} onclick={upgrade}>
      {busy ? "upgrading…" : "retry upgrade"}
    </button>
    <button type="button" class="x" aria-label="Dismiss for this browser session" onclick={dismiss}>
      ✕
    </button>
  </div>
{/if}

<style>
  .banner {
    display: flex;
    align-items: center;
    gap: 0.9rem;
    padding: 0.4rem 1rem;
    font-size: var(--text-sm);
    background: var(--warn-soft);
    border-bottom: 1px solid var(--warn);
    color: var(--ink);
  }
  .msg {
    flex: 1;
    min-width: 0;
  }
  .act {
    flex: none;
    padding: 0.15rem 0.6rem;
    font-size: var(--text-xs);
    border: 1px solid var(--warn);
    border-radius: var(--r-sm);
    color: var(--warn);
  }
  .act:hover:not(:disabled) {
    background: var(--warn);
    color: var(--surface);
  }
  .x {
    flex: none;
    color: var(--ink-faint);
  }
  .x:hover {
    color: var(--ink);
  }
</style>
