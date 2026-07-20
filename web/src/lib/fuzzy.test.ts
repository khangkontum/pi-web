import { describe, expect, it } from "vitest";
import { filterFuzzy, matchFuzzy } from "./fuzzy";

describe("matchFuzzy", () => {
  it("matches an exact substring", () => {
    const m = matchFuzzy("feed", "src/lib/feed.ts");
    expect(m).not.toBeNull();
    expect(m!.positions).toEqual([8, 9, 10, 11]);
  });

  it("matches a scattered subsequence", () => {
    expect(matchFuzzy("slf", "src/lib/feed.ts")).not.toBeNull();
  });

  it("returns null when not a subsequence", () => {
    expect(matchFuzzy("zzz", "src/lib/feed.ts")).toBeNull();
  });

  it("is case-insensitive", () => {
    expect(matchFuzzy("FEED", "src/lib/feed.ts")).not.toBeNull();
  });

  it("empty query matches everything with zero score", () => {
    expect(matchFuzzy("", "anything")).toEqual({ text: "anything", score: 0, positions: [] });
  });
});

describe("filterFuzzy", () => {
  const files = [
    "internal/piweb/server.go",
    "web/src/lib/feed.ts",
    "web/src/lib/feed.test.ts",
    "web/src/components/Feed.svelte",
    "docs/reference.md",
  ];

  it("ranks basename matches above deep-prefix matches", () => {
    const results = filterFuzzy("feed", files).map((m) => m.text);
    expect(results[0]).toBe("web/src/lib/feed.ts");
    expect(results).toContain("web/src/components/Feed.svelte");
  });

  it("respects the limit", () => {
    expect(filterFuzzy("e", files, 2)).toHaveLength(2);
  });

  it("prefers boundary-aligned matches", () => {
    const results = filterFuzzy("ref", files).map((m) => m.text);
    expect(results[0]).toBe("docs/reference.md");
  });
});
