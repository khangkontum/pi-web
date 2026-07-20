<script lang="ts" module>
  // one fetch per base per app session; the index is cheap and refetching on
  // every keystroke would not be
  const cache = new Map<string, { files: string[]; truncated: boolean }>();
</script>

<script lang="ts">
  // The @-file finder above the composer. The file index comes from
  // /api/files (git-aware, capped server-side); matching is client-side
  // fuzzy. The composer owns the keyboard and forwards keys here.
  import { api } from "../lib/api";
  import { filterFuzzy, type FuzzyMatch } from "../lib/fuzzy";

  let {
    base,
    query,
    onPick,
    onClose,
  }: {
    base: string | null;
    query: string;
    onPick: (path: string) => void;
    onClose: () => void;
  } = $props();

  let files = $state<string[]>([]);
  let truncated = $state(false);
  let failed = $state<string | null>(null);
  let active = $state(0);
  let listEl = $state<HTMLElement | null>(null);

  $effect(() => {
    const key = base ?? "";
    const hit = cache.get(key);
    if (hit) {
      files = hit.files;
      truncated = hit.truncated;
      return;
    }
    api
      .files(base)
      .then((res) => {
        cache.set(key, { files: res.files ?? [], truncated: !!res.truncated });
        files = res.files ?? [];
        truncated = !!res.truncated;
      })
      .catch((err) => {
        failed = err instanceof Error ? err.message : String(err);
      });
  });

  const matches = $derived<FuzzyMatch[]>(filterFuzzy(query, files, 40));

  $effect(() => {
    void matches;
    active = 0;
  });

  // handleKey returns true when the key was consumed.
  export function handleKey(e: KeyboardEvent): boolean {
    if (e.key === "ArrowDown") {
      active = Math.min(matches.length - 1, active + 1);
      scrollActive();
      return true;
    }
    if (e.key === "ArrowUp") {
      active = Math.max(0, active - 1);
      scrollActive();
      return true;
    }
    if (e.key === "Enter" || e.key === "Tab") {
      if (matches[active]) {
        onPick(matches[active].text);
        return true;
      }
      onClose();
      return false;
    }
    if (e.key === "Escape") {
      onClose();
      return true;
    }
    return false;
  }

  function scrollActive(): void {
    listEl?.children[active]?.scrollIntoView({ block: "nearest" });
  }

  function highlight(m: FuzzyMatch): { text: string; hit: boolean }[] {
    const set = new Set(m.positions);
    const parts: { text: string; hit: boolean }[] = [];
    for (let i = 0; i < m.text.length; i++) {
      const hit = set.has(i);
      const last = parts[parts.length - 1];
      if (last && last.hit === hit) last.text += m.text[i];
      else parts.push({ text: m.text[i], hit });
    }
    return parts;
  }
</script>

<div class="finder" role="listbox" aria-label="Files">
  {#if failed}
    <div class="note">file index unavailable: {failed}</div>
  {:else if matches.length === 0}
    <div class="note">{files.length === 0 ? "indexing…" : "no matching files"}</div>
  {:else}
    <div class="list" bind:this={listEl}>
      {#each matches as m, i (m.text)}
        <button
          type="button"
          role="option"
          aria-selected={i === active}
          class="item"
          class:active={i === active}
          onpointerenter={() => (active = i)}
          onclick={() => onPick(m.text)}
        >
          {#each highlight(m) as part}
            {#if part.hit}<mark>{part.text}</mark>{:else}{part.text}{/if}
          {/each}
        </button>
      {/each}
    </div>
    {#if truncated}
      <div class="note">index truncated at the server cap — narrow your query</div>
    {/if}
  {/if}
</div>

<style>
  .finder {
    position: absolute;
    bottom: calc(100% + 6px);
    left: 0;
    right: 0;
    z-index: 45;
    max-height: 310px;
    display: flex;
    flex-direction: column;
    background: var(--surface);
    border: 1px solid var(--border-strong);
    border-radius: var(--r-md);
    box-shadow: var(--shadow);
    overflow: hidden;
  }
  .list {
    overflow-y: auto;
    padding: 4px;
  }
  .item {
    display: block;
    width: 100%;
    padding: 0.3rem 0.5rem;
    font-size: var(--text-sm);
    text-align: left;
    border-radius: var(--r-sm);
    color: var(--ink-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .item.active {
    background: var(--accent-hover);
    color: var(--ink);
  }
  mark {
    background: none;
    color: var(--live-ink);
    font-weight: 600;
  }
  .note {
    padding: 0.4rem 0.6rem;
    font-size: var(--text-xs);
    color: var(--ink-faint);
    border-top: 1px solid var(--border);
  }
</style>
