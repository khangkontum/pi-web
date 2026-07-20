import { describe, expect, it } from "vitest";
import { applySnapshot, emptyView } from "./feed";
import { deriveSpine, firstUserText, totalWeight } from "./spine";

describe("deriveSpine", () => {
  it("maps feed items to weighted segments", () => {
    const v = applySnapshot({
      id: "s",
      messages: {
        messages: [
          { role: "user", content: "hello" },
          {
            role: "assistant",
            content: [
              { type: "thinking", thinking: "hmm" },
              { type: "text", text: "answer" },
              { type: "toolCall", id: "t1", name: "bash", arguments: {} },
            ],
          },
          { role: "toolResult", toolCallId: "t1", content: [{ type: "text", text: "out" }], isError: true },
          { role: "bashExecution", command: "x", output: "y", exitCode: 0 },
        ],
      },
    });
    const segs = deriveSpine(v);
    expect(segs.map((s) => s.kind)).toEqual(["user", "thinking", "assistant", "tool", "bash"]);
    // the failing tool is flagged
    expect(segs.find((s) => s.kind === "tool")?.error).toBe(true);
    // segments carry their item index for scroll targeting
    expect(segs[0].index).toBe(0);
    expect(segs[1].index).toBe(1);
    expect(totalWeight(segs)).toBeGreaterThan(0);
  });

  it("weights grow sub-linearly with content size", () => {
    const small = applySnapshot({
      id: "a",
      messages: { messages: [{ role: "assistant", content: [{ type: "text", text: "x" }] }] },
    });
    const big = applySnapshot({
      id: "b",
      messages: {
        messages: [{ role: "assistant", content: [{ type: "text", text: "x".repeat(100000) }] }],
      },
    });
    const ws = totalWeight(deriveSpine(small));
    const wb = totalWeight(deriveSpine(big));
    expect(wb).toBeGreaterThan(ws);
    expect(wb).toBeLessThan(ws * 20);
  });

  it("handles an empty view", () => {
    expect(deriveSpine(emptyView())).toEqual([]);
  });
});

describe("firstUserText", () => {
  it("returns the first user message text", () => {
    const v = applySnapshot({
      id: "s",
      messages: { messages: [{ role: "assistant", content: "a" }, { role: "user", content: "title me" }] },
    });
    expect(firstUserText(v.items)).toBe("title me");
  });
});
