import { describe, expect, it } from "vitest";
import {
  STALE_AFTER_MS,
  filterGroups,
  groupSessions,
  isGroupCollapsed,
  isGroupStale,
  type ProjectGroup,
} from "./rail.svelte";
import type { SessionSummary } from "./api";

const NOW = new Date("2026-07-20T12:00:00Z").getTime();

function summary(over: Partial<SessionSummary>): SessionSummary {
  return {
    id: "id",
    path: "/p",
    cwd: "/work/a",
    title: "t",
    updatedAt: new Date(NOW).toISOString(),
    live: false,
    ...over,
  };
}

function group(over: Partial<ProjectGroup>): ProjectGroup {
  return { cwd: "/work/a", label: "a", sessions: [summary({})], ...over };
}

describe("groupSessions", () => {
  it("groups by cwd, newest session and newest group first", () => {
    const old = new Date(NOW - 5000).toISOString();
    const mid = new Date(NOW - 1000).toISOString();
    const fresh = new Date(NOW).toISOString();
    const groups = groupSessions([
      summary({ id: "1", cwd: "/w/a", updatedAt: old }),
      summary({ id: "2", cwd: "/w/b", updatedAt: mid }),
      summary({ id: "3", cwd: "/w/a", updatedAt: fresh }),
    ]);
    expect(groups.map((g) => g.cwd)).toEqual(["/w/a", "/w/b"]);
    expect(groups[0].sessions.map((s) => s.id)).toEqual(["3", "1"]);
  });

  it("disambiguates duplicate labels with the parent segment", () => {
    const groups = groupSessions([
      summary({ id: "1", cwd: "/one/app" }),
      summary({ id: "2", cwd: "/two/app" }),
    ]);
    expect(groups.map((g) => g.label).sort()).toEqual(["one/app", "two/app"]);
  });
});

describe("isGroupStale", () => {
  it("fresh activity keeps a group fresh", () => {
    expect(isGroupStale(group({}), NOW)).toBe(false);
  });

  it("old groups are stale", () => {
    const g = group({
      sessions: [summary({ updatedAt: new Date(NOW - STALE_AFTER_MS - 1000).toISOString() })],
    });
    expect(isGroupStale(g, NOW)).toBe(true);
  });

  it("a live session overrides age", () => {
    const g = group({
      sessions: [summary({ live: true, updatedAt: new Date(NOW - STALE_AFTER_MS * 4).toISOString() })],
    });
    expect(isGroupStale(g, NOW)).toBe(false);
  });

  it("unparsable timestamps count as stale", () => {
    expect(isGroupStale(group({ sessions: [summary({ updatedAt: "garbage" })] }), NOW)).toBe(true);
  });
});

describe("isGroupCollapsed", () => {
  const stale = group({
    cwd: "/w/stale",
    sessions: [summary({ cwd: "/w/stale", updatedAt: new Date(NOW - STALE_AFTER_MS * 2).toISOString() })],
  });
  const fresh = group({ cwd: "/w/fresh" });

  it("defaults: stale folds, fresh stays open", () => {
    expect(isGroupCollapsed(stale, {}, null, NOW)).toBe(true);
    expect(isGroupCollapsed(fresh, {}, null, NOW)).toBe(false);
  });

  it("explicit preferences win over defaults", () => {
    expect(isGroupCollapsed(stale, { "/w/stale": "expanded" }, null, NOW)).toBe(false);
    expect(isGroupCollapsed(fresh, { "/w/fresh": "collapsed" }, null, NOW)).toBe(true);
  });

  it("the active session's group defaults open even when stale", () => {
    expect(isGroupCollapsed(stale, {}, "/w/stale", NOW)).toBe(false);
  });

  it("an explicit collapse beats even the active group", () => {
    expect(isGroupCollapsed(stale, { "/w/stale": "collapsed" }, "/w/stale", NOW)).toBe(true);
  });
});

describe("filterGroups", () => {
  const groups: ProjectGroup[] = [
    group({
      cwd: "/w/piweb",
      label: "piweb",
      sessions: [
        summary({ id: "a", title: "rebuild the frontend" }),
        summary({ id: "b", title: "fix the updater" }),
      ],
    }),
    group({
      cwd: "/w/other",
      label: "other",
      sessions: [summary({ id: "c", title: "commit and push" })],
    }),
  ];

  it("empty query returns everything untouched", () => {
    expect(filterGroups(groups, "  ")).toBe(groups);
  });

  it("matches session titles and drops non-matching sessions", () => {
    const out = filterGroups(groups, "rebuild");
    expect(out).toHaveLength(1);
    expect(out[0].sessions.map((s) => s.id)).toEqual(["a"]);
  });

  it("a matching group label keeps the whole group", () => {
    const out = filterGroups(groups, "piweb");
    expect(out).toHaveLength(1);
    expect(out[0].sessions).toHaveLength(2);
  });

  it("no matches yields an empty list", () => {
    expect(filterGroups(groups, "zzzzzz")).toEqual([]);
  });
});
