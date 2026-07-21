<script lang="ts">
  // Telemetry strip rendered inside the composer's footer: every agent state
  // is visible here — run state, queue contents, retry countdown, compaction,
  // tokens, context, cost — plus the session controls popover. No silent
  // stalls. The composer owns the surrounding box; this fills its foot row.
  import Dropdown from "./Dropdown.svelte";
  import Toggle from "./Toggle.svelte";
  import { stripAnsi } from "../lib/ansi";
  import { api } from "../lib/api";
  import { session } from "../lib/session.svelte";
  import { toasts } from "../lib/toasts.svelte";

  const view = $derived(session.view);

  let controlsOpen = $state(false);
  let controlsEl = $state<HTMLElement | null>(null);

  // retry countdown, ticking against the event's arrival time
  let now = $state(Date.now());
  $effect(() => {
    if (!view.retry) return;
    const t = setInterval(() => (now = Date.now()), 250);
    return () => clearInterval(t);
  });
  const retryRemaining = $derived(
    view.retry ? Math.max(0, view.retry.delayMs - (now - view.retry.receivedAt)) : 0,
  );

  function fmtTokens(n?: number): string {
    if (n === undefined || n === null) return "–";
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}m`;
    if (n >= 1000) return `${(n / 1000).toFixed(n >= 10000 ? 0 : 1)}k`;
    return String(n);
  }

  function fmtCost(c?: number | null): string {
    if (c === undefined || c === null) return "–";
    return `$${c.toFixed(c >= 10 ? 1 : 2)}`;
  }

  const ctxPercent = $derived(view.stats?.contextUsage?.percent ?? null);
  // Cold sessions carry the context token count but no window size, so the
  // percent is unknown; fall back to the absolute token figure.
  const ctxTokens = $derived(view.stats?.contextUsage?.tokens ?? null);
  const ctxLabel = $derived(
    ctxPercent !== null
      ? `${Math.round(ctxPercent)}%`
      : ctxTokens !== null
        ? fmtTokens(ctxTokens)
        : "–",
  );

  const queued = $derived(view.queue.steering.length + view.queue.followUp.length);

  async function run(action: () => Promise<unknown>): Promise<void> {
    try {
      await action();
    } catch (err) {
      toasts.error(err instanceof Error ? err.message : String(err));
    }
  }

  const modeItems = [
    { value: "one-at-a-time", label: "one at a time" },
    { value: "all", label: "all at once" },
  ];

  function onFocusOut(e: FocusEvent): void {
    if (!controlsEl?.contains(e.relatedTarget as Node)) controlsOpen = false;
  }
</script>

<div class="bar">
  <div class="left">
    <span class="state">
      {#if view.compacting}
        <span class="pulse warn-pulse"></span> <span class="label warn-text">compacting</span>
      {:else if view.retry}
        <span class="pulse warn-pulse"></span>
        <span class="label warn-text">
          retry {view.retry.attempt}/{view.retry.maxAttempts} in {(retryRemaining / 1000).toFixed(0)}s
        </span>
        <button
          type="button"
          class="mini warn-text"
          title={view.retry.errorMessage}
          onclick={() => run(() => api.abortRetry(session.id))}
        >
          abort retry
        </button>
      {:else if view.streaming}
        <span class="pulse"></span> <span class="label live-text">running</span>
      {:else}
        <span class="idle-dot"></span> <span class="label">idle</span>
      {/if}
    </span>

    {#if queued > 0}
      <span class="queue" title="Queued messages">
        {#each view.queue.steering as q, i (i)}
          <span class="chip steer" title={q}>steer: {q}</span>
        {/each}
        {#each view.queue.followUp as q, i (i)}
          <span class="chip follow" title={q}>then: {q}</span>
        {/each}
      </span>
    {/if}

    {#each Object.entries(view.statusLines) as [key, text] (key)}
      <span class="ext-status" title="extension status: {key}">{stripAnsi(text)}</span>
    {/each}
  </div>

  <div class="right">
    <span class="tele tokens" title="Input / output tokens">
      ▲{fmtTokens(view.stats?.tokens?.input)} ▼{fmtTokens(view.stats?.tokens?.output)}
    </span>
    <span
      class="tele ctx"
      class:hot={ctxPercent !== null && ctxPercent >= 80}
      title="Context window used"
    >
      ctx {ctxLabel}
    </span>
    <span class="tele cost" title="Session cost">{fmtCost(view.stats?.cost)}</span>

    {#if session.id}
      <div class="controls" bind:this={controlsEl} onfocusout={onFocusOut}>
        <button
          type="button"
          class="mini"
          aria-expanded={controlsOpen}
          aria-haspopup="true"
          onclick={() => (controlsOpen = !controlsOpen)}
        >
          controls ▾
        </button>
        {#if controlsOpen}
          <div class="panel">
            <div class="ctl">
              <span class="label">steering</span>
              <Dropdown
                label="Steering message delivery"
                items={modeItems}
                value={view.steeringMode}
                buttonText={view.steeringMode === "all" ? "all at once" : "one at a time"}
                align="right"
                up
                onSelect={(mode) =>
                  run(async () => {
                    await api.setSteering(session.id, mode);
                    view.steeringMode = mode;
                  })}
              />
            </div>
            <div class="ctl">
              <span class="label">follow-up</span>
              <Dropdown
                label="Follow-up message delivery"
                items={modeItems}
                value={view.followUpMode}
                buttonText={view.followUpMode === "all" ? "all at once" : "one at a time"}
                align="right"
                up
                onSelect={(mode) =>
                  run(async () => {
                    await api.setFollowUp(session.id, mode);
                    view.followUpMode = mode;
                  })}
              />
            </div>
            <div class="ctl">
              <Toggle
                label="auto-compaction"
                checked={view.autoCompaction}
                onChange={(on) =>
                  run(async () => {
                    await api.setAutoCompaction(session.id, on);
                    view.autoCompaction = on;
                  })}
              />
            </div>
            <div class="ctl">
              <button
                type="button"
                class="mini compact-now"
                disabled={view.compacting}
                onclick={() => {
                  controlsOpen = false;
                  run(() => api.compact(session.id));
                }}
              >
                compact context now
              </button>
            </div>
          </div>
        {/if}
      </div>
    {/if}
  </div>
</div>

<style>
  .bar {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    min-width: 0;
    font-size: var(--text-xs);
    flex-wrap: wrap;
  }
  .left,
  .right {
    display: flex;
    align-items: center;
    gap: 0.9em;
    min-width: 0;
    flex-wrap: wrap;
  }
  .state {
    display: inline-flex;
    align-items: center;
    gap: 0.5em;
  }
  .idle-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--border-strong);
  }
  .live-text {
    color: var(--live-ink);
  }
  .warn-text {
    color: var(--warn);
  }
  .warn-pulse {
    background: var(--warn);
  }
  .queue {
    display: inline-flex;
    gap: 0.4em;
    min-width: 0;
  }
  .chip {
    max-width: 16rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    padding: 0.05rem 0.5em;
    border-radius: 999px;
    border: 1px solid var(--border-strong);
    color: var(--ink-muted);
  }
  .chip.steer {
    border-color: var(--live);
    color: var(--live-ink);
  }
  .ext-status {
    color: var(--think);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 18rem;
  }
  .tele {
    color: var(--ink-muted);
    white-space: nowrap;
  }
  .ctx.hot {
    color: var(--warn);
    font-weight: 600;
  }
  .mini {
    padding: 0.1rem 0.5rem;
    font-size: var(--text-xs);
    letter-spacing: 0.02em;
    color: var(--ink-muted);
    border: 1px solid var(--border);
    border-radius: var(--r-sm);
    background: var(--surface);
  }
  .mini:hover:not(:disabled) {
    color: var(--ink);
    border-color: var(--border-strong);
  }
  .mini:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .controls {
    position: relative;
  }
  .panel {
    position: absolute;
    z-index: 40;
    bottom: calc(100% + 6px);
    right: 0;
    display: flex;
    flex-direction: column;
    gap: 0.7rem;
    width: 16.5rem;
    padding: 0.8rem;
    background: var(--surface);
    border: 1px solid var(--border-strong);
    border-radius: var(--r-md);
    box-shadow: var(--shadow);
  }
  .ctl {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.8rem;
  }
  .compact-now {
    width: 100%;
    padding: 0.35rem;
  }

  /* narrow screens: keep the foot to one row — the dot, state label, ctx and
     controls survive; token/cost figures don't fit and go */
  @media (max-width: 640px) {
    .bar,
    .left,
    .right {
      gap: 0.6em;
    }
    .tele.tokens,
    .tele.cost {
      display: none;
    }
    .ext-status {
      max-width: 8rem;
    }
  }
</style>
