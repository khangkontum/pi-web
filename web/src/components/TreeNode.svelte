<script lang="ts">
  // One directory level of the explorer; directories load their children
  // lazily from /api/tree on first expand. Recursion via self-import.
  import TreeNode from "./TreeNode.svelte";
  import { api, type TreeEntry } from "../lib/api";

  let {
    path,
    depth,
    onOpenFile,
  }: {
    path: string;
    depth: number;
    onOpenFile: (path: string) => void;
  } = $props();

  let entries = $state<TreeEntry[] | null>(null);
  let failed = $state<string | null>(null);
  let openDirs = $state<Record<string, boolean>>({});

  $effect(() => {
    api
      .tree(path)
      .then((t) => {
        const list = (t.entries ?? []).filter((e) => !e.name.startsWith("."));
        list.sort((a, b) => Number(b.dir) - Number(a.dir) || a.name.localeCompare(b.name));
        entries = list;
      })
      .catch((err) => (failed = err instanceof Error ? err.message : String(err)));
  });
</script>

{#if failed}
  <div class="note" style:padding-left="{depth * 0.8 + 0.6}rem">{failed}</div>
{:else if entries === null}
  <div class="note" style:padding-left="{depth * 0.8 + 0.6}rem">…</div>
{:else}
  {#each entries as e (e.name)}
    {#if e.dir}
      <button
        type="button"
        class="row dir"
        style:padding-left="{depth * 0.8 + 0.6}rem"
        aria-expanded={!!openDirs[e.name]}
        onclick={() => (openDirs[e.name] = !openDirs[e.name])}
      >
        <span class="chev">{openDirs[e.name] ? "▾" : "▸"}</span>{e.name}/
      </button>
      {#if openDirs[e.name]}
        <TreeNode path={`${path}/${e.name}`} depth={depth + 1} {onOpenFile} />
      {/if}
    {:else}
      <button
        type="button"
        class="row file"
        style:padding-left="{depth * 0.8 + 1.35}rem"
        onclick={() => onOpenFile(`${path}/${e.name}`)}
      >
        {e.name}
      </button>
    {/if}
  {/each}
  {#if entries.length === 0}
    <div class="note" style:padding-left="{depth * 0.8 + 0.6}rem">empty</div>
  {/if}
{/if}

<style>
  .row {
    display: block;
    width: 100%;
    padding: 0.16rem 0.5rem;
    font-size: var(--text-sm);
    text-align: left;
    color: var(--ink-muted);
    border-radius: var(--r-sm);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .row:hover {
    background: var(--accent-hover);
    color: var(--ink);
  }
  .dir {
    font-weight: 500;
  }
  .chev {
    display: inline-block;
    width: 0.75rem;
    color: var(--ink-faint);
  }
  .note {
    font-size: var(--text-xs);
    color: var(--ink-faint);
    padding: 0.15rem 0.5rem;
  }
</style>
