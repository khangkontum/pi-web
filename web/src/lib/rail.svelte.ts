// The rail store: pi's session listing grouped by working directory, newest
// group first, newest session first within a group. Stateless by design —
// this is always a fresh read of pi's session store, never a cache with its
// own lifecycle.
//
// Group visibility is a per-browser reading preference (localStorage, never
// the server): the operator's explicit collapse/expand choices override the
// defaults, and the defaults keep the rail quiet on big histories — stale
// groups fold themselves, live and recent ones stay open.

import { api, type SessionSummary } from "./api";
import { matchFuzzy } from "./fuzzy";

export interface ProjectGroup {
  cwd: string;
  // last path segment, disambiguated with the parent when two projects share it
  label: string;
  sessions: SessionSummary[];
}

function shortLabel(cwd: string): string {
  const parts = cwd.split("/").filter(Boolean);
  return parts[parts.length - 1] ?? cwd ?? "?";
}

export function groupSessions(sessions: SessionSummary[]): ProjectGroup[] {
  const byCwd = new Map<string, SessionSummary[]>();
  for (const s of sessions) {
    const key = s.cwd || "?";
    const list = byCwd.get(key);
    if (list) list.push(s);
    else byCwd.set(key, [s]);
  }
  const groups: ProjectGroup[] = [];
  for (const [cwd, list] of byCwd) {
    list.sort((a, b) => (a.updatedAt < b.updatedAt ? 1 : -1));
    groups.push({ cwd, label: shortLabel(cwd), sessions: list });
  }
  groups.sort((a, b) => (a.sessions[0].updatedAt < b.sessions[0].updatedAt ? 1 : -1));
  const seen = new Map<string, number>();
  for (const g of groups) seen.set(g.label, (seen.get(g.label) ?? 0) + 1);
  for (const g of groups) {
    if ((seen.get(g.label) ?? 0) > 1) {
      const parts = g.cwd.split("/").filter(Boolean);
      if (parts.length >= 2) g.label = `${parts[parts.length - 2]}/${g.label}`;
    }
  }
  return groups;
}

// --- group visibility -------------------------------------------------------

export type GroupPref = "collapsed" | "expanded";

// a group with no live child and nothing newer than this folds by default
export const STALE_AFTER_MS = 7 * 24 * 60 * 60 * 1000;
// sessions shown per expanded group before the "…more" expander
export const GROUP_CAP = 6;

export function isGroupStale(group: ProjectGroup, now: number): boolean {
  if (group.sessions.some((s) => s.live)) return false;
  const newest = new Date(group.sessions[0]?.updatedAt ?? "").getTime();
  // an unparsable timestamp counts as stale
  return !(now - newest < STALE_AFTER_MS);
}

// isGroupCollapsed resolves what the rail shows: an explicit preference wins;
// otherwise the active session's group is open and stale groups are folded.
export function isGroupCollapsed(
  group: ProjectGroup,
  prefs: Record<string, GroupPref>,
  activeCwd: string | null,
  now: number,
): boolean {
  const pref = prefs[group.cwd];
  if (pref) return pref === "collapsed";
  if (activeCwd !== null && group.cwd === activeCwd) return false;
  return isGroupStale(group, now);
}

// filterGroups narrows the rail to fuzzy matches on session titles and group
// labels. A matching label keeps the whole group; otherwise only matching
// sessions survive. Empty query returns the input untouched.
export function filterGroups(groups: ProjectGroup[], query: string): ProjectGroup[] {
  const q = query.trim();
  if (!q) return groups;
  const out: ProjectGroup[] = [];
  for (const g of groups) {
    if (matchFuzzy(q, g.label)) {
      out.push(g);
      continue;
    }
    const sessions = g.sessions.filter((s) => matchFuzzy(q, s.title || s.id));
    if (sessions.length > 0) out.push({ ...g, sessions });
  }
  return out;
}

const PREFS_KEY = "pi-web:rail-groups";
// pre-tri-state format: a plain array of collapsed cwds
const LEGACY_KEY = "pi-web:rail-collapsed";

function readPrefs(): Record<string, GroupPref> {
  try {
    const raw = localStorage.getItem(PREFS_KEY);
    if (raw) {
      const obj = JSON.parse(raw) as unknown;
      if (obj && typeof obj === "object" && !Array.isArray(obj)) {
        const out: Record<string, GroupPref> = {};
        for (const [k, v] of Object.entries(obj)) {
          if (v === "collapsed" || v === "expanded") out[k] = v;
        }
        return out;
      }
    }
    const legacy = localStorage.getItem(LEGACY_KEY);
    if (legacy) {
      const list = JSON.parse(legacy) as unknown;
      localStorage.removeItem(LEGACY_KEY);
      if (Array.isArray(list)) {
        const out: Record<string, GroupPref> = {};
        for (const c of list) if (typeof c === "string") out[c] = "collapsed";
        localStorage.setItem(PREFS_KEY, JSON.stringify(out));
        return out;
      }
    }
  } catch {
    /* fall through */
  }
  return {};
}

class Rail {
  groups = $state<ProjectGroup[]>([]);
  error = $state<string | null>(null);
  prefs = $state<Record<string, GroupPref>>(readPrefs());
  filter = $state("");
  // transient per-page "…more" expansions; not worth persisting
  showAll = $state<Record<string, boolean>>({});
  #timer: number | undefined;

  // setGroup records an explicit choice, flipping the currently shown state.
  setGroup(cwd: string, pref: GroupPref): void {
    this.prefs[cwd] = pref;
    try {
      localStorage.setItem(PREFS_KEY, JSON.stringify(this.prefs));
    } catch {
      /* best effort */
    }
  }

  async refresh(): Promise<void> {
    try {
      const { sessions } = await api.listSessions();
      this.groups = groupSessions(sessions ?? []);
      this.error = null;
    } catch (err) {
      this.error = err instanceof Error ? err.message : String(err);
    }
  }

  // start polls slowly (live flags change server-side) and refreshes on focus.
  start(): void {
    this.refresh();
    this.#timer = window.setInterval(() => this.refresh(), 30000);
    window.addEventListener("focus", () => this.refresh());
  }

  stop(): void {
    if (this.#timer !== undefined) clearInterval(this.#timer);
  }
}

export const rail = new Rail();
