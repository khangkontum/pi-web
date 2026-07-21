// Render-time folding of tool bursts. Consecutive assistant items that only
// worked — thinking and tool calls, no prose — collapse into one display
// entry: the last GROUP_TAIL members stay visible, the rest sit behind an
// "earlier steps" fold. This is presentation only; the feed model and item
// indices are untouched, and every entry maps back to its original items.
//
// The group breaks on prose, on user/bash/notice items, and on anything
// carrying an error — failures must never fold out of sight. A group only
// forms once the run holds GROUP_MIN tool calls in total (parallel calls in
// one message each count) and has more members than the visible tail, so a
// burst that is about to turn into a prose answer doesn't group and then pop
// apart, and a fold always hides at least one member.

import type { FeedItem, ToolRun } from "./feed";

export const GROUP_MIN = 3;
export const GROUP_TAIL = 2;

export interface ItemEntry {
  kind: "item";
  index: number;
}

export interface GroupEntry {
  kind: "group";
  // indices into view.items — consecutive, in feed order
  indices: number[];
  // tool calls inside the folded members (all but the last GROUP_TAIL)
  hiddenSteps: number;
}

export type DisplayEntry = ItemEntry | GroupEntry;

// isWorkItem: an assistant item that has produced no prose and no errors.
// Empty (just-started) items count as work so a streaming turn joins the
// group instead of flickering in and out of it.
export function isWorkItem(item: FeedItem, tools: Record<string, ToolRun>): boolean {
  if (item.kind !== "assistant") return false;
  if (item.error) return false;
  for (const b of item.blocks) {
    if (b.type === "thinking") continue;
    if (b.type === "toolCall") {
      if (tools[b.id]?.status === "error") return false;
      continue;
    }
    if (b.type === "text" && (!b.text || b.text.trim() === "")) continue;
    return false;
  }
  return true;
}

function toolCallCount(item: FeedItem): number {
  if (item.kind !== "assistant") return 0;
  return item.blocks.filter((b) => b.type === "toolCall").length;
}

export function deriveDisplay(
  items: FeedItem[],
  tools: Record<string, ToolRun>,
): DisplayEntry[] {
  const out: DisplayEntry[] = [];
  let run: number[] = [];

  const flush = (): void => {
    const steps = run.reduce((n, i) => n + toolCallCount(items[i]), 0);
    if (steps >= GROUP_MIN && run.length > GROUP_TAIL) {
      const folded = run.slice(0, run.length - GROUP_TAIL);
      const hiddenSteps =
        folded.reduce((n, i) => n + toolCallCount(items[i]), 0) || folded.length;
      out.push({ kind: "group", indices: run, hiddenSteps });
    } else {
      for (const i of run) out.push({ kind: "item", index: i });
    }
    run = [];
  };

  for (let i = 0; i < items.length; i++) {
    if (isWorkItem(items[i], tools)) {
      run.push(i);
    } else {
      flush();
      out.push({ kind: "item", index: i });
    }
  }
  flush();
  return out;
}

export function firstItemIndex(entry: DisplayEntry): number {
  return entry.kind === "group" ? entry.indices[0] : entry.index;
}

export function lastItemIndex(entry: DisplayEntry): number {
  return entry.kind === "group" ? entry.indices[entry.indices.length - 1] : entry.index;
}

// displayIndexOf maps an original item index to the display entry containing
// it (entries are in feed order, so the first entry reaching i is the one).
export function displayIndexOf(display: DisplayEntry[], itemIndex: number): number {
  const di = display.findIndex((e) => lastItemIndex(e) >= itemIndex);
  return di === -1 ? Math.max(0, display.length - 1) : di;
}
