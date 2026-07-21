<script lang="ts">
  // Select agent prose and a floating pill appears; clicking it hands the
  // selected text to the composer as a markdown blockquote. Tracks the live
  // document selection, so it works anywhere an assistant block renders.
  let { onQuote }: { onQuote: (text: string) => void } = $props();

  let pos = $state<{ x: number; y: number; below: boolean } | null>(null);
  let selected = "";

  function insideAssistant(node: Node | null): boolean {
    const el = node instanceof Element ? node : (node?.parentElement ?? null);
    return !!el?.closest(".assistant");
  }

  function sync(): void {
    const sel = window.getSelection();
    if (!sel || sel.isCollapsed || sel.rangeCount === 0) {
      pos = null;
      return;
    }
    const text = sel.toString().trim();
    if (!text || !insideAssistant(sel.anchorNode) || !insideAssistant(sel.focusNode)) {
      pos = null;
      return;
    }
    const rect = sel.getRangeAt(0).getBoundingClientRect();
    if (rect.width === 0 && rect.height === 0) {
      pos = null;
      return;
    }
    selected = text;
    const below = rect.top < 44;
    pos = {
      x: Math.min(Math.max(rect.left + rect.width / 2, 48), window.innerWidth - 48),
      y: below ? rect.bottom + 8 : rect.top - 8,
      below,
    };
  }

  function quote(): void {
    onQuote(selected);
    window.getSelection()?.removeAllRanges();
    pos = null;
  }
</script>

<svelte:document onselectionchange={sync} />
<svelte:window onscrollcapture={sync} onresize={sync} />

{#if pos}
  <button
    type="button"
    class="quote"
    class:below={pos.below}
    style:left="{pos.x}px"
    style:top="{pos.y}px"
    onpointerdown={(e) => e.preventDefault()}
    onclick={quote}
  >
    ❝ quote
  </button>
{/if}

<style>
  .quote {
    position: fixed;
    z-index: 40;
    transform: translate(-50%, -100%);
    padding: 0.25rem 0.7rem;
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    letter-spacing: 0.02em;
    color: var(--surface);
    background: var(--ink);
    border-radius: 999px;
    box-shadow: var(--shadow);
    white-space: nowrap;
  }
  .quote.below {
    transform: translate(-50%, 0);
  }
  .quote:hover {
    background: var(--live-ink);
  }
</style>
