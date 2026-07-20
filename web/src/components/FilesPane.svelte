<script lang="ts">
  // The file explorer pane, rooted at the session's working directory.
  import TreeNode from "./TreeNode.svelte";

  let {
    root,
    onOpenFile,
    onClose,
  }: {
    root: string;
    onOpenFile: (path: string) => void;
    onClose: () => void;
  } = $props();
</script>

<aside class="pane" aria-label="File explorer">
  <div class="head">
    <span class="label" title={root}>{root.split("/").pop() || root}</span>
    <button type="button" class="x" aria-label="Close explorer" onclick={onClose}>✕</button>
  </div>
  <div class="tree">
    {#key root}
      <TreeNode path={root} depth={0} {onOpenFile} />
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
