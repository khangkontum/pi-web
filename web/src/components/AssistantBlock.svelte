<script lang="ts">
  // Agent voice: prose in print, with thinking and tool runs inline in the
  // order the model produced them.
  import Prose from "./Prose.svelte";
  import ThinkingBlock from "./ThinkingBlock.svelte";
  import ToolBlock from "./ToolBlock.svelte";
  import type { AssistantItem, ToolRun } from "../lib/feed";

  let {
    item,
    tools,
    onOpenFile,
  }: {
    item: AssistantItem;
    tools: Record<string, ToolRun>;
    onOpenFile: (path: string) => void;
  } = $props();
</script>

<div class="assistant">
  {#each item.blocks as block, i (i)}
    {#if block.type === "text" && block.text}
      <Prose content={block.text} />
    {:else if block.type === "thinking"}
      <ThinkingBlock
        thinking={block.thinking ?? ""}
        streaming={item.streaming && i === item.blocks.length - 1}
      />
    {:else if block.type === "toolCall" && tools[block.id]}
      <ToolBlock tool={tools[block.id]} {onOpenFile} />
    {/if}
  {/each}
  {#if item.streaming}
    <span class="caret" aria-hidden="true"></span>
  {/if}
  {#if item.error}
    <div class="error">
      <span class="label err-label">error</span>
      <span class="detail">{item.error}</span>
    </div>
  {/if}
</div>

<style>
  .assistant {
    min-width: 0;
  }
  .caret {
    display: inline-block;
    width: 0.55em;
    height: 1em;
    margin-left: 2px;
    vertical-align: text-bottom;
    background: var(--live);
    animation: breathe 1.1s ease-in-out infinite;
  }
  .error {
    display: flex;
    align-items: baseline;
    gap: 0.7em;
    margin-top: 0.6rem;
    padding: 0.5rem 0.75rem;
    border-left: 2px solid var(--err);
    background: var(--err-soft);
    border-radius: 0 var(--r-md) var(--r-md) 0;
  }
  .err-label {
    color: var(--err);
  }
  .detail {
    font-size: var(--text-sm);
    word-break: break-word;
  }
</style>
