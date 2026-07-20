<script lang="ts">
  import { untrack } from "svelte";
  import { deriveSpine, totalWeight, type SpineSegment } from "../lib/spine";
  import type { SessionView } from "../lib/feed";

  // The spine: the whole session as a vertical trace — operator ticks, agent
  // ink, tool hatch, error marks, compaction seams. Derived from the message
  // model (never the DOM) so it works under virtualization. Click or drag to
  // scrub; the viewport window shows where you are; the tail breathes while
  // the agent streams.
  let {
    view,
    first,
    last,
    onJump,
  }: {
    view: SessionView;
    first: number;
    last: number;
    onJump: (index: number) => void;
  } = $props();

  // Re-deriving on every streaming delta would be wasteful; refresh on item
  // count changes and on a slow tick while streaming.
  let tick = $state(0);
  $effect(() => {
    if (!view.streaming) return;
    const t = setInterval(() => tick++, 600);
    return () => clearInterval(t);
  });

  const segments = $derived.by(() => {
    void view.items.length;
    void view.streaming;
    void tick;
    return untrack(() => deriveSpine(view));
  });

  const total = $derived(Math.max(totalWeight(segments), 0.001));

  // per-item start/end fractions, for the viewport window
  const itemSpans = $derived.by(() => {
    const spans = new Map<number, { start: number; end: number }>();
    let acc = 0;
    for (const s of segments) {
      const span = spans.get(s.index) ?? { start: acc / total, end: acc / total };
      acc += s.weight;
      span.end = acc / total;
      spans.set(s.index, span);
    }
    return spans;
  });

  const windowTop = $derived((itemSpans.get(first)?.start ?? 0) * 100);
  const windowBottom = $derived((itemSpans.get(last)?.end ?? 1) * 100);

  let track = $state<HTMLElement | null>(null);
  let dragging = false;

  function jumpToY(clientY: number): void {
    if (!track || segments.length === 0) return;
    const rect = track.getBoundingClientRect();
    const frac = Math.min(1, Math.max(0, (clientY - rect.top) / rect.height));
    let acc = 0;
    for (const s of segments) {
      acc += s.weight / total;
      if (acc >= frac) {
        onJump(s.index);
        return;
      }
    }
    onJump(segments[segments.length - 1].index);
  }

  function onPointerDown(e: PointerEvent): void {
    dragging = true;
    track?.setPointerCapture(e.pointerId);
    jumpToY(e.clientY);
  }

  function onPointerMove(e: PointerEvent): void {
    if (dragging) jumpToY(e.clientY);
  }

  function onKeydown(e: KeyboardEvent): void {
    if (view.items.length === 0) return;
    if (e.key === "ArrowUp" || e.key === "ArrowLeft") {
      e.preventDefault();
      onJump(Math.max(0, first - 1));
    } else if (e.key === "ArrowDown" || e.key === "ArrowRight") {
      e.preventDefault();
      onJump(Math.min(view.items.length - 1, first + 1));
    } else if (e.key === "Home") {
      e.preventDefault();
      onJump(0);
    } else if (e.key === "End") {
      e.preventDefault();
      onJump(view.items.length - 1);
    }
  }

  function kindClass(s: SpineSegment): string {
    return s.error ? "error" : s.kind;
  }
</script>

<div
  class="spine"
  bind:this={track}
  role="slider"
  tabindex="0"
  aria-label="Session position"
  aria-valuemin={0}
  aria-valuemax={Math.max(0, view.items.length - 1)}
  aria-valuenow={first}
  onpointerdown={onPointerDown}
  onpointermove={onPointerMove}
  onpointerup={() => (dragging = false)}
  onkeydown={onKeydown}
>
  <div class="trace" aria-hidden="true">
    {#each segments as s, i (i)}
      <div class="seg {kindClass(s)}" style:flex-grow={s.weight}></div>
    {/each}
    {#if view.streaming}
      <div class="tail-dot"></div>
    {/if}
  </div>
  {#if view.items.length > 0}
    <div
      class="window"
      aria-hidden="true"
      style:top="{windowTop}%"
      style:height="{Math.max(windowBottom - windowTop, 1.5)}%"
    ></div>
  {/if}
</div>

<style>
  .spine {
    position: relative;
    width: var(--spine-w);
    height: 100%;
    padding: 8px 9px;
    cursor: pointer;
    touch-action: none;
    border-left: 1px solid var(--border);
    background: var(--bg);
  }
  .trace {
    display: flex;
    flex-direction: column;
    gap: 1px;
    height: 100%;
    width: 100%;
  }
  .seg {
    width: 100%;
    min-height: 2px;
    border-radius: 1px;
  }
  .seg.user {
    background: var(--ink);
    min-height: 3px;
  }
  .seg.assistant {
    background: color-mix(in srgb, var(--live) 45%, var(--border-strong));
  }
  .seg.thinking {
    background: var(--think-soft);
  }
  .seg.tool {
    background: repeating-linear-gradient(
      45deg,
      var(--border-strong) 0 2px,
      transparent 2px 4px
    );
  }
  .seg.bash {
    background: var(--warn-soft);
  }
  .seg.compaction {
    background: repeating-linear-gradient(90deg, var(--border-strong) 0 2px, transparent 2px 4px);
    min-height: 3px;
  }
  .seg.error {
    background: var(--err);
    min-height: 3px;
  }
  .tail-dot {
    flex: none;
    align-self: center;
    margin-top: 3px;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--live);
    animation: breathe 1.6s ease-in-out infinite;
  }
  .window {
    position: absolute;
    left: 3px;
    right: 3px;
    border: 1px solid var(--live);
    border-radius: 3px;
    background: var(--live-soft);
    pointer-events: none;
  }
</style>
