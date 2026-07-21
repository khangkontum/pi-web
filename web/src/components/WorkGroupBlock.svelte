<script lang="ts">
  // A folded tool burst: "… N earlier steps" above the last two members'
  // tool rows, so the live tail reads like a terminal while history stays
  // short. Expanding replays the whole burst in full — thinking interleaved
  // in the order the model produced it. A member that is still streaming
  // renders complete (thinking, caret and all) so the operator can watch.
  import AssistantBlock from "./AssistantBlock.svelte";
  import ToolBlock from "./ToolBlock.svelte";
  import { GROUP_TAIL, type GroupEntry } from "../lib/workgroups";
  import type { AssistantItem, FeedItem, ToolRun } from "../lib/feed";

  let {
    entry,
    items,
    tools,
    onOpenFile,
  }: {
    entry: GroupEntry;
    items: FeedItem[];
    tools: Record<string, ToolRun>;
    onOpenFile: (path: string) => void;
  } = $props();

  let open = $state(false);

  const members = $derived(
    entry.indices
      .map((i) => items[i])
      .filter((m): m is AssistantItem => m !== undefined && m.kind === "assistant"),
  );
  const tail = $derived(members.slice(-GROUP_TAIL));
</script>

<div class="workgroup">
  <button type="button" class="foldline" aria-expanded={open} onclick={() => (open = !open)}>
    <span class="chev">{open ? "▾" : "…"}</span>
    {entry.hiddenSteps} earlier {entry.hiddenSteps === 1 ? "step" : "steps"}
  </button>
  {#if open}
    {#each members as member, i (entry.indices[i])}
      <AssistantBlock item={member} {tools} {onOpenFile} />
    {/each}
  {:else}
    {#each tail as member, i (i)}
      {#if member.streaming}
        <AssistantBlock item={member} {tools} {onOpenFile} />
      {:else}
        {#each member.blocks as block, j (j)}
          {#if block.type === "toolCall" && tools[block.id]}
            <ToolBlock tool={tools[block.id]} {onOpenFile} />
          {/if}
        {/each}
      {/if}
    {/each}
  {/if}
</div>

<style>
  .workgroup {
    min-width: 0;
  }
  .foldline {
    display: flex;
    align-items: baseline;
    gap: 0.6em;
    width: 100%;
    margin: 0.6rem 0 0.2rem;
    border-left: 2px solid var(--border);
    padding: 0.05rem 0 0.05rem 0.85rem;
    font-size: var(--text-sm);
    color: var(--ink-faint);
    text-align: left;
  }
  .foldline:hover {
    color: var(--live-ink);
    border-left-color: var(--border-strong);
  }
  .chev {
    flex: none;
  }
</style>
