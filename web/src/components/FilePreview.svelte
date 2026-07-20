<script lang="ts">
  // File preview overlay: text via /api/file; images, PDF, and audio via
  // /api/raw. Anything else falls back to a raw link.
  import Overlay from "./Overlay.svelte";
  import { api, type FileView } from "../lib/api";

  let {
    path,
    base,
    onClose,
  }: {
    path: string;
    base: string | null;
    onClose: () => void;
  } = $props();

  type Kind = "image" | "pdf" | "audio" | "text";

  function kindOf(p: string): Kind {
    const ext = p.slice(p.lastIndexOf(".") + 1).toLowerCase();
    if (["png", "jpg", "jpeg", "gif", "webp", "svg", "bmp", "ico", "avif"].includes(ext)) return "image";
    if (ext === "pdf") return "pdf";
    if (["mp3", "wav", "ogg", "m4a", "flac", "aac"].includes(ext)) return "audio";
    return "text";
  }

  const kind = $derived(kindOf(path));
  const rawUrl = $derived(api.rawUrl(path, base));

  let view = $state<FileView | null>(null);
  let failed = $state<string | null>(null);

  $effect(() => {
    view = null;
    failed = null;
    if (kind !== "text") return;
    api
      .file(path, base)
      .then((v) => (view = v))
      .catch((err) => (failed = err instanceof Error ? err.message : String(err)));
  });

  const lines = $derived(view && !view.binary ? view.content.split("\n") : []);
</script>

<Overlay title={path} {onClose} wide>
  {#if kind === "image"}
    <img class="media" src={rawUrl} alt={path} />
  {:else if kind === "pdf"}
    <iframe class="pdf" src={rawUrl} title={path}></iframe>
  {:else if kind === "audio"}
    <audio class="audio" controls src={rawUrl}></audio>
  {:else if failed}
    <p class="note">{failed}</p>
  {:else if !view}
    <p class="note">loading…</p>
  {:else if view.binary}
    <p class="note">
      binary file ({view.size} bytes) — <a href={rawUrl} target="_blank" rel="noreferrer">open raw</a>
    </p>
  {:else}
    <div class="code">
      {#each lines as line, i (i)}
        <div class="ln"><span class="no">{i + 1}</span><span class="tx">{line || " "}</span></div>
      {/each}
    </div>
    {#if view.truncated}
      <p class="note">truncated — <a href={rawUrl} target="_blank" rel="noreferrer">open raw</a></p>
    {/if}
  {/if}
</Overlay>

<style>
  .media {
    display: block;
    max-width: 100%;
    max-height: 68vh;
    margin: 0 auto;
    border-radius: var(--r-sm);
  }
  .pdf {
    width: 100%;
    height: 68vh;
    border: 1px solid var(--border);
    border-radius: var(--r-sm);
    background: var(--surface-2);
  }
  .audio {
    width: 100%;
  }
  .code {
    font-family: var(--font-mono);
    font-size: var(--text-sm);
    line-height: 1.5;
    background: var(--code-bg);
    border: 1px solid var(--border);
    border-radius: var(--r-md);
    padding: 0.5rem 0;
    overflow-x: auto;
  }
  .ln {
    display: flex;
    min-width: max-content;
    padding: 0 0.75rem;
  }
  .no {
    flex: none;
    width: 3em;
    text-align: right;
    padding-right: 1em;
    color: var(--ink-faint);
    user-select: none;
  }
  .tx {
    white-space: pre;
  }
  .note {
    margin: 0.6rem 0 0;
    font-size: var(--text-sm);
    color: var(--ink-muted);
  }
</style>
