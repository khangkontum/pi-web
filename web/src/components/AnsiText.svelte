<script lang="ts">
  // Styled terminal output. The parser emits data spans; this template builds
  // DOM from them — never HTML strings.
  import { parseAnsi } from "../lib/ansi";

  let { text }: { text: string } = $props();

  const spans = $derived(parseAnsi(text));
</script>

<pre class="ansi">{#each spans as s}<span
      style:color={s.fg}
      style:background-color={s.bg}
      class:b={s.bold}
      class:d={s.dim}
      class:i={s.italic}
      class:u={s.underline}>{s.text}</span>{/each}</pre>

<style>
  .ansi {
    margin: 0;
    font-family: var(--font-mono);
    font-size: var(--text-sm);
    line-height: 1.5;
    white-space: pre-wrap;
    word-break: break-word;
  }
  .b {
    font-weight: 600;
  }
  .d {
    opacity: 0.6;
  }
  .i {
    font-style: italic;
  }
  .u {
    text-decoration: underline;
  }
</style>
