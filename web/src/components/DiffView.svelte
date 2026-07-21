<script lang="ts">
  // Diff rows from lib/diff rendered as a unified diff. Rows are data;
  // this template is the only place they become DOM.
  import type { ToolDiff } from "../lib/diff";
  import { langForPath, tokenize, type TokenLine } from "../lib/highlight";

  let { diff }: { diff: ToolDiff } = $props();

  // Row texts are tokenized as one block so grammar state flows across
  // lines; gap rows are excluded and indices mapped back. Del/add tints stay
  // on the row background underneath the token colors.
  let rowTokens = $state<Map<number, TokenLine> | null>(null);

  // Above this row count a diff renders as plain text: shiki tokenization of a
  // few thousand lines is the expensive part, and a diff that large is being
  // scanned, not read line by line.
  const MAX_HIGHLIGHT_ROWS = 2000;

  $effect(() => {
    rowTokens = null;
    const current = diff;
    const lang = langForPath(current.path ?? "");
    if (!lang || current.rows.length > MAX_HIGHLIGHT_ROWS) return;
    const idx: number[] = [];
    const texts: string[] = [];
    current.rows.forEach((r, i) => {
      if (r.kind !== "gap") {
        idx.push(i);
        texts.push(r.text);
      }
    });
    if (texts.length === 0) return;
    tokenize(texts.join("\n"), lang).then((lines) => {
      if (!lines || diff !== current) return;
      const map = new Map<number, TokenLine>();
      idx.forEach((rowIdx, j) => {
        if (lines[j]?.length) map.set(rowIdx, lines[j]);
      });
      rowTokens = map;
    });
  });
</script>

<div class="diff">
  {#each diff.rows as row, i}
    {#if row.kind === "gap"}
      <div class="row gap"><span class="sign">·</span><span class="text">···</span></div>
    {:else}
      {@const tokens = rowTokens?.get(i)}
      <div class="row {row.kind}">
        <span class="sign">{row.kind === "add" ? "+" : row.kind === "del" ? "-" : " "}</span>
        <span class="text">
          {#if tokens}
            {#each tokens as t}<span style:color={t.color}>{t.content}</span>{/each}
          {:else}
            {row.text || " "}
          {/if}
        </span>
      </div>
    {/if}
  {/each}
</div>

<style>
  .diff {
    font-family: var(--font-mono);
    font-size: var(--text-sm);
    line-height: 1.5;
    overflow-x: auto;
  }
  .row {
    display: flex;
    min-width: max-content;
  }
  .sign {
    flex: none;
    width: 1.4em;
    text-align: center;
    color: var(--ink-faint);
    user-select: none;
  }
  .text {
    white-space: pre;
  }
  .row.add {
    background: color-mix(in srgb, var(--ok) 14%, transparent);
  }
  .row.add .sign {
    color: var(--ok);
  }
  .row.del {
    background: color-mix(in srgb, var(--err) 12%, transparent);
  }
  .row.del .sign {
    color: var(--err);
  }
  .row.gap {
    color: var(--ink-faint);
  }
</style>
