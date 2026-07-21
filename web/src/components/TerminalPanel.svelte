<script lang="ts">
  // Bottom dock of private terminals: tabs, new/kill, drag-resize. Shells are
  // detached server-side (dtach), so closing this panel or the browser leaves
  // them running; reopening rediscovers them via GET /api/terminals. Nothing
  // shown here ever reaches the agent's session context.
  import TerminalView from "./TerminalView.svelte";
  import { api } from "../lib/api";
  import { session } from "../lib/session.svelte";
  import { toasts } from "../lib/toasts.svelte";

  let { onClose }: { onClose: () => void } = $props();

  interface Tab {
    id: string;
    cwd: string;
    exited: number | null;
  }

  let tabs = $state<Tab[]>([]);
  let activeId = $state<string | null>(null);
  let height = $state(260);
  let loaded = $state(false);

  $effect(() => {
    api
      .terminals()
      .then((r) => {
        tabs = (r.terminals ?? []).map((t) => ({ id: t.id, cwd: t.cwd, exited: null }));
        loaded = true;
        if (tabs.length === 0) {
          void newTerminal();
        } else {
          activeId = tabs[tabs.length - 1].id;
        }
      })
      .catch((err) => {
        loaded = true;
        toasts.error(err instanceof Error ? err.message : String(err));
      });
  });

  async function newTerminal(): Promise<void> {
    try {
      const created = await api.createTerminal(session.cwd);
      tabs = [...tabs, { id: created.id, cwd: created.cwd ?? "", exited: null }];
      activeId = created.id;
    } catch (err) {
      toasts.error(err instanceof Error ? err.message : String(err));
    }
  }

  function markExited(id: string, code: number): void {
    tabs = tabs.map((t) => (t.id === id ? { ...t, exited: code } : t));
  }

  function closeTab(id: string): void {
    // A live shell gets SIGTERMed; an exited one is already forgotten
    // server-side, where the kill is a harmless no-op.
    api.killTerminal(id).catch(() => {});
    tabs = tabs.filter((t) => t.id !== id);
    if (activeId === id) activeId = tabs.length > 0 ? tabs[tabs.length - 1].id : null;
    if (tabs.length === 0) onClose();
  }

  function shortCwd(cwd: string): string {
    return cwd.split("/").pop() || cwd;
  }

  // drag-resize: pointer up = taller panel
  function startDrag(e: PointerEvent): void {
    e.preventDefault();
    const startY = e.clientY;
    const startH = height;
    const move = (ev: PointerEvent) => {
      height = Math.min(Math.max(startH + (startY - ev.clientY), 80), Math.round(window.innerHeight * 0.6));
    };
    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  }
</script>

<section class="panel" style:height="{height}px" aria-label="Private terminals">
  <div class="grip" role="separator" aria-orientation="horizontal" onpointerdown={startDrag}></div>
  <div class="bar">
    <span class="label" title="Private terminals — the agent cannot see these">terminal</span>
    <div class="tabs" role="tablist">
      {#each tabs as tab (tab.id)}
        <span class="tab" class:active={tab.id === activeId}>
          <button
            type="button"
            role="tab"
            aria-selected={tab.id === activeId}
            class="pick"
            onclick={() => (activeId = tab.id)}
          >
            <span class="dot" class:dead={tab.exited !== null}>{tab.exited === null ? "●" : "✗"}</span>
            {shortCwd(tab.cwd)}
          </button>
          <button type="button" class="x" aria-label="Close terminal" onclick={() => closeTab(tab.id)}>✕</button>
        </span>
      {/each}
      <button type="button" class="new" title="New terminal" onclick={newTerminal}>+</button>
    </div>
    <button type="button" class="x hide" aria-label="Hide terminal panel" onclick={onClose}>—</button>
  </div>
  <div class="body">
    {#if !loaded}
      <p class="note">loading…</p>
    {:else}
      {#each tabs as tab (tab.id)}
        <TerminalView id={tab.id} active={tab.id === activeId} onExit={(code) => markExited(tab.id, code)} />
      {/each}
    {/if}
  </div>
</section>

<style>
  .panel {
    position: relative;
    flex: none;
    display: flex;
    flex-direction: column;
    border-top: 1px solid var(--border);
    background: var(--surface);
    min-height: 80px;
  }
  .grip {
    position: absolute;
    top: -3px;
    left: 0;
    right: 0;
    height: 6px;
    cursor: row-resize;
    z-index: 1;
  }
  .bar {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    padding: 0.3rem 0.7rem;
    border-bottom: 1px solid var(--border);
  }
  .label {
    font-size: var(--text-xs);
    letter-spacing: var(--track);
    text-transform: uppercase;
    color: var(--ink-faint);
    flex: none;
  }
  .tabs {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    min-width: 0;
    overflow-x: auto;
  }
  .tab {
    display: inline-flex;
    align-items: center;
    border: 1px solid transparent;
    border-radius: var(--r-sm);
  }
  .tab.active {
    background: var(--surface-2);
    border-color: var(--border);
  }
  .pick {
    display: inline-flex;
    align-items: center;
    gap: 0.35em;
    font-family: var(--font-mono);
    font-size: var(--text-sm);
    color: var(--ink-muted);
    padding: 0.15rem 0.2rem 0.15rem 0.45rem;
    white-space: nowrap;
  }
  .tab.active .pick {
    color: var(--ink);
  }
  .dot {
    color: var(--live);
    font-size: 0.6em;
  }
  .dot.dead {
    color: var(--err);
    font-size: 0.9em;
  }
  .x {
    color: var(--ink-faint);
    font-size: var(--text-xs);
    padding: 0.15rem 0.35rem;
    border-radius: var(--r-sm);
  }
  .x:hover {
    color: var(--ink);
    background: var(--surface-3);
  }
  .hide {
    margin-left: auto;
    flex: none;
  }
  .new {
    flex: none;
    color: var(--ink-muted);
    font-size: var(--text-md);
    padding: 0 0.4rem;
    border-radius: var(--r-sm);
  }
  .new:hover {
    color: var(--ink);
    background: var(--surface-2);
  }
  .body {
    flex: 1;
    min-height: 0;
  }
  .note {
    margin: 0.6rem 0.8rem;
    font-size: var(--text-sm);
    color: var(--ink-muted);
  }
</style>
