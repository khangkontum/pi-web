import { describe, expect, it } from "vitest";
import { collapseContext, deriveToolDiff, diffLines } from "./diff";

describe("diffLines", () => {
  it("marks unchanged lines as context", () => {
    expect(diffLines("a\nb", "a\nb")).toEqual([
      { kind: "ctx", text: "a" },
      { kind: "ctx", text: "b" },
    ]);
  });

  it("diffs a changed line as del+add", () => {
    expect(diffLines("a\nold\nc", "a\nnew\nc")).toEqual([
      { kind: "ctx", text: "a" },
      { kind: "del", text: "old" },
      { kind: "add", text: "new" },
      { kind: "ctx", text: "c" },
    ]);
  });

  it("handles pure insertion and deletion", () => {
    expect(diffLines("a", "a\nb")).toEqual([
      { kind: "ctx", text: "a" },
      { kind: "add", text: "b" },
    ]);
    expect(diffLines("a\nb", "b")).toEqual([
      { kind: "del", text: "a" },
      { kind: "ctx", text: "b" },
    ]);
  });

  it("ignores a single trailing newline", () => {
    expect(diffLines("a\n", "a")).toEqual([{ kind: "ctx", text: "a" }]);
  });
});

describe("collapseContext", () => {
  it("elides long unchanged runs into gaps", () => {
    const rows = diffLines("1\n2\n3\n4\n5\n6\n7\n8\n9\nX", "1\n2\n3\n4\n5\n6\n7\n8\n9\nY");
    const collapsed = collapseContext(rows);
    expect(collapsed[0]).toEqual({ kind: "gap" });
    expect(collapsed.slice(1)).toEqual([
      { kind: "ctx", text: "8" },
      { kind: "ctx", text: "9" },
      { kind: "del", text: "X" },
      { kind: "add", text: "Y" },
    ]);
  });

  it("keeps short runs intact", () => {
    const rows = diffLines("a\nx", "a\ny");
    expect(collapseContext(rows)).toEqual(rows);
  });
});

describe("deriveToolDiff", () => {
  it("derives from current edit schema (edits array)", () => {
    const d = deriveToolDiff("edit", {
      path: "f.ts",
      edits: [{ oldText: "a\nb", newText: "a\nc" }],
    });
    expect(d?.path).toBe("f.ts");
    expect(d?.rows).toEqual([
      { kind: "ctx", text: "a" },
      { kind: "del", text: "b" },
      { kind: "add", text: "c" },
    ]);
    expect(d?.adds).toBe(1);
    expect(d?.dels).toBe(1);
  });

  it("derives from legacy top-level oldText/newText", () => {
    const d = deriveToolDiff("edit", { path: "f", oldText: "x", newText: "y" });
    expect(d?.rows).toEqual([
      { kind: "del", text: "x" },
      { kind: "add", text: "y" },
    ]);
  });

  it("separates multiple edits with a gap", () => {
    const d = deriveToolDiff("edit", {
      path: "f",
      edits: [
        { oldText: "a", newText: "b" },
        { oldText: "c", newText: "d" },
      ],
    });
    expect(d?.rows.filter((r) => r.kind === "gap")).toHaveLength(1);
  });

  it("renders write content as all-new lines", () => {
    const d = deriveToolDiff("write", { path: "n.txt", content: "one\ntwo" });
    expect(d?.rows).toEqual([
      { kind: "add", text: "one" },
      { kind: "add", text: "two" },
    ]);
    expect(d?.dels).toBe(0);
  });

  it("returns null for non-file tools or missing args", () => {
    expect(deriveToolDiff("bash", { command: "ls" })).toBeNull();
    expect(deriveToolDiff("edit", undefined)).toBeNull();
    expect(deriveToolDiff("edit", { path: "f" })).toBeNull();
  });
});
