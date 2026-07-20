<script lang="ts">
  // Operator voice: mono, marked with the prompt glyph. Fork rewinds the
  // session to this message.
  import type { UserItem } from "../lib/feed";

  let {
    item,
    onFork,
  }: {
    item: UserItem;
    onFork: (() => void) | null;
  } = $props();

  function timeOf(ts?: number): string {
    if (!ts) return "";
    return new Date(ts).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  }
</script>

<div class="user">
  <div class="meta">
    <span class="glyph" aria-hidden="true">❯</span>
    <span class="label">you</span>
    {#if item.imageCount > 0}
      <span class="images">{item.imageCount} image{item.imageCount > 1 ? "s" : ""}</span>
    {/if}
    <span class="time">{timeOf(item.ts)}</span>
    {#if onFork}
      <button type="button" class="fork" title="Fork the session from this message" onclick={onFork}>
        fork ⑂
      </button>
    {/if}
  </div>
  <div class="text">{item.text}</div>
</div>

<style>
  .user {
    padding: 0.55rem 0.85rem;
    border-left: 2px solid var(--ink);
    background: var(--surface);
    border-radius: 0 var(--r-md) var(--r-md) 0;
  }
  .meta {
    display: flex;
    align-items: baseline;
    gap: 0.7em;
  }
  .glyph {
    color: var(--live);
    font-weight: 600;
  }
  .images {
    font-size: var(--text-xs);
    color: var(--ink-faint);
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0 0.5em;
  }
  .time {
    margin-left: auto;
    font-size: var(--text-xs);
    color: var(--ink-faint);
  }
  .fork {
    font-size: var(--text-xs);
    color: var(--ink-faint);
    opacity: 0;
    transition: opacity 80ms ease;
  }
  .user:hover .fork,
  .fork:focus-visible {
    opacity: 1;
  }
  .fork:hover {
    color: var(--live-ink);
  }
  .text {
    margin-top: 0.25rem;
    font-family: var(--font-mono);
    font-size: var(--text-md);
    white-space: pre-wrap;
    word-break: break-word;
  }
</style>
