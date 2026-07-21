<script lang="ts">
  // The file explorer pane, rooted at the session's working directory.
  // Changed files are tinted from /api/git, refreshed when a turn settles.
  import TreeNode from "./TreeNode.svelte";
  import { api } from "../lib/api";
  import { session } from "../lib/session.svelte";

  let {
    root,
    onOpenFile,
    onClose,
  }: {
    root: string;
    onOpenFile: (path: string) => void;
    onClose: () => void;
  } = $props();

  // absolute file path → status letter; absolute dir path → true
  let gitFiles = $state<Record<string, string>>({});
  let gitDirs = $state<Record<string, boolean>>({});

  $effect(() => {
    if (session.view.streaming) return; // re-runs (and refetches) on settle
    api
      .git(root)
      .then((g) => {
        const files: Record<string, string> = {};
        const dirs: Record<string, boolean> = {};
        for (const [rel, st] of Object.entries(g.changes ?? {})) {
          files[`${root}/${rel}`] = st;
          let dir = rel;
          for (;;) {
            const cut = dir.lastIndexOf("/");
            if (cut < 0) break;
            dir = dir.slice(0, cut);
            dirs[`${root}/${dir}`] = true;
          }
        }
        gitFiles = files;
        gitDirs = dirs;
      })
      .catch(() => {});
  });
</script>

<aside class="pane" aria-label="File explorer">
  <div class="head">
    <span class="label" title={root}>{root.split("/").pop() || root}</span>
    <button type="button" class="x" aria-label="Close explorer" onclick={onClose}>✕</button>
  </div>
  <div class="tree">
    {#key root}
      <TreeNode path={root} depth={0} {onOpenFile} {gitFiles} {gitDirs} />
    {/key}
  </div>
</aside>

<style>
  .pane {
    display: flex;
    flex-direction: column;
    width: 268px;
    flex: none;
    height: 100%;
    border-left: 1px solid var(--border);
    background: var(--surface);
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.55rem 0.8rem;
    border-bottom: 1px solid var(--border);
  }
  .x {
    color: var(--ink-faint);
    font-size: var(--text-sm);
  }
  .x:hover {
    color: var(--ink);
  }
  .tree {
    flex: 1;
    overflow: auto;
    padding: 0.4rem 0.3rem 1rem;
  }
  @media (max-width: 900px) {
    .pane {
      position: fixed;
      right: 0;
      top: 0;
      bottom: 0;
      z-index: 55;
      box-shadow: var(--shadow);
    }
  }
</style>
