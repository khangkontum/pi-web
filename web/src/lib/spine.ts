// The spine is the session minimap: a vertical trace of the whole session,
// derived from the message model (never the DOM) so it works under
// virtualization. Each feed item becomes a segment with a kind and a weight
// proportional to its rendered bulk; the component scales weights to pixels.

import type { FeedItem, SessionView } from "./feed";
import { contentText } from "./protocol";

export type SegmentKind = "user" | "assistant" | "thinking" | "tool" | "bash" | "compaction" | "error";

export interface SpineSegment {
  index: number; // feed item index, for scroll targeting
  kind: SegmentKind;
  weight: number;
  error: boolean;
}

function textWeight(len: number): number {
  // sub-linear so a huge dump doesn't drown the rest of the trace
  return 1 + Math.log2(1 + len / 320);
}

function itemSegments(item: FeedItem, index: number, view: SessionView): SpineSegment[] {
  switch (item.kind) {
    case "user":
      return [{ index, kind: "user", weight: textWeight(item.text.length), error: false }];
    case "assistant": {
      const segs: SpineSegment[] = [];
      for (const b of item.blocks) {
        if (b.type === "text" && b.text) {
          segs.push({ index, kind: "assistant", weight: textWeight(b.text.length), error: false });
        } else if (b.type === "thinking") {
          segs.push({
            index,
            kind: "thinking",
            weight: textWeight((b.thinking ?? "").length) * 0.6,
            error: false,
          });
        } else if (b.type === "toolCall") {
          const tool = view.tools[b.id];
          segs.push({
            index,
            kind: "tool",
            weight: textWeight(tool?.output.length ?? 0) * 0.8,
            error: tool?.status === "error",
          });
        }
      }
      if (item.error) segs.push({ index, kind: "error", weight: 1, error: true });
      if (segs.length === 0) segs.push({ index, kind: "assistant", weight: 0.5, error: false });
      return segs;
    }
    case "tool": {
      const tool = view.tools[item.id];
      return [
        {
          index,
          kind: "tool",
          weight: textWeight(tool?.output.length ?? 0) * 0.8,
          error: tool?.status === "error",
        },
      ];
    }
    case "bash":
      return [{ index, kind: "bash", weight: textWeight(item.output.length) * 0.8, error: item.exitCode !== 0 }];
    case "compaction":
      return [
        {
          index,
          kind: "compaction",
          weight: 1.2,
          error: !!item.error,
        },
      ];
    case "notice":
      return [{ index, kind: "error", weight: 1, error: item.level === "error" }];
  }
}

export function deriveSpine(view: SessionView): SpineSegment[] {
  const segs: SpineSegment[] = [];
  view.items.forEach((item, i) => segs.push(...itemSegments(item, i, view)));
  return segs;
}

// totalWeight is exported for the component's pixel scaling.
export function totalWeight(segs: SpineSegment[]): number {
  return segs.reduce((sum, s) => sum + s.weight, 0);
}

// firstUserText gives the rail its fallback session title.
export function firstUserText(items: FeedItem[]): string {
  for (const item of items) {
    if (item.kind === "user" && item.text) return contentText(item.text);
  }
  return "";
}
