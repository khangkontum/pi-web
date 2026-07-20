<script lang="ts">
  // Inline notices: extension errors, final retry failures. Loud enough to
  // never scroll past unseen, quiet enough not to shout.
  import type { NoticeItem } from "../lib/feed";

  let { item }: { item: NoticeItem } = $props();
</script>

<div class="notice {item.level}">
  <span class="label title">{item.title}</span>
  {#if item.detail}
    <pre class="detail">{item.detail}</pre>
  {/if}
</div>

<style>
  .notice {
    padding: 0.5rem 0.75rem;
    border-left: 2px solid var(--ink-muted);
    background: var(--surface-2);
    border-radius: 0 var(--r-md) var(--r-md) 0;
  }
  .notice.error {
    border-left-color: var(--err);
    background: var(--err-soft);
  }
  .notice.warn {
    border-left-color: var(--warn);
    background: var(--warn-soft);
  }
  .notice.error .title {
    color: var(--err);
  }
  .notice.warn .title {
    color: var(--warn);
  }
  .detail {
    margin: 0.3rem 0 0;
    font-size: var(--text-sm);
    white-space: pre-wrap;
    word-break: break-word;
  }
</style>
