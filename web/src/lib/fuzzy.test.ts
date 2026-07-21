import { describe, expect, it } from "vitest";
import { filterFuzzy, highlightSegments, matchFuzzy } from "./fuzzy";

describe("highlightSegments", () => {
  it("splits into hit and miss runs", () => {
    expect(highlightSegments({ text: "skill:dayflow", score: 0, positions: [0, 1, 6, 7, 8] })).toEqual([
      { text: "sk", hit: true },
      { text: "ill:", hit: false },
      { text: "day", hit: true },
      { text: "flow", hit: false },
    ]);
  });

  it("returns one unhit run for an empty query match", () => {
    expect(highlightSegments({ text: "compact", score: 0, positions: [] })).toEqual([
      { text: "compact", hit: false },
    ]);
  });
});

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
