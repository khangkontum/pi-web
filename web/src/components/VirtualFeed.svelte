<script lang="ts" generics="T">
  import type { Snippet } from "svelte";

  // Windowed feed renderer. Only the items near the viewport are in the DOM,
  // so a 1000-message session scrolls smoothly. Heights start as an estimate
  // and are corrected by a ResizeObserver as items render; corrections above
  // the viewport are compensated so the reading position never jumps.
  //
  // Sticky follow: `follow` turns off the moment the user scrolls away from
  // the bottom and back on when they return — streaming output never yanks
  // them up while reading history.
  let {
    items,
    resetKey = "",
    estimate = 96,
    overscan = 600,
    follow = $bindable(true),
    onRange,
    row,
  }: {
    items: T[];
    // clearing signal: session switches invalidate all measured heights
    resetKey?: string;
    estimate?: number;
    overscan?: number;
    follow?: boolean;
    onRange?: (first: number, last: number) => void;
    row: Snippet<[T, number]>;
  } = $props();

  let viewport = $state<HTMLElement | null>(null);
  let scrollTop = $state(0);
  let viewH = $state(0);

  let heights: number[] = [];
  let lastResetKey = "";
  // version bumps when measurements change so derived offsets recompute
  let measureVersion = $state(0);

  $effect(() => {
    if (resetKey !== lastResetKey) {
      lastResetKey = resetKey;
      heights = [];
      measureVersion++;
      follow = true;
    }
  });

  const offsets = $derived.by(() => {
    void measureVersion;
    const out = new Array<number>(items.length + 1);
    out[0] = 0;
    for (let i = 0; i < items.length; i++) {
      out[i + 1] = out[i] + (heights[i] ?? estimate);
    }
    return out;
  });

  const totalH = $derived(offsets[items.length] ?? 0);

  function indexAt(y: number): number {
    let lo = 0;
    let hi = items.length - 1;
    while (lo < hi) {
      const mid = (lo + hi + 1) >> 1;
      if (offsets[mid] <= y) lo = mid;
      else hi = mid - 1;
    }
    return lo;
  }

  const range = $derived.by(() => {
    if (items.length === 0) return { first: 0, last: -1 };
    const first = indexAt(Math.max(0, scrollTop - overscan));
    const last = indexAt(Math.min(totalH - 1, scrollTop + viewH + overscan));
    return { first, last };
  });

  const visible = $derived.by(() => {
    if (items.length === 0) return { first: 0, last: -1 };
    return { first: indexAt(scrollTop), last: indexAt(Math.max(0, scrollTop + viewH - 1)) };
  });

  $effect(() => {
    if (onRange && items.length > 0) onRange(visible.first, visible.last);
  });

  // --- measuring ------------------------------------------------------------

  let suppressScrollSync = false;

  const ro = new ResizeObserver((entries) => {
    let deltaAbove = 0;
    let changed = false;
    for (const entry of entries) {
      const el = entry.target as HTMLElement;
      const idx = Number(el.dataset.index);
      if (Number.isNaN(idx)) continue;
      const h = el.offsetHeight;
      if (h > 0 && heights[idx] !== h) {
        const old = heights[idx] ?? estimate;
        if (!follow && viewport && offsets[idx] + Math.min(old, h) < viewport.scrollTop) {
          deltaAbove += h - old;
        }
        heights[idx] = h;
        changed = true;
      }
    }
    if (!changed) return;
    measureVersion++;
    if (follow) {
      queueMicrotask(scrollToBottom);
    } else if (deltaAbove !== 0 && viewport) {
      // keep the reading position anchored when estimates above correct
      suppressScrollSync = true;
      viewport.scrollTop += deltaAbove;
    }
  });

  function measure(el: HTMLElement, index: number) {
    el.dataset.index = String(index);
    ro.observe(el);
    return {
      update(next: number) {
        el.dataset.index = String(next);
      },
      destroy() {
        ro.unobserve(el);
      },
    };
  }

  $effect(() => () => ro.disconnect());

  // --- scrolling ------------------------------------------------------------

  function scrollToBottom(): void {
    if (!viewport) return;
    suppressScrollSync = true;
    viewport.scrollTop = viewport.scrollHeight;
  }

  function onScroll(): void {
    if (!viewport) return;
    scrollTop = viewport.scrollTop;
    if (suppressScrollSync) {
      suppressScrollSync = false;
      return;
    }
    follow = viewport.scrollTop + viewport.clientHeight >= viewport.scrollHeight - 60;
  }

  // follow new content: items growing or the tail item growing both raise
  // totalH; while following, stay glued to the bottom.
  $effect(() => {
    void totalH;
    void items.length;
    if (follow) scrollToBottom();
  });

  export function scrollToIndex(index: number): void {
    if (!viewport || items.length === 0) return;
    const i = Math.max(0, Math.min(items.length - 1, index));
    follow = i >= items.length - 1;
    suppressScrollSync = !follow;
    viewport.scrollTop = Math.max(0, offsets[i] - 12);
    scrollTop = viewport.scrollTop;
  }

  export function jumpToBottom(): void {
    follow = true;
    scrollToBottom();
  }
</script>

<div
  class="viewport"
  bind:this={viewport}
  bind:clientHeight={viewH}
  onscroll={onScroll}
>
  <div class="space" style:height="{totalH}px">
    <div class="slice" style:transform="translateY({offsets[range.first] ?? 0}px)">
      {#each items.slice(range.first, range.last + 1) as item, k (range.first + k)}
        <div use:measure={range.first + k}>
          {@render row(item, range.first + k)}
        </div>
      {/each}
    </div>
  </div>
</div>

<style>
  .viewport {
    height: 100%;
    overflow-y: auto;
    overscroll-behavior: contain;
  }
  .space {
    position: relative;
    min-height: 100%;
  }
  .slice {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    will-change: transform;
  }
</style>
