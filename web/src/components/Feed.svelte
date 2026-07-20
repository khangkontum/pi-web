<script lang="ts">
  // The session feed: virtualized item stream + the spine scrubber. Sticky
  // follow lives in VirtualFeed; the spine and jump pill are its controls.
  import AssistantBlock from "./AssistantBlock.svelte";
  import BashBlock from "./BashBlock.svelte";
  import CompactionBlock from "./CompactionBlock.svelte";
  import NoticeBlock from "./NoticeBlock.svelte";
  import Spine from "./Spine.svelte";
  import ToolBlock from "./ToolBlock.svelte";
  import UserBlock from "./UserBlock.svelte";
  import VirtualFeed from "./VirtualFeed.svelte";
  import { api } from "../lib/api";
  import { rail } from "../lib/rail.svelte";
  import { router } from "../lib/router.svelte";
  import { session } from "../lib/session.svelte";
  import { toasts } from "../lib/toasts.svelte";
  import type { FeedItem } from "../lib/feed";

  let { onOpenFile }: { onOpenFile: (path: string) => void } = $props();

  const view = $derived(session.view);

  let follow = $state(true);
  let first = $state(0);
  let last = $state(0);
  let feedEl = $state<ReturnType<typeof VirtualFeed<FeedItem>> | null>(null);

  function onRange(f: number, l: number): void {
    first = f;
    last = l;
  }

  // Fork: rewind into a fresh session file at the nth user message. pi's
  // fork-messages list is ordered user messages on the active branch — match
  // by ordinal, verified by text.
  async function fork(itemIndex: number): Promise<void> {
    const item = view.items[itemIndex];
    if (!item || item.kind !== "user" || !session.id) return;
    let ordinal = 0;
    for (let i = 0; i < itemIndex; i++) {
      if (view.items[i].kind === "user") ordinal++;
    }
    try {
      const { messages } = await api.forkMessages(session.id);
      let entry: (typeof messages)[number] | undefined = messages?.[ordinal];
      if (!entry || entry.text.trim() !== item.text.trim()) {
        entry = messages?.find((m) => m.text.trim() === item.text.trim());
      }
      if (!entry) {
        toasts.error("This message is not on the active branch — cannot fork from it.");
        return;
      }
      const currentId = session.id;
      const resp = (await api.fork(currentId, entry.entryId)) as {
        result?: { text?: string; cancelled?: boolean };
      };
      if (resp.result?.cancelled) {
        toasts.show("Fork cancelled by an extension.", "warning");
        return;
      }
      await rail.refresh();
      const group = rail.groups.find((g) => g.cwd === view.cwd);
      const newest = group?.sessions[0];
      if (newest && newest.id !== currentId) {
        router.openSession(newest.id);
      } else {
        session.reopen();
      }
      // pi hands back the forked message so the operator can revise and
      // resend; deliver it after the reopened snapshot settles the composer
      const text = resp.result?.text;
      if (text) setTimeout(() => session.onSetEditorText?.(text), 150);
      toasts.show("Forked — your message is back in the composer to revise.");
    } catch (err) {
      toasts.error(err instanceof Error ? err.message : String(err));
    }
  }

  export function jumpToBottom(): void {
    feedEl?.jumpToBottom();
  }
</script>

<div class="feed">
  {#if session.streamError}
    <div class="stream-error">
      <span class="label err-label">stream error</span>
      <span>{session.streamError}</span>
    </div>
  {/if}

  {#if view.items.length === 0}
    <div class="empty">
      <div class="mark" aria-hidden="true">
        <span></span><span></span><span></span><span></span><span></span>
      </div>
      {#if session.pending}
        <p class="where">new session in <strong>{session.pending.cwd ?? "the workspace"}</strong></p>
        <p class="hint">The session is created when you send the first message.</p>
      {:else if session.id && !session.connected && !session.streamError}
        <p class="hint"><span class="pulse"></span> opening session…</p>
      {:else if session.id}
        <p class="hint">No messages yet. Type below to begin.</p>
      {:else}
        <p class="where">pi-web</p>
        <p class="hint">Pick a session from the rail, or press ⌘K for a new one.</p>
      {/if}
      <p class="hint keys">Enter sends · ! runs shell · @ inserts a file</p>
    </div>
  {:else}
    <VirtualFeed items={view.items} resetKey={view.id} bind:follow {onRange} bind:this={feedEl}>
      {#snippet row(item: FeedItem, index: number)}
        <div class="row">
          {#if item.kind === "user"}
            <UserBlock {item} onFork={session.id ? () => fork(index) : null} />
          {:else if item.kind === "assistant"}
            <AssistantBlock {item} tools={view.tools} {onOpenFile} />
          {:else if item.kind === "bash"}
            <BashBlock {item} />
          {:else if item.kind === "tool" && view.tools[item.id]}
            <ToolBlock tool={view.tools[item.id]} {onOpenFile} />
          {:else if item.kind === "compaction"}
            <CompactionBlock {item} />
          {:else if item.kind === "notice"}
            <NoticeBlock {item} />
          {/if}
        </div>
      {/snippet}
    </VirtualFeed>

    <Spine {view} {first} {last} onJump={(i) => feedEl?.scrollToIndex(i)} />

    {#if !follow}
      <button type="button" class="jump" onclick={() => feedEl?.jumpToBottom()}>
        ↓ latest{view.streaming ? " · streaming" : ""}
      </button>
    {/if}
  {/if}
</div>

<style>
  .feed {
    position: relative;
    display: flex;
    height: 100%;
    min-height: 0;
  }
  .feed :global(.viewport) {
    flex: 1;
    min-width: 0;
  }
  .row {
    max-width: var(--measure);
    margin: 0 auto;
    padding: 0.55rem 1.25rem;
  }
  .stream-error {
    position: absolute;
    top: 0;
    left: 0;
    right: var(--spine-w);
    z-index: 10;
    display: flex;
    gap: 0.8em;
    align-items: baseline;
    padding: 0.4rem 1.25rem;
    font-size: var(--text-sm);
    background: var(--err-soft);
    border-bottom: 1px solid var(--err);
  }
  .err-label {
    color: var(--err);
  }
  .empty {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 0.4rem;
    color: var(--ink-muted);
    padding: 2rem;
    text-align: center;
  }
  /* the identity mark: a small spine trace */
  .mark {
    display: flex;
    flex-direction: column;
    gap: 3px;
    width: 22px;
    margin-bottom: 1rem;
  }
  .mark span {
    height: 3px;
    border-radius: 1px;
    background: var(--border-strong);
  }
  .mark span:nth-child(1) {
    background: var(--ink);
    width: 60%;
  }
  .mark span:nth-child(2) {
    background: color-mix(in srgb, var(--live) 45%, var(--border-strong));
    height: 9px;
  }
  .mark span:nth-child(4) {
    background: color-mix(in srgb, var(--live) 45%, var(--border-strong));
    height: 6px;
  }
  .mark span:nth-child(5) {
    background: var(--live);
    width: 40%;
  }
  .where {
    margin: 0;
    font-size: var(--text-md);
    color: var(--ink);
  }
  .where strong {
    font-weight: 600;
  }
  .hint {
    margin: 0;
    font-size: var(--text-sm);
  }
  .keys {
    margin-top: 0.8rem;
    color: var(--ink-faint);
  }
  .jump {
    position: absolute;
    bottom: 0.9rem;
    left: 50%;
    transform: translateX(-50%);
    padding: 0.3rem 0.85rem;
    font-size: var(--text-xs);
    letter-spacing: 0.02em;
    color: var(--surface);
    background: var(--ink);
    border-radius: 999px;
    box-shadow: var(--shadow);
  }
  .jump:hover {
    background: var(--live-ink);
  }
</style>
