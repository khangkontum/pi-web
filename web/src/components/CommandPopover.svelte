<script lang="ts" module>
  // one fetch per live session per app session; the list only changes when
  // extensions/prompts/skills are (re)loaded, not per keystroke
  const cache = new Map<string, CommandInfo[]>();
</script>

<script lang="ts">
  // The /-command autocomplete above the composer, mirroring pi's TUI: the
  // list comes from /api/sessions/{id}/commands (pi's get_commands), matching
  // is client-side fuzzy on the name. The composer owns the keyboard and
  // forwards keys here.
  import { api, type CommandInfo } from "../lib/api";
  import { filterFuzzy, highlightSegments, type FuzzyMatch } from "../lib/fuzzy";

  let {
    sessionId,
    query,
    onPick,
    onClose,
  }: {
    sessionId: string;
    query: string;
    onPick: (name: string) => void;
    onClose: () => void;
  } = $props();

  let commands = $state<CommandInfo[]>([]);
  let failed = $state<string | null>(null);
  let loaded = $state(false);
  let active = $state(0);
  let listEl = $state<HTMLElement | null>(null);

  $effect(() => {
    const hit = cache.get(sessionId);
    if (hit) {
      commands = hit;
      loaded = true;
      return;
    }
    api
      .commands(sessionId)
      .then((res) => {
        cache.set(sessionId, res.commands ?? []);
        commands = res.commands ?? [];
        loaded = true;
      })
      .catch((err) => {
        failed = err instanceof Error ? err.message : String(err);
      });
  });

  const byName = $derived(new Map(commands.map((c) => [c.name, c])));
  const matches = $derived<FuzzyMatch[]>(
    query === ""
      ? commands.map((c) => ({ text: c.name, score: 0, positions: [] })).slice(0, 40)
      : filterFuzzy(
          query,
          commands.map((c) => c.name),
          40,
        ),
  );

  $effect(() => {
    void matches;
    active = 0;
  });

  // tag mirrors the TUI's origin marker: [u]ser / [p]roject / [f]ile path
  // for prompts and skills, [x] for extension commands.
  function tag(c: CommandInfo): string {
    if (c.source === "extension") return "x";
    if (c.location === "path") return "f";
    return c.location?.[0] ?? c.source[0];
  }

  // handleKey returns true when the key was consumed.
  export function handleKey(e: KeyboardEvent): boolean {
    if (e.key === "ArrowDown") {
      active = Math.min(matches.length - 1, active + 1);
      scrollActive();
      return true;
    }
    if (e.key === "ArrowUp") {
      active = Math.max(0, active - 1);
      scrollActive();
      return true;
    }
    if (e.key === "Enter" || e.key === "Tab") {
      if (matches[active]) {
        onPick(matches[active].text);
        return true;
      }
      onClose();
      return false;
    }
    if (e.key === "Escape") {
      onClose();
      return true;
    }
    return false;
  }

  function scrollActive(): void {
    listEl?.children[active]?.scrollIntoView({ block: "nearest" });
  }
</script>

<div class="commands" role="listbox" aria-label="Commands">
  {#if failed}
    <div class="note">commands unavailable: {failed}</div>
  {:else if matches.length === 0}
    <div class="note">
      {!loaded ? "loading commands…" : commands.length === 0 ? "no commands in this session" : "no matching commands"}
    </div>
  {:else}
    <div class="list" bind:this={listEl}>
      {#each matches as m, i (m.text)}
        {@const cmd = byName.get(m.text)}
        <button
          type="button"
          role="option"
          aria-selected={i === active}
          class="item"
          class:active={i === active}
          onpointerenter={() => (active = i)}
          onclick={() => onPick(m.text)}
        >
          <span class="name">
            /{#each highlightSegments(m) as part}
              {#if part.hit}<mark>{part.text}</mark>{:else}{part.text}{/if}
            {/each}
          </span>
          {#if cmd}
            <span class="tag">[{tag(cmd)}]</span>
            <span class="desc">{cmd.description ?? ""}</span>
          {/if}
        </button>
      {/each}
    </div>
  {/if}
</div>

<style>
  .commands {
    position: absolute;
    bottom: calc(100% + 6px);
    left: 0;
    right: 0;
    z-index: 45;
    max-height: 310px;
    display: flex;
    flex-direction: column;
    background: var(--surface);
    border: 1px solid var(--border-strong);
    border-radius: var(--r-md);
    box-shadow: var(--shadow);
    overflow: hidden;
  }
  .list {
    overflow-y: auto;
    padding: 4px;
  }
  .item {
    display: flex;
    align-items: baseline;
    gap: 0.6rem;
    width: 100%;
    padding: 0.3rem 0.5rem;
    font-size: var(--text-sm);
    text-align: left;
    border-radius: var(--r-sm);
    color: var(--ink-muted);
  }
  .item.active {
    background: var(--accent-hover);
    color: var(--ink);
  }
  .name {
    flex: none;
    font-family: var(--font-mono);
    color: var(--live-ink);
  }
  .tag {
    flex: none;
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    color: var(--ink-faint);
  }
  .desc {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--ink-faint);
  }
  mark {
    background: none;
    color: inherit;
    font-weight: 600;
    text-decoration: underline;
  }
  .note {
    padding: 0.4rem 0.6rem;
    font-size: var(--text-xs);
    color: var(--ink-faint);
  }
</style>
