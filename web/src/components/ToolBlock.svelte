<script lang="ts">
  // One tool call, updating in place while it runs. The head line is the
  // record; bodies (diffs for edit/write, ANSI output for the rest) stay
  // folded until opened — except errors, which always open. Output is
  // clamped to a tail until expanded.
  import AnsiText from "./AnsiText.svelte";
  import DiffView from "./DiffView.svelte";
  import { deriveToolDiff } from "../lib/diff";
  import type { ToolRun } from "../lib/feed";
  import { toolSummaryText } from "../lib/protocol";

  let {
    tool,
    onOpenFile,
  }: {
    tool: ToolRun;
    onOpenFile: (path: string) => void;
  } = $props();

  const CLAMP_LINES = 22;

  let expanded = $state(false);
  // per-block override of the default fold; null = closed unless errored
  let open = $state<boolean | null>(null);

  const diff = $derived(deriveToolDiff(tool.name, tool.arguments));
  const summary = $derived(toolSummaryText(tool.arguments));
  const outputLines = $derived(tool.output ? tool.output.split("\n") : []);
  const collapsed = $derived(open === null ? tool.status !== "error" : !open);
  const clamped = $derived(!expanded && outputLines.length > CLAMP_LINES + 4);
  const shownOutput = $derived(
    clamped ? outputLines.slice(-CLAMP_LINES).join("\n") : tool.output,
  );
</script>

<div class="tool" class:running={tool.status === "running"} class:error={tool.status === "error"}>
  <div class="head">
    <span class="status" aria-label={tool.status}>
      {#if tool.status === "running"}<span class="pulse"></span>
      {:else if tool.status === "error"}✕
      {:else}✓{/if}
    </span>
    <span class="name">{tool.name}</span>
    {#if tool.path}
      <button type="button" class="path" title="Preview {tool.path}" onclick={() => onOpenFile(tool.path!)}>
        {tool.path}
      </button>
    {:else if summary}
      <span class="summary" title={summary}>{summary}</span>
    {/if}
    {#if diff}
      <span class="stats"><span class="adds">+{diff.adds}</span> <span class="dels">−{diff.dels}</span></span>
    {/if}
    {#if diff || tool.output}
      <button
        type="button"
        class="fold"
        title={collapsed ? (diff ? "Show diff" : "Show output") : diff ? "Hide diff" : "Hide output"}
        onclick={() => (open = collapsed)}
      >
        {#if !collapsed}
          ▾
        {:else if diff}
          ▸
        {:else}
          ▸ {outputLines.length} {outputLines.length === 1 ? "line" : "lines"}
        {/if}
      </button>
    {/if}
  </div>
  {#if diff && !collapsed}
    <div class="body">
      <DiffView {diff} />
    </div>
  {:else if !diff && tool.output && !collapsed}
    <div class="body">
      {#if clamped}
        <button type="button" class="more" onclick={() => (expanded = true)}>
          … show all {outputLines.length} lines
        </button>
      {/if}
      <AnsiText text={shownOutput} />
      {#if expanded && outputLines.length > CLAMP_LINES + 4}
        <button type="button" class="more" onclick={() => (expanded = false)}>collapse</button>
      {/if}
    </div>
  {/if}
</div>

<style>
  .tool {
    margin: 0.6rem 0;
    border-left: 2px solid var(--border-strong);
    padding-left: 0.85rem;
  }
  .tool.error {
    border-left-color: var(--err);
  }
  /* running state: the left rule carries a moving sheen — motion as agent state */
  @keyframes sheen {
    from {
      background-position: 0 -40px;
    }
    to {
      background-position: 0 40px;
    }
  }
  .tool.running {
    border-left-color: transparent;
    background-image: linear-gradient(var(--live) 40%, var(--border-strong) 60%);
    background-size: 2px 40px;
    background-repeat: repeat-y;
    background-position: 0 0;
    animation: sheen 1.2s linear infinite;
  }
  @media (prefers-reduced-motion: reduce) {
    .tool.running {
      animation: none;
      background: none;
      border-left-color: var(--live);
    }
  }
  .head {
    display: flex;
    align-items: baseline;
    gap: 0.6em;
    min-width: 0;
    font-size: var(--text-sm);
  }
  .status {
    flex: none;
    color: var(--ok);
  }
  .error .status {
    color: var(--err);
  }
  .name {
    flex: none;
    font-weight: 600;
    letter-spacing: 0.02em;
    color: var(--ink-muted);
  }
  .summary,
  .path {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--ink-faint);
    font-size: var(--text-sm);
  }
  .path {
    text-align: left;
    color: var(--live-ink);
  }
  .path:hover {
    text-decoration: underline;
  }
  .stats {
    flex: none;
    font-size: var(--text-xs);
  }
  .adds {
    color: var(--ok);
  }
  .dels {
    color: var(--err);
  }
  .fold {
    flex: none;
    font-size: var(--text-xs);
    color: var(--ink-faint);
  }
  .fold:hover {
    color: var(--live-ink);
  }
  .body {
    margin-top: 0.35rem;
    padding: 0.5rem 0.7rem;
    background: var(--code-bg);
    border: 1px solid var(--border);
    border-radius: var(--r-md);
    overflow-x: auto;
  }
  .more {
    display: block;
    margin: 0.15rem 0;
    font-size: var(--text-xs);
    color: var(--live-ink);
  }
  .more:hover {
    text-decoration: underline;
  }
</style>
