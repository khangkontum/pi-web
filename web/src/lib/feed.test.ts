import { describe, expect, it } from "vitest";
import { applyEvent, applySnapshot, emptyView, type AssistantItem, type CompactionItem } from "./feed";
import type { Snapshot } from "./protocol";

const snapshot: Snapshot = {
  id: "s1",
  cwd: "/work",
  state: {
    model: { provider: "anthropic", id: "m1", contextWindow: 1000 },
    thinkingLevel: "medium",
    isStreaming: false,
    steeringMode: "one-at-a-time",
    autoCompactionEnabled: false,
    sessionName: "demo",
  },
  messages: {
    messages: [
      { role: "user", content: "hello", timestamp: 1 },
      {
        role: "assistant",
        timestamp: 2,
        content: [
          { type: "text", text: "hi" },
          { type: "toolCall", id: "t1", name: "bash", arguments: { command: "ls" } },
        ],
      },
      { role: "toolResult", toolCallId: "t1", content: [{ type: "text", text: "a\nb" }], isError: false },
      { role: "bashExecution", command: "pwd", output: "/work", exitCode: 0 },
    ],
  },
  stats: { tokens: { input: 10, output: 5 }, contextUsage: { percent: 3, contextWindow: 1000 } },
};

describe("applySnapshot", () => {
  it("builds the full view", () => {
    const v = applySnapshot(snapshot);
    expect(v.id).toBe("s1");
    expect(v.cwd).toBe("/work");
    expect(v.sessionName).toBe("demo");
    expect(v.steeringMode).toBe("one-at-a-time");
    expect(v.autoCompaction).toBe(false);
    expect(v.items.map((i) => i.kind)).toEqual(["user", "assistant", "bash"]);
    expect(v.tools["t1"].status).toBe("done");
    expect(v.tools["t1"].output).toBe("a\nb");
    expect(v.stats?.contextUsage?.percent).toBe(3);
  });
});

describe("streaming lifecycle", () => {
  it("streams an assistant message into place and settles", () => {
    const v = applySnapshot(snapshot);
    applyEvent(v, { type: "agent_start" });
    expect(v.streaming).toBe(true);
    applyEvent(v, { type: "message_start", message: { role: "assistant", content: [] } });
    applyEvent(v, {
      type: "message_update",
      message: { role: "assistant", content: [{ type: "text", text: "wor" }] },
    });
    const item = v.items[v.items.length - 1] as AssistantItem;
    expect(item.streaming).toBe(true);
    expect(item.blocks).toEqual([{ type: "text", text: "wor" }]);
    applyEvent(v, {
      type: "message_end",
      message: {
        role: "assistant",
        content: [{ type: "text", text: "working" }],
        usage: { input: 100, output: 20 },
      },
    });
    expect(item.streaming).toBe(false);
    expect(v.streamingIndex).toBe(-1);
    applyEvent(v, { type: "agent_settled" });
    expect(v.streaming).toBe(false);
  });

  it("settles on agent_end but not on turn_end", () => {
    const v = emptyView();
    applyEvent(v, { type: "agent_start" });
    applyEvent(v, { type: "turn_end" });
    expect(v.streaming).toBe(true);
    applyEvent(v, { type: "agent_end" });
    expect(v.streaming).toBe(false);
  });

  it("does not duplicate user messages seen via start and end", () => {
    const v = emptyView();
    applyEvent(v, { type: "message_start", message: { role: "user", content: "steer", timestamp: 9 } });
    applyEvent(v, { type: "message_end", message: { role: "user", content: "steer", timestamp: 9 } });
    expect(v.items.filter((i) => i.kind === "user")).toHaveLength(1);
  });

  it("accumulates usage into stats and context estimate", () => {
    const v = applySnapshot(snapshot);
    applyEvent(v, { type: "message_start", message: { role: "assistant", content: [] } });
    applyEvent(v, {
      type: "message_end",
      message: {
        role: "assistant",
        content: [{ type: "text", text: "x" }],
        usage: { input: 90, output: 10, cacheRead: 100, cost: { total: 0.5 } } as never,
      },
    });
    expect(v.stats?.tokens?.input).toBe(100);
    expect(v.stats?.tokens?.output).toBe(15);
    expect(v.stats?.cost).toBe(0.5);
    // 90 + 10 + 100 = 200 of 1000-token window
    expect(v.stats?.contextUsage?.tokens).toBe(200);
    expect(v.stats?.contextUsage?.percent).toBe(20);
  });
});

describe("tool execution", () => {
  it("creates an orphan tool item and updates it in place", () => {
    const v = emptyView();
    applyEvent(v, { type: "tool_execution_start", toolCallId: "t9", toolName: "grep", args: { pattern: "x" } });
    expect(v.items).toEqual([{ kind: "tool", id: "t9" }]);
    applyEvent(v, {
      type: "tool_execution_update",
      toolCallId: "t9",
      partialResult: { content: [{ type: "text", text: "partial" }] },
    });
    expect(v.tools["t9"].output).toBe("partial");
    expect(v.tools["t9"].status).toBe("running");
    applyEvent(v, {
      type: "tool_execution_end",
      toolCallId: "t9",
      result: { content: [{ type: "text", text: "full" }] },
      isError: true,
    });
    expect(v.tools["t9"].output).toBe("full");
    expect(v.tools["t9"].status).toBe("error");
  });
});

describe("telemetry events", () => {
  it("tracks the queue", () => {
    const v = emptyView();
    applyEvent(v, { type: "queue_update", steering: ["a"], followUp: ["b", "c"] });
    expect(v.queue).toEqual({ steering: ["a"], followUp: ["b", "c"] });
  });

  it("records compaction start and end with summary", () => {
    const v = emptyView();
    v.stats = { contextUsage: { tokens: 900, percent: 90, contextWindow: 1000 } };
    applyEvent(v, { type: "compaction_start", reason: "threshold" });
    expect(v.compacting).toBe(true);
    applyEvent(v, {
      type: "compaction_end",
      reason: "threshold",
      result: { summary: "sum", tokensBefore: 900, estimatedTokensAfter: 100 },
    });
    expect(v.compacting).toBe(false);
    const item = v.items[0] as CompactionItem;
    expect(item.running).toBe(false);
    expect(item.summary).toBe("sum");
    expect(item.tokensAfter).toBe(100);
    // context usage unknown until the next assistant response
    expect(v.stats?.contextUsage?.percent).toBeNull();
  });

  it("tracks auto-retry and surfaces final failure", () => {
    const v = emptyView();
    applyEvent(v, { type: "auto_retry_start", attempt: 1, maxAttempts: 3, delayMs: 2000, errorMessage: "529" });
    expect(v.retry?.attempt).toBe(1);
    applyEvent(v, { type: "auto_retry_end", success: false, attempt: 3, finalError: "overloaded" });
    expect(v.retry).toBeNull();
    expect(v.items[0]).toMatchObject({ kind: "notice", level: "error" });
  });

  it("surfaces extension errors as notices", () => {
    const v = emptyView();
    applyEvent(v, { type: "extension_error", event: "tool_call", extensionPath: "/e.ts", error: "boom" });
    expect(v.items[0]).toMatchObject({ kind: "notice", level: "error", title: "extension error (tool_call)" });
  });

  it("applies passive extension UI state", () => {
    const v = emptyView();
    applyEvent(v, { type: "extension_ui_request", method: "setStatus", statusKey: "k", statusText: "busy" });
    expect(v.statusLines).toEqual({ k: "busy" });
    applyEvent(v, { type: "extension_ui_request", method: "setStatus", statusKey: "k" });
    expect(v.statusLines).toEqual({});
    applyEvent(v, {
      type: "extension_ui_request",
      method: "setWidget",
      widgetKey: "w",
      widgetLines: ["l1"],
      widgetPlacement: "belowEditor",
    });
    expect(v.widgetsBelow).toEqual({ w: ["l1"] });
    applyEvent(v, { type: "extension_ui_request", method: "setTitle", title: "T" });
    expect(v.title).toBe("T");
  });
});

describe("synthetic piweb events", () => {
  it("appends operator bash results", () => {
    const v = emptyView();
    applyEvent(v, { type: "piweb_bash", command: "ls", result: { output: "a", exitCode: 1 } });
    expect(v.items[0]).toEqual({ kind: "bash", command: "ls", output: "a", exitCode: 1 });
  });

  it("syncs model and thinking level across browsers", () => {
    const v = emptyView();
    applyEvent(v, { type: "piweb_model", model: { provider: "p", id: "m" } });
    applyEvent(v, { type: "piweb_thinking", level: "high" });
    expect(v.model).toEqual({ provider: "p", id: "m" });
    expect(v.thinkingLevel).toBe("high");
  });

  it("ignores unknown event types", () => {
    const v = emptyView();
    applyEvent(v, { type: "totally_new_event_from_future_pi" });
    expect(v.items).toEqual([]);
  });
});
