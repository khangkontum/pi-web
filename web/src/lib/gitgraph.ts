// Lane assignment for the commit graph. Input is newest-first history (as
// `git log` emits it); output gives every commit a column plus the lane state
// entering and leaving its row, which is all the SVG renderer needs.

export interface GraphCommit {
  hash: string;
  parents: string[];
}

export interface GraphRow {
  lane: number;
  // Expected commit hashes per lane entering / leaving this row. A null slot
  // is an empty column kept so lanes right of it don't shift.
  lanesBefore: (string | null)[];
  lanesAfter: (string | null)[];
  // Lanes (before-state) whose expectation was this commit — branches merging
  // into the dot from above. Includes the dot's own lane when continuing.
  mergesFrom: number[];
  // Lane (after-state) of each parent — edges leaving the dot downward.
  parentLanes: number[];
}

function firstFree(lanes: (string | null)[]): number {
  const i = lanes.indexOf(null);
  if (i >= 0) return i;
  lanes.push(null);
  return lanes.length - 1;
}

function trimTrailing(lanes: (string | null)[]): void {
  while (lanes.length > 0 && lanes[lanes.length - 1] === null) lanes.pop();
}

export function assignLanes(commits: GraphCommit[]): GraphRow[] {
  const lanes: (string | null)[] = [];
  const rows: GraphRow[] = [];

  for (const c of commits) {
    const lanesBefore = [...lanes];

    const mergesFrom: number[] = [];
    for (let i = 0; i < lanes.length; i++) {
      if (lanes[i] === c.hash) mergesFrom.push(i);
    }

    let lane: number;
    if (mergesFrom.length > 0) {
      lane = mergesFrom[0];
      for (const i of mergesFrom.slice(1)) lanes[i] = null;
    } else {
      // A tip nothing expected yet (branch head, or a second root).
      lane = firstFree(lanes);
    }

    lanes[lane] = c.parents[0] ?? null;

    const parentLanes: number[] = [];
    if (c.parents.length > 0) parentLanes.push(lane);
    for (const p of c.parents.slice(1)) {
      const existing = lanes.indexOf(p);
      if (existing >= 0) {
        parentLanes.push(existing);
      } else {
        const free = firstFree(lanes);
        lanes[free] = p;
        parentLanes.push(free);
      }
    }
    trimTrailing(lanes);

    rows.push({ lane, lanesBefore, lanesAfter: [...lanes], mergesFrom, parentLanes });
  }
  return rows;
}

// laneCount is the widest lane state across all rows, for sizing the SVG.
export function laneCount(rows: GraphRow[]): number {
  let max = 1;
  for (const r of rows) {
    max = Math.max(max, r.lanesBefore.length, r.lanesAfter.length, r.lane + 1);
  }
  return max;
}
