import { describe, expect, it } from "vitest";
import { assignLanes, laneCount, type GraphCommit } from "./gitgraph";

describe("assignLanes", () => {
  it("keeps a linear history in lane 0", () => {
    const commits: GraphCommit[] = [
      { hash: "c", parents: ["b"] },
      { hash: "b", parents: ["a"] },
      { hash: "a", parents: [] },
    ];
    const rows = assignLanes(commits);
    expect(rows.map((r) => r.lane)).toEqual([0, 0, 0]);
    expect(laneCount(rows)).toBe(1);
    // root has no parents leaving it
    expect(rows[2].parentLanes).toEqual([]);
  });

  it("branches a merge commit into two lanes and rejoins", () => {
    // m merges b (lane 0) and c (lane 1); both children of root r.
    const commits: GraphCommit[] = [
      { hash: "m", parents: ["b", "c"] },
      { hash: "b", parents: ["r"] },
      { hash: "c", parents: ["r"] },
      { hash: "r", parents: [] },
    ];
    const rows = assignLanes(commits);
    const [m, b, c, r] = rows;
    expect(m.lane).toBe(0);
    expect(m.parentLanes).toEqual([0, 1]); // edges to both parents' lanes
    expect(b.lane).toBe(0);
    expect(c.lane).toBe(1);
    // both lanes now expect r; r collapses them into lane 0
    expect(r.lane).toBe(0);
    expect(r.mergesFrom).toEqual([0, 1]);
    expect(r.lanesAfter).toEqual([]);
    expect(laneCount(rows)).toBe(2);
  });

  it("handles an unexpected tip (second branch head) in a free lane", () => {
    const commits: GraphCommit[] = [
      { hash: "x", parents: ["a"] },
      { hash: "y", parents: ["a"] }, // second head, nothing expected it
      { hash: "a", parents: [] },
    ];
    const rows = assignLanes(commits);
    expect(rows[0].lane).toBe(0);
    expect(rows[1].lane).toBe(1);
    expect(rows[2].mergesFrom).toEqual([0, 1]);
  });

  it("keeps two independent roots apart", () => {
    const commits: GraphCommit[] = [
      { hash: "b", parents: ["a"] },
      { hash: "d", parents: ["c"] },
      { hash: "a", parents: [] },
      { hash: "c", parents: [] },
    ];
    const rows = assignLanes(commits);
    expect(rows[0].lane).toBe(0);
    expect(rows[1].lane).toBe(1);
    expect(rows[2].lane).toBe(0); // a collapses lane 0
    expect(rows[3].lane).toBe(1); // c was still expected in lane 1
  });

  it("frees a lane once its branch root passes", () => {
    const commits: GraphCommit[] = [
      { hash: "m", parents: ["a", "b"] },
      { hash: "a", parents: [] }, // lane 0 root ends
      { hash: "b", parents: [] },
    ];
    const rows = assignLanes(commits);
    expect(rows[1].lanesAfter).toEqual([null, "b"]); // lane 0 freed, b still lane 1
    expect(rows[2].lane).toBe(1);
    expect(rows[2].lanesAfter).toEqual([]);
  });
});
