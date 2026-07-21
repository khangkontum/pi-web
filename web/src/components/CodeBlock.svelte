<script lang="ts">
  // Fenced code blocks: streamdown's shiki element plus our own copy control.
  // streamdown's built-in controls are sized by Tailwind classes we don't
  // ship, so they render unusable — controls stay off in Prose and the copy
  // button lives here, styled like the rest of the chrome. Shiki is heavy, so
  // the highlighting element loads lazily and blocks upgrade in place; until
  // then a plain <pre> keeps the same shape.
  import type { Tokens } from "marked";

  let { token, id }: { token: Tokens.Code; id: string } = $props();

  let Code = $state<typeof import("svelte-streamdown/code").default | null>(null);
  import("svelte-streamdown/code").then((m) => (Code = m.default));

  let copied = $state(false);
  let resetTimer: ReturnType<typeof setTimeout> | undefined;

  async function copy() {
    try {
      await navigator.clipboard.writeText(token.text);
    } catch {
      return;
    }
    copied = true;
    clearTimeout(resetTimer);
    resetTimer = setTimeout(() => (copied = false), 1600);
  }
</script>

<div class="codeblock">
  {#if Code}
    <Code {token} {id} />
  {:else}
    <pre><code>{token.text}</code></pre>
  {/if}
  <button type="button" class="copy" class:copied onclick={copy} title="Copy code">
    {copied ? "copied" : "copy"}
  </button>
</div>

<style>
  .codeblock {
    position: relative;
  }
  /* streamdown's header row only carries the language name — hide it */
  .codeblock :global([data-streamdown-code] > div:first-child) {
    display: none;
  }
  .copy {
    position: absolute;
    top: 0.4rem;
    right: 0.4rem;
    padding: 0.1rem 0.45rem;
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    letter-spacing: var(--track);
    text-transform: uppercase;
    color: var(--ink-faint);
    background: var(--code-bg);
    border-radius: var(--r-sm);
    opacity: 0;
    transition: opacity 120ms;
  }
  .codeblock:hover .copy,
  .copy:focus-visible,
  .copy.copied {
    opacity: 1;
  }
  .copy:hover,
  .copy.copied {
    color: var(--live-ink);
  }
  /* no hover on touch — keep it reachable */
  @media (hover: none) {
    .copy {
      opacity: 1;
    }
  }
</style>
