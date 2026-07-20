// Line diffs for edit/write tool calls, derived from the tool arguments (the
// result payload only confirms success). Rows are data; the DiffView component
// renders them through Svelte templates, never HTML strings.

export type DiffRow =
  | { kind: "ctx"; text: string }
  | { kind: "del"; text: string }
  | { kind: "add"; text: string }
  | { kind: "gap" };

export interface ToolDiff {
  path: string | null;
  rows: DiffRow[];
  adds: number;
  dels: number;
}

const MAX_DIFF_LINES = 600;
const CONTEXT = 2;

function splitLines(s: string): string[] {
  const lines = s.split("\n");
  if (lines[lines.length - 1] === "") lines.pop();
  return lines;
}

// diffLines is a classic LCS table diff, fine at tool-call sizes; oversized
// inputs fall back to whole-block del/add.
export function diffLines(oldText: string, newText: string): DiffRow[] {
  const a = splitLines(oldText);
  const b = splitLines(newText);
  if (a.length + b.length > MAX_DIFF_LINES) {
    return [
      ...a.map((text): DiffRow => ({ kind: "del", text })),
      ...b.map((text): DiffRow => ({ kind: "add", text })),
    ];
  }
  const n = a.length;
  const m = b.length;
  const lcs: number[][] = Array.from({ length: n + 1 }, () => new Array(m + 1).fill(0));
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      lcs[i][j] = a[i] === b[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
    }
  }
  const rows: DiffRow[] = [];
  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      rows.push({ kind: "ctx", text: a[i] });
      i++;
      j++;
    } else if (lcs[i + 1][j] >= lcs[i][j + 1]) {
      rows.push({ kind: "del", text: a[i] });
      i++;
    } else {
      rows.push({ kind: "add", text: b[j] });
      j++;
    }
  }
  while (i < n) rows.push({ kind: "del", text: a[i++] });
  while (j < m) rows.push({ kind: "add", text: b[j++] });
  return rows;
}

// collapseContext trims long unchanged runs to CONTEXT lines around changes,
// inserting gap rows, like a unified diff.
export function collapseContext(rows: DiffRow[]): DiffRow[] {
  const out: DiffRow[] = [];
  let run: DiffRow[] = [];
  let seenChange = false;
  const flush = (isEnd: boolean) => {
    if (run.length === 0) return;
    const keepHead = seenChange ? CONTEXT : 0;
    const keepTail = isEnd ? 0 : CONTEXT;
    if (run.length <= keepHead + keepTail + 1) {
      out.push(...run);
    } else {
      out.push(...run.slice(0, keepHead));
      out.push({ kind: "gap" });
      if (keepTail > 0) out.push(...run.slice(-keepTail));
    }
    run = [];
  };
  for (const row of rows) {
    if (row.kind === "ctx") {
      run.push(row);
    } else {
      flush(false);
      out.push(row);
      seenChange = true;
    }
  }
  flush(true);
  return out;
}

interface EditArgs {
  path?: unknown;
  file_path?: unknown;
  content?: unknown;
  oldText?: unknown;
  newText?: unknown;
  edits?: unknown;
}

// deriveToolDiff maps a tool call's arguments to a renderable diff:
//  - edit: {path, edits: [{oldText, newText}]} (current) or top-level
//    {oldText, newText} (older sessions)
//  - write/create: {path, content} rendered as all-new lines
// Returns null for tools that aren't file mutations.
export function deriveToolDiff(name: string, args?: Record<string, unknown>): ToolDiff | null {
  if (!args) return null;
  const a = args as EditArgs;
  const path = typeof a.path === "string" ? a.path : typeof a.file_path === "string" ? a.file_path : null;

  let rows: DiffRow[] | null = null;
  if (name === "edit" || name === "multi_edit" || name === "str_replace") {
    const edits: { oldText: string; newText: string }[] = [];
    if (Array.isArray(a.edits)) {
      for (const e of a.edits) {
        const ed = e as { oldText?: unknown; newText?: unknown };
        if (typeof ed?.oldText === "string" && typeof ed?.newText === "string") {
          edits.push({ oldText: ed.oldText, newText: ed.newText });
        }
      }
    }
    if (typeof a.oldText === "string" && typeof a.newText === "string") {
      edits.push({ oldText: a.oldText, newText: a.newText });
    }
    if (edits.length === 0) return null;
    rows = [];
    edits.forEach((e, idx) => {
      if (idx > 0) rows!.push({ kind: "gap" });
      rows!.push(...collapseContext(diffLines(e.oldText, e.newText)));
    });
  } else if (name === "write" || name === "create" || name === "write_file") {
    if (typeof a.content !== "string") return null;
    rows = splitLines(a.content).map((text): DiffRow => ({ kind: "add", text }));
  }

  if (!rows) return null;
  return {
    path,
    rows,
    adds: rows.filter((r) => r.kind === "add").length,
    dels: rows.filter((r) => r.kind === "del").length,
  };
}
