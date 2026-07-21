<script lang="ts">
  // File preview overlay: text via /api/file; images, PDF, and audio via
  // /api/raw. Anything else falls back to a raw link.
  import Overlay from "./Overlay.svelte";
  import DiffView from "./DiffView.svelte";
  import { api, type FileView } from "../lib/api";
  import { langForPath, tokenize, type TokenLine } from "../lib/highlight";
  import { parsePatch, type PatchFile } from "../lib/patch";

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
  // token lines are data; the template below is the only place they become DOM
  let tokens = $state<TokenLine[] | null>(null);

  // "diff" shows this file's working-tree patch; fetched lazily the first time
  // the toggle is used and reset when the previewed file changes.
  let mode = $state<"code" | "diff">("code");
  let diffFiles = $state<PatchFile[] | null>(null);
  let diffTruncated = $state(false);
  let diffError = $state<string | null>(null);

  $effect(() => {
    view = null;
    failed = null;
    mode = "code";
    diffFiles = null;
    diffError = null;
    if (kind !== "text") return;
    api
      .file(path, base)
      .then((v) => (view = v))
      .catch((err) => (failed = err instanceof Error ? err.message : String(err)));
  });

  $effect(() => {
    if (kind !== "text" || mode !== "diff" || diffFiles !== null) return;
    api
      .gitDiff(base, null, path)
      .then((d) => {
        diffFiles = parsePatch(d.patch);
        diffTruncated = d.truncated ?? false;
      })
      .catch((err) => (diffError = err instanceof Error ? err.message : String(err)));
  });

  $effect(() => {
    tokens = null;
    if (!view || view.binary) return;
    const content = view.content;
    tokenize(content, langForPath(path)).then((t) => {
      // drop a stale result if the popup moved on to another file
      if (view?.content === content) tokens = t;
    });
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
    <div class="modes" role="tablist">
      <button type="button" role="tab" class:active={mode === "code"} aria-selected={mode === "code"} onclick={() => (mode = "code")}>
        file
      </button>
      <button type="button" role="tab" class:active={mode === "diff"} aria-selected={mode === "diff"} onclick={() => (mode = "diff")}>
        diff
      </button>
    </div>
    {#if mode === "code"}
      <div class="code">
        {#each lines as line, i (i)}
          <div class="ln">
            <span class="no">{i + 1}</span>
            <span class="tx">
              {#if tokens?.[i]?.length}
                {#each tokens[i] as t}<span style:color={t.color}>{t.content}</span>{/each}
              {:else}
                {line || " "}
              {/if}
            </span>
          </div>
        {/each}
      </div>
      {#if view.truncated}
        <p class="note">truncated — <a href={rawUrl} target="_blank" rel="noreferrer">open raw</a></p>
      {/if}
    {:else if diffError}
      <p class="note">{diffError}</p>
    {:else if diffFiles === null}
      <p class="note">loading…</p>
    {:else if diffFiles.length === 0}
      <p class="note">no changes to this file</p>
    {:else}
      {#each diffFiles as f (f.path + (f.oldPath ?? ""))}
        {#if f.binary}
          <p class="note">binary file</p>
        {:else if f.rows.length > 0}
          <DiffView diff={{ path: f.path, rows: f.rows, adds: f.adds, dels: f.dels }} />
        {/if}
      {/each}
      {#if diffTruncated}
        <p class="note">patch truncated by the server</p>
      {/if}
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
  .modes {
    display: flex;
    gap: 0.4rem;
    margin-bottom: 0.8rem;
  }
  .modes button {
    font-family: var(--font-mono);
    font-size: var(--text-sm);
    letter-spacing: var(--track);
    text-transform: uppercase;
    color: var(--ink-muted);
    padding: 0.25rem 0.6rem;
    border: 1px solid var(--border);
    border-radius: var(--r-sm);
    background: transparent;
  }
  .modes button:hover {
    color: var(--ink);
    background: var(--surface-2);
  }
  .modes button.active {
    color: var(--live-ink);
    border-color: var(--live);
    background: var(--live-soft);
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
