import { describe, expect, it } from "vitest";
import type { FeedItem, ToolRun } from "./feed";
import {
  deriveDisplay,
  displayIndexOf,
  firstItemIndex,
  isWorkItem,
  lastItemIndex,
} from "./workgroups";

let nextId = 0;

function toolRun(status: ToolRun["status"] = "done"): ToolRun {
  const id = `t${nextId++}`;
  return { id, name: "bash", path: null, output: "", status };
}

// workTurn: an assistant item shaped like a tool-burst turn — thinking plus
// one tool call — registered in the tools map.
function workTurn(tools: Record<string, ToolRun>, status: ToolRun["status"] = "done"): FeedItem {
  const t = toolRun(status);
  tools[t.id] = t;
  return {
    kind: "assistant",
    blocks: [
      { type: "thinking", thinking: "hm" },
      { type: "toolCall", id: t.id, name: t.name },
    ],
    streaming: false,
  };
}

// parallelTurn: one assistant message issuing several tool calls at once.
function parallelTurn(tools: Record<string, ToolRun>, calls: number): FeedItem {
  const item = workTurn(tools);
  if (item.kind !== "assistant") throw new Error("unreachable");
  for (let i = 1; i < calls; i++) {
    const t = toolRun();
    tools[t.id] = t;
    item.blocks.push({ type: "toolCall", id: t.id, name: t.name });
  }
  return item;
}

function prose(text: string): FeedItem {
  return { kind: "assistant", blocks: [{ type: "text", text }], streaming: false };
}

function user(text: string): FeedItem {
  return { kind: "user", text, imageCount: 0 };
}

describe("isWorkItem", () => {
  it("accepts thinking + tool calls, rejects prose", () => {
    const tools: Record<string, ToolRun> = {};
    expect(isWorkItem(workTurn(tools), tools)).toBe(true);
    expect(isWorkItem(prose("done."), tools)).toBe(false);
    expect(isWorkItem(user("hi"), tools)).toBe(false);
  });

  it("treats empty and whitespace-only text as work (streaming turn start)", () => {
    const tools: Record<string, ToolRun> = {};
    const empty: FeedItem = { kind: "assistant", blocks: [], streaming: true };
    const blank: FeedItem = {
      kind: "assistant",
      blocks: [{ type: "text", text: "  \n" }],
      streaming: true,
    };
    expect(isWorkItem(empty, tools)).toBe(true);
    expect(isWorkItem(blank, tools)).toBe(true);
  });

  it("rejects items with an item-level error or a failed tool", () => {
    const tools: Record<string, ToolRun> = {};
    const failed = workTurn(tools, "error");
    const errored: FeedItem = { kind: "assistant", blocks: [], streaming: false, error: "boom" };
    expect(isWorkItem(failed, tools)).toBe(false);
    expect(isWorkItem(errored, tools)).toBe(false);
  });
});

describe("deriveDisplay", () => {
  it("folds a run of three or more tool turns into one group", () => {
    const tools: Record<string, ToolRun> = {};
    const items = [user("go"), workTurn(tools), workTurn(tools), workTurn(tools), prose("done.")];
    const display = deriveDisplay(items, tools);
    expect(display.map((e) => e.kind)).toEqual(["item", "group", "item"]);
    const group = display[1];
    if (group.kind !== "group") throw new Error("expected group");
    expect(group.indices).toEqual([1, 2, 3]);
    expect(group.hiddenSteps).toBe(1); // one member folded, one tool call in it
  });

  it("leaves short runs inline", () => {
    const tools: Record<string, ToolRun> = {};
    const items = [user("go"), workTurn(tools), workTurn(tools), prose("done.")];
    expect(deriveDisplay(items, tools).every((e) => e.kind === "item")).toBe(true);
  });

  it("counts parallel tool calls in one message toward the threshold", () => {
    const tools: Record<string, ToolRun> = {};
    const thinkingOnly: FeedItem = {
      kind: "assistant",
      blocks: [{ type: "thinking", thinking: "…" }],
      streaming: false,
    };
    // Two toolful members would not have grouped under a member count, but
    // the first carries two calls, so the run holds three steps.
    const items = [parallelTurn(tools, 2), workTurn(tools), thinkingOnly];
    const display = deriveDisplay(items, tools);
    expect(display.map((e) => e.kind)).toEqual(["group"]);
    const group = display[0];
    if (group.kind !== "group") throw new Error("expected group");
    expect(group.hiddenSteps).toBe(2); // the parallel pair is folded
  });

  it("leaves a run no longer than the visible tail inline, whatever it called", () => {
    const tools: Record<string, ToolRun> = {};
    const items = [parallelTurn(tools, 3), parallelTurn(tools, 2)];
    expect(deriveDisplay(items, tools).every((e) => e.kind === "item")).toBe(true);
  });

  it("does not count thinking-only members toward the threshold", () => {
    const tools: Record<string, ToolRun> = {};
    const thinkingOnly: FeedItem = {
      kind: "assistant",
      blocks: [{ type: "thinking", thinking: "…" }],
      streaming: true,
    };
    const items = [workTurn(tools), workTurn(tools), thinkingOnly];
    expect(deriveDisplay(items, tools).every((e) => e.kind === "item")).toBe(true);
  });

  it("includes a trailing streaming thinking-only member in the group", () => {
    const tools: Record<string, ToolRun> = {};
    const thinkingOnly: FeedItem = {
      kind: "assistant",
      blocks: [{ type: "thinking", thinking: "…" }],
      streaming: true,
    };
    const items = [workTurn(tools), workTurn(tools), workTurn(tools), thinkingOnly];
    const display = deriveDisplay(items, tools);
    expect(display).toHaveLength(1);
    expect(firstItemIndex(display[0])).toBe(0);
    expect(lastItemIndex(display[0])).toBe(3);
  });

  it("keeps the group when the trailing member turns into prose", () => {
    const tools: Record<string, ToolRun> = {};
    const items = [workTurn(tools), workTurn(tools), workTurn(tools), prose("answer")];
    const display = deriveDisplay(items, tools);
    expect(display.map((e) => e.kind)).toEqual(["group", "item"]);
  });

  it("splits the run at a failed tool so errors stay visible", () => {
    const tools: Record<string, ToolRun> = {};
    const items = [
      workTurn(tools),
      workTurn(tools),
      workTurn(tools),
      workTurn(tools, "error"),
      workTurn(tools),
    ];
    const display = deriveDisplay(items, tools);
    expect(display.map((e) => e.kind)).toEqual(["group", "item", "item"]);
    expect((display[1] as { index: number }).index).toBe(3);
  });

  it("counts hidden tool calls across all folded members", () => {
    const tools: Record<string, ToolRun> = {};
    const items = [
      workTurn(tools),
      workTurn(tools),
      workTurn(tools),
      workTurn(tools),
      workTurn(tools),
    ];
    const display = deriveDisplay(items, tools);
    const group = display[0];
    if (group.kind !== "group") throw new Error("expected group");
    expect(group.indices).toHaveLength(5);
    expect(group.hiddenSteps).toBe(3); // five members, last two visible
  });
});

describe("displayIndexOf", () => {
  it("maps item indices through groups", () => {
    const tools: Record<string, ToolRun> = {};
    const items = [user("go"), workTurn(tools), workTurn(tools), workTurn(tools), prose("done.")];
    const display = deriveDisplay(items, tools);
    expect(displayIndexOf(display, 0)).toBe(0);
    expect(displayIndexOf(display, 1)).toBe(1);
    expect(displayIndexOf(display, 3)).toBe(1);
    expect(displayIndexOf(display, 4)).toBe(2);
    expect(displayIndexOf(display, 99)).toBe(2); // clamp past the end
  });
});
