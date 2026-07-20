<script lang="ts">
  // A compaction is a cut in the session's memory — rendered as a labeled
  // seam in the feed, with the surviving summary readable underneath.
  import Prose from "./Prose.svelte";
  import type { CompactionItem } from "../lib/feed";

  let { item }: { item: CompactionItem } = $props();

  let showSummary = $state(false);

  function fmt(n?: number): string {
    if (n === undefined) return "?";
    return n >= 1000 ? `${(n / 1000).toFixed(n >= 10000 ? 0 : 1)}k` : String(n);
  }
</script>

<div class="compaction" class:failed={!!item.error}>
  <div class="seam" aria-hidden="true"></div>
  <div class="row">
    {#if item.running}
      <span class="pulse"></span>
      <span class="label live-label">compacting context ({item.reason})…</span>
    {:else if item.aborted}
      <span class="label">compaction aborted</span>
    {:else if item.error}
      <span class="label err-label">compaction failed</span>
      <span class="detail">{item.error}</span>
    {:else}
      <span class="label live-label">context compacted ({item.reason})</span>
      <span class="detail">{fmt(item.tokensBefore)} → {fmt(item.tokensAfter)} tokens</span>
      {#if item.summary}
        <button type="button" class="show" onclick={() => (showSummary = !showSummary)}>
          {showSummary ? "hide summary" : "read summary"}
        </button>
      {/if}
    {/if}
  </div>
  {#if showSummary && item.summary}
    <div class="summary">
      <Prose content={item.summary} muted />
    </div>
  {/if}
  <div class="seam" aria-hidden="true"></div>
</div>

<style>
  .compaction {
    margin: 0.4rem 0;
  }
  .seam {
    height: 1px;
    background: repeating-linear-gradient(
      90deg,
      var(--border-strong) 0 6px,
      transparent 6px 12px
    );
  }
  .failed .seam {
    background: repeating-linear-gradient(90deg, var(--err) 0 6px, transparent 6px 12px);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 0.8em;
    padding: 0.45rem 0;
    flex-wrap: wrap;
  }
  .live-label {
    color: var(--live-ink);
  }
  .err-label {
    color: var(--err);
  }
  .detail {
    font-size: var(--text-xs);
    color: var(--ink-muted);
  }
  .show {
    font-size: var(--text-xs);
    color: var(--live-ink);
  }
  .show:hover {
    text-decoration: underline;
  }
  .summary {
    margin: 0.3rem 0 0.6rem;
    padding: 0.7rem 0.9rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--r-md);
  }
</style>
