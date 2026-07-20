<script lang="ts">
  // Hardened markdown for untrusted streaming model output. svelte-streamdown
  // parses with marked and renders through Svelte components — there is no
  // raw-innerHTML path, which matters because the feed can invoke bash
  // (feed XSS = shell RCE). Nothing in this app may bypass this component
  // into {@html}. Code highlighting (shiki) is heavy, so it loads lazily and
  // code blocks upgrade in place; the shiki theme follows the app theme.
  import { Streamdown } from "svelte-streamdown";
  import { theme } from "../lib/theme.svelte";

  let { content, muted = false }: { content: string; muted?: boolean } = $props();

  let Code = $state<typeof import("svelte-streamdown/code").default | null>(null);
  import("svelte-streamdown/code").then((m) => (Code = m.default));

  const shikiTheme = $derived(theme.resolved === "dark" ? "vitesse-dark" : "vitesse-light");
</script>

<div class="prose" class:muted>
  <Streamdown {content} {shikiTheme} parseIncompleteMarkdown components={Code ? { code: Code } : {}} />
</div>

<style>
  .prose {
    font-family: var(--font-prose);
    font-size: var(--text-prose);
    line-height: 1.68;
    color: var(--ink);
    overflow-wrap: break-word;
  }
  .prose.muted {
    color: var(--ink-muted);
  }
  .prose :global(p) {
    margin: 0 0 0.8em;
  }
  .prose :global(:is(h1, h2, h3, h4, h5, h6)) {
    font-family: var(--font-prose);
    font-weight: 650;
    line-height: 1.3;
    margin: 1.3em 0 0.5em;
  }
  .prose :global(h1) {
    font-size: var(--text-xl);
  }
  .prose :global(h2) {
    font-size: var(--text-lg);
  }
  .prose :global(:is(h3, h4, h5, h6)) {
    font-size: var(--text-prose);
  }
  .prose :global(:is(ul, ol)) {
    margin: 0 0 0.8em;
    padding-left: 1.5em;
  }
  .prose :global(li) {
    margin: 0.25em 0;
  }
  .prose :global(a) {
    color: var(--live-ink);
    text-decoration-thickness: 1px;
    text-underline-offset: 2px;
  }
  .prose :global(blockquote) {
    margin: 0 0 0.8em;
    padding-left: 1em;
    border-left: 2px solid var(--border-strong);
    color: var(--ink-muted);
    font-style: italic;
  }
  /* inline code switches to the machine voice */
  .prose :global(:not(pre) > code) {
    font-family: var(--font-mono);
    font-size: 0.82em;
    padding: 0.1em 0.35em;
    background: var(--code-bg);
    border: 1px solid var(--border);
    border-radius: var(--r-sm);
  }
  /* fenced code (shiki sets inline colors on inner spans) */
  .prose :global(pre) {
    margin: 0 0 0.8em;
    padding: 0.75rem 0.9rem;
    overflow-x: auto;
    font-size: var(--text-sm);
    line-height: 1.55;
    background: var(--code-bg) !important;
    border: 1px solid var(--border);
    border-radius: var(--r-md);
  }
  .prose :global(pre code) {
    font-family: var(--font-mono);
    background: none;
    border: 0;
    padding: 0;
  }
  .prose :global(table) {
    display: block;
    max-width: 100%;
    overflow-x: auto;
    margin: 0 0 0.8em;
    border-collapse: collapse;
    font-size: var(--text-md);
  }
  .prose :global(:is(th, td)) {
    padding: 0.35em 0.65em;
    border: 1px solid var(--border);
    text-align: left;
  }
  .prose :global(th) {
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    letter-spacing: var(--track);
    text-transform: uppercase;
    color: var(--ink-muted);
    background: var(--surface-2);
  }
  .prose :global(hr) {
    border: 0;
    border-top: 1px solid var(--border);
    margin: 1.2em 0;
  }
  .prose :global(img) {
    max-width: 100%;
    border-radius: var(--r-sm);
  }
  .prose :global(> :last-child),
  .prose :global(> div > :last-child) {
    margin-bottom: 0;
  }
</style>
