<script lang="ts">
  // The session rail: every session pi knows about, grouped by project
  // (working directory), newest first. Live children breathe. Big histories
  // stay quiet: stale groups fold by default (explicit choices win and
  // persist), expanded groups cap at GROUP_CAP sessions, and the filter box
  // fuzzy-narrows everything.
  import {
    GROUP_CAP,
    filterGroups,
    isGroupCollapsed,
    rail,
    type ProjectGroup,
  } from "../lib/rail.svelte";
  import { router } from "../lib/router.svelte";
  import { session } from "../lib/session.svelte";
  import type { SessionSummary } from "../lib/api";

  let {
    onNew,
    onNewIn,
  }: {
    onNew: () => void;
    // quick-start a session in an existing project folder, skipping the picker
    onNewIn: (cwd: string) => void;
  } = $props();

  const filtering = $derived(rail.filter.trim() !== "");
  const shown = $derived(filterGroups(rail.groups, rail.filter));
  // staleness is judged when the listing refreshes, not per animation frame
  const now = $derived.by(() => {
    void rail.groups;
    return Date.now();
  });

  function collapsed(group: ProjectGroup): boolean {
    if (filtering) return false;
    return isGroupCollapsed(group, rail.prefs, session.cwd, now);
  }

  function visibleSessions(group: ProjectGroup): SessionSummary[] {
    if (filtering || rail.showAll[group.cwd]) return group.sessions;
    const top = group.sessions.slice(0, GROUP_CAP);
    // the open session must never hide behind the cap
    const active = group.sessions.find((s) => s.id === session.id);
    if (active && !top.includes(active)) top.push(active);
    return top;
  }

  function openFirstMatch(): void {
    const first = shown.find((g) => g.sessions.length > 0)?.sessions[0];
    if (first) {
      rail.filter = "";
      router.openSession(first.id);
    }
  }

  function onFilterKeydown(e: KeyboardEvent): void {
    if (e.isComposing || e.keyCode === 229) return;
    if (e.key === "Escape") {
      rail.filter = "";
      (e.target as HTMLInputElement).blur();
    } else if (e.key === "Enter" && filtering) {
      e.preventDefault();
      openFirstMatch();
    }
  }

  function timeAgo(iso: string): string {
    const t = new Date(iso).getTime();
    if (Number.isNaN(t)) return "";
    const s = Math.max(0, (Date.now() - t) / 1000);
    if (s < 60) return "now";
    if (s < 3600) return `${Math.floor(s / 60)}m`;
    if (s < 86400) return `${Math.floor(s / 3600)}h`;
    return `${Math.floor(s / 86400)}d`;
  }
</script>

<nav class="rail" aria-label="Sessions">
  <div class="head">
    <span class="wordmark">pi<span class="dash">—</span>web</span>
    <button type="button" class="new" onclick={onNew} title="New session (⌘K)">+ new</button>
  </div>

  <div class="filter-row">
    <input
      type="text"
      class="filter"
      placeholder="filter sessions"
      aria-label="Filter sessions"
      bind:value={rail.filter}
      onkeydown={onFilterKeydown}
    />
    {#if filtering}
      <button type="button" class="clear" aria-label="Clear filter" onclick={() => (rail.filter = "")}>
        ✕
      </button>
    {/if}
  </div>

  <div class="groups">
    {#if rail.error}
      <div class="note">sessions unavailable: {rail.error}</div>
    {/if}
    {#each shown as group (group.cwd)}
      {@const isCollapsed = collapsed(group)}
      {@const isActiveGroup = group.cwd === session.cwd}
      {@const visible = isCollapsed ? [] : visibleSessions(group)}
      <section>
        <h2 class="group">
          <button
            type="button"
            class="group-head"
            class:active-mark={isCollapsed && isActiveGroup}
            aria-expanded={!isCollapsed}
            title={group.cwd}
            onclick={() => rail.setGroup(group.cwd, isCollapsed ? "expanded" : "collapsed")}
          >
            <span class="chev" aria-hidden="true">{isCollapsed ? "▸" : "▾"}</span>
            <span class="label group-name">{group.label}</span>
            {#if isCollapsed}
              <span class="count">{group.sessions.length}</span>
              {#if group.sessions.some((s) => s.live)}
                <span class="pulse" title="live session inside"></span>
              {/if}
            {/if}
          </button>
          {#if group.cwd.startsWith("/")}
            <button
              type="button"
              class="group-new"
              title="New session in {group.label}"
              aria-label="New session in {group.label}"
              onclick={() => onNewIn(group.cwd)}
            >
              +
            </button>
          {/if}
        </h2>
        {#each visible as s (s.id)}
          <button
            type="button"
            class="item"
            class:active={s.id === session.id}
            onclick={() => router.openSession(s.id)}
          >
            <span class="live-slot">
              {#if s.live}<span class="pulse" title="live"></span>{/if}
            </span>
            <span class="title">{s.title || s.id}</span>
            <span class="when">{timeAgo(s.updatedAt)}</span>
          </button>
        {/each}
        {#if !isCollapsed && visible.length < group.sessions.length}
          <button type="button" class="more" onclick={() => (rail.showAll[group.cwd] = true)}>
            …{group.sessions.length - visible.length} more
          </button>
        {/if}
      </section>
    {/each}
    {#if !rail.error && shown.length === 0}
      <div class="note">
        {filtering ? `nothing matches “${rail.filter.trim()}”` : "No sessions yet. Start one with + new."}
      </div>
    {/if}
  </div>
</nav>

<style>
  .rail {
    display: flex;
    flex-direction: column;
    height: 100%;
    background: var(--surface);
    border-right: 1px solid var(--border);
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.8rem 0.9rem;
  }
  .wordmark {
    font-size: var(--text-md);
    font-weight: 600;
    letter-spacing: 0.04em;
  }
  .dash {
    color: var(--live);
  }
  .new {
    padding: 0.15rem 0.55rem;
    font-size: var(--text-xs);
    letter-spacing: 0.02em;
    color: var(--ink-muted);
    border: 1px solid var(--border);
    border-radius: var(--r-sm);
  }
  .new:hover {
    color: var(--ink);
    border-color: var(--live);
  }
  .filter-row {
    position: relative;
    padding: 0 0.9rem 0.7rem;
    border-bottom: 1px solid var(--border);
  }
  .filter {
    width: 100%;
    padding: 0.3rem 1.6rem 0.3rem 0.55rem;
    font-size: var(--text-sm);
    color: var(--ink);
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--r-sm);
    outline: none;
  }
  .filter:focus {
    border-color: var(--live);
  }
  .filter::placeholder {
    color: var(--ink-faint);
  }
  .clear {
    position: absolute;
    right: 1.2rem;
    top: 0.35rem;
    font-size: var(--text-xs);
    color: var(--ink-faint);
  }
  .clear:hover {
    color: var(--ink);
  }
  .groups {
    flex: 1;
    overflow-y: auto;
    padding: 0.5rem 0.5rem 1rem;
  }
  section {
    margin-bottom: 0.9rem;
  }
  h2.group {
    display: flex;
    align-items: center;
    gap: 2px;
    margin: 0 0 0.2rem;
  }
  .group-new {
    flex: none;
    width: 1.35rem;
    height: 1.35rem;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font-size: var(--text-sm);
    color: var(--ink-faint);
    border-radius: var(--r-sm);
    opacity: 0;
    transition: opacity 80ms ease;
  }
  .group:hover .group-new,
  .group:focus-within .group-new {
    opacity: 1;
  }
  /* no hover on touch — keep the affordance visible */
  @media (hover: none) {
    .group-new {
      opacity: 1;
    }
  }
  .group-new:hover {
    color: var(--live-ink);
    background: var(--accent-hover);
  }
  .group-head {
    display: flex;
    align-items: center;
    gap: 0.45em;
    flex: 1;
    min-width: 0;
    padding: 0.3rem 0.45rem 0.15rem;
    text-align: left;
    border-radius: var(--r-sm);
  }
  .group-head:hover {
    background: var(--accent-hover);
  }
  .group-head:hover .group-name {
    color: var(--ink);
  }
  /* the current session's group, when explicitly collapsed, keeps a trace */
  .group-head.active-mark {
    box-shadow: inset 2px 0 0 var(--live);
  }
  .group-head.active-mark .group-name {
    color: var(--ink);
  }
  .chev {
    flex: none;
    font-size: var(--text-xs);
    color: var(--ink-faint);
  }
  .group-name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .count {
    flex: none;
    font-size: var(--text-xs);
    color: var(--ink-faint);
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0 0.45em;
  }
  .item {
    display: flex;
    align-items: baseline;
    gap: 0.5em;
    width: 100%;
    padding: 0.32rem 0.45rem;
    font-size: var(--text-sm);
    text-align: left;
    border-radius: var(--r-sm);
    color: var(--ink-muted);
  }
  .item:hover {
    background: var(--accent-hover);
    color: var(--ink);
  }
  .item.active {
    background: var(--surface-2);
    color: var(--ink);
  }
  .live-slot {
    flex: none;
    width: 9px;
    display: inline-flex;
    align-items: center;
  }
  .title {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .when {
    flex: none;
    font-size: var(--text-xs);
    color: var(--ink-faint);
  }
  .more {
    display: block;
    width: 100%;
    padding: 0.2rem 0.45rem 0.2rem 1.35rem;
    font-size: var(--text-xs);
    text-align: left;
    color: var(--ink-faint);
    border-radius: var(--r-sm);
  }
  .more:hover {
    color: var(--live-ink);
    background: var(--accent-hover);
  }
  .note {
    padding: 0.6rem 0.45rem;
    font-size: var(--text-sm);
    color: var(--ink-faint);
  }
</style>
