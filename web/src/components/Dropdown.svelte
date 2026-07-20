<script lang="ts">
  // Custom select: button + listbox panel, fully keyboard-driven. Used for
  // every picker in the app — native <select> is never used.
  export interface DropdownItem {
    value: string;
    label: string;
    hint?: string;
  }

  let {
    items,
    value = null,
    onSelect,
    label,
    buttonText,
    align = "left",
    up = false,
  }: {
    items: DropdownItem[];
    value?: string | null;
    onSelect: (value: string) => void;
    label: string;
    buttonText: string;
    align?: "left" | "right";
    up?: boolean;
  } = $props();

  let open = $state(false);
  let active = $state(-1);
  let root: HTMLElement;
  let list = $state<HTMLElement | null>(null);

  function toggle(): void {
    open = !open;
    if (open) active = Math.max(0, items.findIndex((i) => i.value === value));
  }

  function choose(v: string): void {
    open = false;
    onSelect(v);
  }

  function onKeydown(e: KeyboardEvent): void {
    if (!open) {
      if (e.key === "ArrowDown" || e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        toggle();
      }
      return;
    }
    if (e.key === "Escape") {
      e.preventDefault();
      open = false;
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      active = Math.min(items.length - 1, active + 1);
      scrollActive();
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      active = Math.max(0, active - 1);
      scrollActive();
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (items[active]) choose(items[active].value);
    } else if (e.key === "Tab") {
      open = false;
    }
  }

  function scrollActive(): void {
    list?.children[active]?.scrollIntoView({ block: "nearest" });
  }

  function onFocusOut(e: FocusEvent): void {
    if (!root.contains(e.relatedTarget as Node)) open = false;
  }
</script>

<!-- keydown listens here so both the trigger and the options share one
     handler; focus always sits on a button inside -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="dropdown" bind:this={root} onfocusout={onFocusOut} onkeydown={onKeydown}>
  <button
    type="button"
    class="trigger"
    class:open
    aria-haspopup="listbox"
    aria-expanded={open}
    aria-label={label}
    onclick={toggle}
  >
    <span class="text">{buttonText}</span>
    <span class="caret" aria-hidden="true">{up ? "▴" : "▾"}</span>
  </button>
  {#if open}
    <div class="panel" class:right={align === "right"} class:up role="listbox" aria-label={label} bind:this={list}>
      {#each items as item, i (item.value)}
        <button
          type="button"
          role="option"
          aria-selected={item.value === value}
          class="item"
          class:active={i === active}
          class:selected={item.value === value}
          onclick={() => choose(item.value)}
          onpointerenter={() => (active = i)}
        >
          <span class="item-label">{item.label}</span>
          {#if item.hint}<span class="hint">{item.hint}</span>{/if}
        </button>
      {/each}
      {#if items.length === 0}
        <div class="empty">nothing here</div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .dropdown {
    position: relative;
    display: inline-block;
  }
  .trigger {
    display: inline-flex;
    align-items: center;
    gap: 0.45em;
    max-width: 100%;
    padding: 0.25rem 0.55rem;
    font-size: var(--text-xs);
    letter-spacing: 0.02em;
    color: var(--ink-muted);
    border: 1px solid var(--border);
    border-radius: var(--r-sm);
    background: var(--surface);
  }
  .trigger:hover {
    color: var(--ink);
    border-color: var(--border-strong);
    background: var(--accent-hover);
  }
  .trigger.open {
    color: var(--ink);
    border-color: var(--live);
  }
  .text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .caret {
    color: var(--ink-faint);
    font-size: 0.85em;
  }
  .panel {
    position: absolute;
    z-index: 40;
    top: calc(100% + 4px);
    left: 0;
    min-width: max(100%, 14rem);
    max-width: 24rem;
    max-height: 310px;
    overflow-y: auto;
    padding: 4px;
    background: var(--surface);
    border: 1px solid var(--border-strong);
    border-radius: var(--r-md);
    box-shadow: var(--shadow);
  }
  .panel.right {
    left: auto;
    right: 0;
  }
  .panel.up {
    top: auto;
    bottom: calc(100% + 4px);
  }
  .item {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 1em;
    width: 100%;
    padding: 0.35rem 0.5rem;
    font-size: var(--text-sm);
    text-align: left;
    border-radius: var(--r-sm);
    color: var(--ink);
  }
  .item.active {
    background: var(--accent-hover);
  }
  .item.selected {
    color: var(--live-ink);
    font-weight: 600;
  }
  .item-label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .hint {
    flex: none;
    font-size: var(--text-xs);
    color: var(--ink-faint);
  }
  .empty {
    padding: 0.5rem;
    font-size: var(--text-sm);
    color: var(--ink-faint);
  }
</style>
