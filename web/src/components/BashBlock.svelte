<script lang="ts">
  // An operator `!` command run in the session cwd. The agent sees it on its
  // next prompt; the feed shows it as part of the record.
  import AnsiText from "./AnsiText.svelte";
  import type { BashItem } from "../lib/feed";

  let { item }: { item: BashItem } = $props();
</script>

<div class="bash" class:failed={item.exitCode !== 0}>
  <div class="head">
    <span class="glyph" aria-hidden="true">!</span>
    <span class="command">{item.command}</span>
    {#if item.exitCode !== 0}
      <span class="exit">exit {item.exitCode}</span>
    {/if}
  </div>
  {#if item.output}
    <div class="out">
      <AnsiText text={item.output} />
    </div>
  {/if}
</div>

<style>
  .bash {
    padding: 0.55rem 0.85rem;
    background: var(--code-bg);
    border: 1px solid var(--border);
    border-radius: var(--r-md);
  }
  .bash.failed {
    border-left: 2px solid var(--err);
  }
  .head {
    display: flex;
    align-items: baseline;
    gap: 0.6em;
    font-size: var(--text-md);
  }
  .glyph {
    color: var(--warn);
    font-weight: 600;
  }
  .command {
    font-weight: 500;
    word-break: break-word;
  }
  .exit {
    margin-left: auto;
    flex: none;
    font-size: var(--text-xs);
    color: var(--err);
  }
  .out {
    margin-top: 0.4rem;
    max-height: 24rem;
    overflow-y: auto;
  }
</style>
