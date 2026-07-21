<script lang="ts">
  // Git audit overlay: "changes" is the working tree (staged, unstaged,
  // untracked) — usually the interesting view, since agents rarely commit —
  // and "history" is the commit graph with per-commit patches. Read-only by
  // design: pi-web audits the repo, it never drives it.
  import DiffView from "./DiffView.svelte";
  import Overlay from "./Overlay.svelte";
  import { api, type GitCommit } from "../lib/api";
  import { assignLanes, laneCount, type GraphRow } from "../lib/gitgraph";
  import { parsePatch, type PatchFile } from "../lib/patch";

  let { base, onClose }: { base: string | null; onClose: () => void } = $props();

  // How many commit rows render before the "show more" control.
  const HISTORY_PAGE = 80;

  let tab = $state<"changes" | "history">("changes");

  let changes = $state<PatchFile[] | null>(null);
  let changesTruncated = $state(false);
  let changesError = $state<string | null>(null);
  let changesOpen = $state<Set<string>>(new Set());

  let commits = $state<GitCommit[] | null>(null);
  let historyError = $state<string | null>(null);
  // History is windowed: only the first `shown` rows render, since each row is
  // its own SVG. Lanes are still computed over the full log so the graph is
  // correct; "show more" reveals the next page.
  let shown = $state(HISTORY_PAGE);

  let selected = $state<GitCommit | null>(null);
  let commitFiles = $state<PatchFile[] | null>(null);
  let commitTruncated = $state(false);
  let commitError = $state<string | null>(null);
  let commitOpen = $state<Set<string>>(new Set());

  $effect(() => {
    api
      .gitDiff(base)
      .then((d) => {
        changes = parsePatch(d.patch);
        changesTruncated = d.truncated ?? false;
        changesOpen = autoExpand(changes);
      })
      .catch((err) => (changesError = err instanceof Error ? err.message : String(err)));
  });

  $effect(() => {
    if (tab !== "history" || commits !== null) return;
    api
      .gitLog(base)
      .then((r) => (commits = r.commits ?? []))
      .catch((err) => (historyError = err instanceof Error ? err.message : String(err)));
  });

  const rows = $derived<GraphRow[]>(commits ? assignLanes(commits) : []);
  const cols = $derived(commits ? laneCount(rows) : 1);

  function select(c: GitCommit): void {
    if (selected?.hash === c.hash) {
      selected = null;
      return;
    }
    selected = c;
    commitFiles = null;
    commitError = null;
    commitOpen = new Set();
    api
      .gitDiff(base, c.hash)
      .then((d) => {
        if (selected?.hash !== c.hash) return;
        commitFiles = parsePatch(d.patch);
        commitTruncated = d.truncated ?? false;
        commitOpen = autoExpand(commitFiles);
      })
      .catch((err) => {
        if (selected?.hash === c.hash) commitError = err instanceof Error ? err.message : String(err);
      });
  }

  // Files render collapsed; a diff is parsed into rows but only turned into DOM
  // (and syntax-highlighted) when its file is expanded. Leading files are
  // auto-opened up to a row budget so small changesets show with no clicks
  // while a large one stays cheap until the user drills in.
  const AUTO_EXPAND_ROWS = 600;

  function fileKey(f: PatchFile): string {
    return f.path + (f.oldPath ?? "");
  }

  function autoExpand(files: PatchFile[]): Set<string> {
    const open = new Set<string>();
    let budget = AUTO_EXPAND_ROWS;
    for (const f of files) {
      const n = f.binary ? 0 : f.rows.length;
      if (n > budget) break;
      open.add(fileKey(f));
      budget -= n;
    }
    return open;
  }

  function toggleFile(open: Set<string>, key: string): Set<string> {
    const next = new Set(open);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    return next;
  }

  // graph geometry: one SVG cell per row, lanes as columns
  const COL = 14;
  const ROW = 28;
  const LANE_COLORS = [
    "var(--ansi-6)",
    "var(--ansi-4)",
    "var(--ansi-5)",
    "var(--ansi-3)",
    "var(--ansi-2)",
    "var(--ansi-12)",
    "var(--ansi-13)",
    "var(--ansi-11)",
  ];
  const laneColor = (lane: number) => LANE_COLORS[lane % LANE_COLORS.length];
  const cx = (lane: number) => lane * COL + COL / 2;

  // edge path from a lane at the row edge to the dot (or straight through)
  function edge(fromLane: number, toLane: number, down: boolean): string {
    const x1 = cx(fromLane);
    const x2 = cx(toLane);
    const [y1, y2] = down ? [ROW / 2, ROW] : [0, ROW / 2];
    if (x1 === x2) return `M ${x1} ${y1} L ${x2} ${y2}`;
    return `M ${x1} ${y1} C ${x1} ${(y1 + y2) / 2}, ${x2} ${(y1 + y2) / 2}, ${x2} ${y2}`;
  }

  function shortDate(iso: string): string {
    return iso.slice(0, 10);
  }

  function refChips(refs: string): string[] {
    return refs
      ? refs
          .split(", ")
          .map((r) => r.replace(/^HEAD -> /, ""))
          .filter((r) => r !== "HEAD")
      : [];
  }

  function statusSign(f: PatchFile): string {
    if (f.status === "add") return "A";
    if (f.status === "del") return "D";
    if (f.status === "rename") return "R";
    return "M";
  }
</script>

{#snippet patchList(files: PatchFile[], truncated: boolean, open: Set<string>, onToggle: (key: string) => void)}
  {#if files.length === 0}
    <p class="note">no textual changes</p>
  {:else}
    {#each files as f (f.path + (f.oldPath ?? ""))}
      {@const key = f.path + (f.oldPath ?? "")}
      {@const isOpen = open.has(key)}
      <section class="file">
        <button type="button" class="file-head" aria-expanded={isOpen} onclick={() => onToggle(key)}>
          <span class="caret" class:open={isOpen}>▸</span>
          <span class="status {f.status}">{statusSign(f)}</span>
          <span class="path" title={f.path}>
            {#if f.status === "rename" && f.oldPath}{f.oldPath} → {/if}{f.path}
          </span>
          {#if f.adds || f.dels}
            <span class="counts"><span class="plus">+{f.adds}</span> <span class="minus">−{f.dels}</span></span>
          {/if}
        </button>
        {#if isOpen}
          {#if f.binary}
            <p class="note">binary file</p>
          {:else if f.rows.length > 0}
            <DiffView diff={{ path: f.path, rows: f.rows, adds: f.adds, dels: f.dels }} />
          {/if}
        {/if}
      </section>
    {/each}
    {#if truncated}
      <p class="note">patch truncated by the server</p>
    {/if}
  {/if}
{/snippet}

<Overlay title="git" {onClose} wide>
  <div class="tabs" role="tablist">
    <button type="button" role="tab" class:active={tab === "changes"} aria-selected={tab === "changes"} onclick={() => (tab = "changes")}>
      changes
    </button>
    <button type="button" role="tab" class:active={tab === "history"} aria-selected={tab === "history"} onclick={() => (tab = "history")}>
      history
    </button>
  </div>

  {#if tab === "changes"}
    {#if changesError}
      <p class="note">{changesError}</p>
    {:else if changes === null}
      <p class="note">loading…</p>
    {:else if changes.length === 0}
      <p class="note">working tree clean</p>
    {:else}
      {@render patchList(changes, changesTruncated, changesOpen, (key) => (changesOpen = toggleFile(changesOpen, key)))}
    {/if}
  {:else if historyError}
    <p class="note">{historyError}</p>
  {:else if commits === null}
    <p class="note">loading…</p>
  {:else if commits.length === 0}
    <p class="note">no commits</p>
  {:else}
    <div class="log">
      {#each commits.slice(0, shown) as c, i (c.hash)}
        {@const row = rows[i]}
        <button type="button" class="commit" class:selected={selected?.hash === c.hash} onclick={() => select(c)}>
          <svg class="graph" width={cols * COL} height={ROW} viewBox="0 0 {cols * COL} {ROW}" aria-hidden="true">
            {#each row.lanesBefore as v, j}
              {#if v !== null && v === row.lanesAfter[j] && j !== row.lane}
                <path d={`M ${cx(j)} 0 L ${cx(j)} ${ROW}`} stroke={laneColor(j)} />
              {/if}
            {/each}
            {#each row.mergesFrom as m}
              <path d={edge(m, row.lane, false)} stroke={laneColor(m)} />
            {/each}
            {#each row.parentLanes as p}
              <path d={edge(row.lane, p, true)} stroke={laneColor(p)} />
            {/each}
            <circle cx={cx(row.lane)} cy={ROW / 2} r="3.2" fill={laneColor(row.lane)} />
          </svg>
          <span class="subject" title={c.subject}>{c.subject}</span>
          {#each refChips(c.refs) as ref}
            <span class="ref">{ref}</span>
          {/each}
          <span class="meta">{c.author} · {shortDate(c.date)} · {c.hash.slice(0, 7)}</span>
        </button>
        {#if selected?.hash === c.hash}
          <div class="patch">
            {#if commitError}
              <p class="note">{commitError}</p>
            {:else if commitFiles === null}
              <p class="note">loading…</p>
            {:else}
              {@render patchList(commitFiles, commitTruncated, commitOpen, (key) => (commitOpen = toggleFile(commitOpen, key)))}
            {/if}
          </div>
        {/if}
      {/each}
      {#if shown < commits.length}
        <button type="button" class="more" onclick={() => (shown += HISTORY_PAGE)}>
          show {Math.min(HISTORY_PAGE, commits.length - shown)} more
        </button>
      {/if}
    </div>
  {/if}
</Overlay>

<style>
  .tabs {
    display: flex;
    gap: 0.4rem;
    margin-bottom: 0.8rem;
  }
  .tabs button {
    font-family: var(--font-mono);
    font-size: var(--text-sm);
    letter-spacing: var(--track);
    text-transform: uppercase;
    color: var(--ink-muted);
    padding: 0.25rem 0.6rem;
    border: 1px solid var(--border);
    border-radius: var(--r-sm);
    background: transparent;
  }
  .tabs button:hover {
    color: var(--ink);
    background: var(--surface-2);
  }
  .tabs button.active {
    color: var(--live-ink);
    border-color: var(--live);
    background: var(--live-soft);
  }

  .file {
    margin-bottom: 1rem;
    border: 1px solid var(--border);
    border-radius: var(--r-md);
    overflow: hidden;
  }
  .file-head {
    display: flex;
    align-items: center;
    gap: 0.6em;
    width: 100%;
    text-align: left;
    padding: 0.35rem 0.6rem;
    font-family: var(--font-mono);
    font-size: var(--text-sm);
    color: var(--ink);
    background: var(--surface-2);
    border: none;
    border-bottom: 1px solid var(--border);
  }
  .file-head[aria-expanded="false"] {
    border-bottom: none;
  }
  .file-head:hover {
    background: var(--surface-3, var(--surface-2));
  }
  .caret {
    flex: none;
    color: var(--ink-faint);
    transition: transform 0.12s ease;
  }
  .caret.open {
    transform: rotate(90deg);
  }
  .status {
    flex: none;
    width: 1.2em;
    text-align: center;
    font-weight: 600;
  }
  .status.add {
    color: var(--ok);
  }
  .status.del {
    color: var(--err);
  }
  .status.rename {
    color: var(--warn);
  }
  .status.modify {
    color: var(--think);
  }
  .path {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .counts {
    margin-left: auto;
    flex: none;
  }
  .plus {
    color: var(--ok);
  }
  .minus {
    color: var(--err);
  }
  .file :global(.diff) {
    padding: 0.3rem 0;
    background: var(--code-bg);
  }

  .log {
    font-family: var(--font-mono);
    font-size: var(--text-sm);
  }
  .commit {
    display: flex;
    align-items: center;
    gap: 0.6em;
    width: 100%;
    text-align: left;
    padding: 0 0.4rem;
    border-radius: var(--r-sm);
    color: var(--ink);
  }
  .commit:hover {
    background: var(--surface-2);
  }
  .commit.selected {
    background: var(--live-soft);
  }
  .graph {
    flex: none;
    display: block;
  }
  .graph path {
    fill: none;
    stroke-width: 1.5;
  }
  .subject {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .ref {
    flex: none;
    font-size: var(--text-xs);
    color: var(--live-ink);
    border: 1px solid var(--live);
    border-radius: var(--r-sm);
    padding: 0 0.3em;
    background: var(--live-soft);
  }
  .meta {
    flex: none;
    margin-left: auto;
    color: var(--ink-faint);
    font-size: var(--text-xs);
  }
  .patch {
    margin: 0.3rem 0 0.8rem;
    padding-left: 0.4rem;
  }

  .more {
    display: block;
    width: 100%;
    margin-top: 0.4rem;
    padding: 0.4rem;
    font-family: var(--font-mono);
    font-size: var(--text-sm);
    letter-spacing: var(--track);
    text-transform: uppercase;
    color: var(--ink-muted);
    border: 1px solid var(--border);
    border-radius: var(--r-sm);
    background: transparent;
  }
  .more:hover {
    color: var(--ink);
    background: var(--surface-2);
  }

  .note {
    margin: 0.6rem 0 0;
    font-size: var(--text-sm);
    color: var(--ink-muted);
  }
</style>
