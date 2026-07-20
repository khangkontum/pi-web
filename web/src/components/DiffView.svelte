<script lang="ts">
  // Diff rows from lib/diff rendered as a unified diff. Rows are data;
  // this template is the only place they become DOM.
  import type { ToolDiff } from "../lib/diff";

  let { diff }: { diff: ToolDiff } = $props();
</script>

<div class="diff">
  {#each diff.rows as row}
    {#if row.kind === "gap"}
      <div class="row gap"><span class="sign">·</span><span class="text">···</span></div>
    {:else}
      <div class="row {row.kind}">
        <span class="sign">{row.kind === "add" ? "+" : row.kind === "del" ? "-" : " "}</span>
        <span class="text">{row.text || " "}</span>
      </div>
    {/if}
  {/each}
</div>

<style>
  .diff {
    font-family: var(--font-mono);
    font-size: var(--text-sm);
    line-height: 1.5;
    overflow-x: auto;
  }
  .row {
    display: flex;
    min-width: max-content;
  }
  .sign {
    flex: none;
    width: 1.4em;
    text-align: center;
    color: var(--ink-faint);
    user-select: none;
  }
  .text {
    white-space: pre;
  }
  .row.add {
    background: color-mix(in srgb, var(--ok) 14%, transparent);
  }
  .row.add .sign {
    color: var(--ok);
  }
  .row.del {
    background: color-mix(in srgb, var(--err) 12%, transparent);
  }
  .row.del .sign {
    color: var(--err);
  }
  .row.gap {
    color: var(--ink-faint);
  }
</style>
