<script lang="ts">
  // The operator's input. Enter sends (IME-safe), Shift+Enter breaks a line,
  // `!` runs shell in the session cwd, `@` opens the fuzzy file finder, `/`
  // at the start opens the slash-command autocomplete, images paste/drop in
  // as chips, drafts persist per session. The state bar and the send/stop
  // buttons live in a footer strip inside the same box.
  import CommandPopover from "./CommandPopover.svelte";
  import FinderPopover from "./FinderPopover.svelte";
  import StateBar from "./StateBar.svelte";
  import { api, type ImagePart } from "../lib/api";
  import { NEW_SESSION, clearDraft, loadDraft, saveDraft } from "../lib/drafts";
  import { rail } from "../lib/rail.svelte";
  import { router } from "../lib/router.svelte";
  import { session } from "../lib/session.svelte";
  import { toasts } from "../lib/toasts.svelte";

  const view = $derived(session.view);

  interface ImageChip {
    dataUrl: string;
    mimeType: string;
  }

  let text = $state("");
  let images = $state<ImageChip[]>([]);
  let sending = $state(false);
  let dragOver = $state(false);
  let textarea = $state<HTMLTextAreaElement | null>(null);
  let finder = $state<ReturnType<typeof FinderPopover> | null>(null);
  let commandList = $state<ReturnType<typeof CommandPopover> | null>(null);

  // finder state: the "@token" under the caret
  let finderOpen = $state(false);
  let finderQuery = $state("");
  let atStart = -1;

  // command state: the "/name" token opening the message
  let commandOpen = $state(false);
  let commandQuery = $state("");

  const draftKey = $derived(session.pending ? NEW_SESSION : view.id || "");

  // load the draft when the session changes; save-through on edit below
  let lastDraftKey: string | null = null;
  $effect(() => {
    if (draftKey === lastDraftKey) return;
    lastDraftKey = draftKey;
    text = draftKey ? loadDraft(draftKey) : "";
    images = [];
    closeFinder();
    resize();
  });

  $effect(() => {
    session.onSetEditorText = (t: string) => {
      text = t;
      resize();
      textarea?.focus();
    };
    return () => {
      session.onSetEditorText = null;
    };
  });

  let saveTimer: number | undefined;
  function onInput(): void {
    resize();
    syncFinder();
    clearTimeout(saveTimer);
    const key = draftKey;
    saveTimer = window.setTimeout(() => key && saveDraft(key, text), 300);
  }

  function resize(): void {
    if (!textarea) return;
    textarea.style.height = "0";
    textarea.style.height = `${Math.min(textarea.scrollHeight, 320)}px`;
  }

  // --- @ finder and / commands ----------------------------------------------

  function syncFinder(): void {
    if (!textarea) return;
    const caret = textarea.selectionStart ?? text.length;
    const upto = text.slice(0, caret);
    // a leading / opens the command list while the caret stays inside the
    // command token; commands come from the live session's pi, so a pending
    // session has none to offer yet
    if (session.id && text.startsWith("/") && caret > 0 && !/\s/.test(upto)) {
      commandQuery = upto.slice(1);
      commandOpen = true;
      finderOpen = false;
      finderQuery = "";
      atStart = -1;
      return;
    }
    commandOpen = false;
    commandQuery = "";
    const at = upto.lastIndexOf("@");
    // an @ opens the finder when it starts a token (start of text or after
    // whitespace) and the query since it contains no whitespace
    if (at >= 0 && (at === 0 || /\s/.test(upto[at - 1]))) {
      const q = upto.slice(at + 1);
      if (!/\s/.test(q)) {
        atStart = at;
        finderQuery = q;
        finderOpen = true;
        return;
      }
    }
    closeFinder();
  }

  function closeFinder(): void {
    finderOpen = false;
    finderQuery = "";
    atStart = -1;
    commandOpen = false;
    commandQuery = "";
  }

  function pickFile(path: string): void {
    if (!textarea || atStart < 0) return;
    const caret = textarea.selectionStart ?? text.length;
    text = text.slice(0, atStart) + path + " " + text.slice(caret);
    const pos = atStart + path.length + 1;
    closeFinder();
    queueMicrotask(() => {
      textarea?.focus();
      textarea?.setSelectionRange(pos, pos);
      onInput();
    });
  }

  function pickCommand(name: string): void {
    if (!textarea) return;
    const caret = textarea.selectionStart ?? text.length;
    text = "/" + name + " " + text.slice(caret);
    const pos = name.length + 2;
    closeFinder();
    queueMicrotask(() => {
      textarea?.focus();
      textarea?.setSelectionRange(pos, pos);
      onInput();
    });
  }

  // --- images ---------------------------------------------------------------

  function addImageFile(file: File): void {
    if (!file.type.startsWith("image/")) return;
    const reader = new FileReader();
    reader.onload = () => {
      if (typeof reader.result === "string") {
        images.push({ dataUrl: reader.result, mimeType: file.type });
      }
    };
    reader.readAsDataURL(file);
  }

  function onPaste(e: ClipboardEvent): void {
    const files = Array.from(e.clipboardData?.items ?? [])
      .filter((i) => i.kind === "file")
      .map((i) => i.getAsFile())
      .filter((f): f is File => !!f);
    if (files.length > 0) {
      e.preventDefault();
      files.forEach(addImageFile);
    }
  }

  function onDrop(e: DragEvent): void {
    e.preventDefault();
    dragOver = false;
    Array.from(e.dataTransfer?.files ?? []).forEach(addImageFile);
  }

  // --- sending --------------------------------------------------------------

  function toImageParts(): ImagePart[] {
    return images.map((img) => ({
      data: img.dataUrl.slice(img.dataUrl.indexOf(",") + 1),
      mimeType: img.mimeType,
    }));
  }

  async function submit(): Promise<void> {
    const trimmed = text.trim();
    if (sending || (!trimmed && images.length === 0)) return;

    // `!command` runs operator shell in the session cwd (live sessions only)
    if (trimmed.startsWith("!")) {
      const command = trimmed.slice(1).trim();
      if (!command) return;
      if (!session.id) {
        toasts.error("Shell commands need a session — send a first message to create one.");
        return;
      }
      sending = true;
      try {
        clearComposer();
        await api.bash(session.id, command);
      } catch (err) {
        toasts.error(err instanceof Error ? err.message : String(err));
      } finally {
        sending = false;
      }
      return;
    }

    sending = true;
    const wasPending = !!session.pending;
    const parts = toImageParts();
    try {
      const id = await session.send(trimmed, parts);
      if (id !== null) {
        clearComposer();
        if (wasPending) {
          router.openSession(id);
          rail.refresh();
        }
      }
    } finally {
      sending = false;
    }
  }

  function clearComposer(): void {
    if (draftKey) clearDraft(draftKey);
    text = "";
    images = [];
    closeFinder();
    resize();
  }

  function stop(): void {
    if (!session.id) return;
    api.abort(session.id).catch((err) => toasts.error(err instanceof Error ? err.message : String(err)));
  }

  export function focus(): void {
    textarea?.focus();
  }

  export function insertQuote(quoted: string): void {
    const block = quoted
      .split("\n")
      .map((line) => "> " + line)
      .join("\n");
    text = (text.trim() ? text.trimEnd() + "\n\n" : "") + block + "\n\n";
    queueMicrotask(() => {
      textarea?.focus();
      textarea?.setSelectionRange(text.length, text.length);
      onInput();
    });
  }

  export function abortTurn(): void {
    if (view.streaming) stop();
  }

  function onKeydown(e: KeyboardEvent): void {
    // IME (Vietnamese/CJK) commits must never send early
    if (e.isComposing || e.keyCode === 229) return;
    if (finderOpen && finder?.handleKey(e)) {
      e.preventDefault();
      return;
    }
    if (commandOpen && commandList?.handleKey(e)) {
      e.preventDefault();
      return;
    }
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      submit();
    }
  }
</script>

<div
  class="composer"
  class:drag={dragOver}
  ondragover={(e) => {
    e.preventDefault();
    dragOver = true;
  }}
  ondragleave={() => (dragOver = false)}
  ondrop={onDrop}
  role="group"
  aria-label="Composer"
>
  {#each Object.entries(view.widgetsAbove) as [key, lines] (key)}
    <pre class="widget" title="extension widget: {key}">{lines.join("\n")}</pre>
  {/each}

  {#if finderOpen}
    <FinderPopover
      base={session.cwd}
      query={finderQuery}
      onPick={pickFile}
      onClose={closeFinder}
      bind:this={finder}
    />
  {/if}

  {#if commandOpen && session.id}
    <CommandPopover
      sessionId={session.id}
      query={commandQuery}
      onPick={pickCommand}
      onClose={closeFinder}
      bind:this={commandList}
    />
  {/if}

  {#if images.length > 0}
    <div class="chips">
      {#each images as img, i (i)}
        <span class="chip">
          <img src={img.dataUrl} alt="attachment {i + 1}" />
          <button type="button" aria-label="Remove image" onclick={() => images.splice(i, 1)}>✕</button>
        </span>
      {/each}
    </div>
  {/if}

  <div class="box">
    <div class="input-row">
      <span class="prompt" aria-hidden="true">{text.trimStart().startsWith("!") ? "!" : "❯"}</span>
      <textarea
        bind:this={textarea}
        bind:value={text}
        rows="1"
        placeholder={session.pending || session.id
          ? "message · /command · !shell · @file"
          : "pick or create a session to begin"}
        disabled={!session.pending && !session.id}
        oninput={onInput}
        onkeydown={onKeydown}
        onpaste={onPaste}
        onclick={syncFinder}
        onkeyup={(e) => {
          if (e.key.startsWith("Arrow")) syncFinder();
        }}
      ></textarea>
    </div>
    <div class="foot">
      <StateBar />
      {#if view.streaming}
        <button type="button" class="stop" onclick={stop} title="Abort the current turn (⌘.)">
          ■ stop
        </button>
      {/if}
      <button
        type="button"
        class="send"
        disabled={sending || (!text.trim() && images.length === 0) || (!session.pending && !session.id)}
        onclick={submit}
      >
        {view.streaming ? "steer" : "send"}
      </button>
    </div>
  </div>

  {#each Object.entries(view.widgetsBelow) as [key, lines] (key)}
    <pre class="widget" title="extension widget: {key}">{lines.join("\n")}</pre>
  {/each}
</div>

<style>
  .composer {
    position: relative;
    max-width: var(--measure-wide);
    margin: 0 auto;
    width: 100%;
    padding: 0.65rem 1.25rem 0.9rem;
  }
  .composer.drag .box {
    border-color: var(--live);
    background: var(--live-soft);
  }
  .widget {
    margin: 0 0 0.4rem;
    padding: 0.35rem 0.6rem;
    font-size: var(--text-xs);
    line-height: 1.45;
    color: var(--think);
    background: var(--think-soft);
    border-radius: var(--r-sm);
    white-space: pre-wrap;
  }
  .chips {
    display: flex;
    gap: 0.5rem;
    margin-bottom: 0.4rem;
    flex-wrap: wrap;
  }
  .chip {
    position: relative;
    display: inline-block;
  }
  .chip img {
    height: 52px;
    border-radius: var(--r-sm);
    border: 1px solid var(--border-strong);
    display: block;
  }
  .chip button {
    position: absolute;
    top: -6px;
    right: -6px;
    width: 16px;
    height: 16px;
    font-size: 9px;
    line-height: 1;
    color: var(--surface);
    background: var(--ink);
    border-radius: 50%;
  }
  .box {
    background: var(--surface);
    border: 1px solid var(--border-strong);
    border-radius: var(--r-lg);
  }
  .box:focus-within {
    border-color: var(--live);
  }
  .input-row {
    display: flex;
    align-items: flex-start;
    gap: 0.6rem;
    padding: 0.65rem 0.75rem 0.5rem 0.9rem;
  }
  .foot {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    padding: 0.3rem 0.55rem 0.3rem 0.9rem;
    border-top: 1px solid var(--border);
    background: var(--surface-2);
    /* inner radius tracks the box border so the tinted foot fills the corners */
    border-radius: 0 0 calc(var(--r-lg) - 1px) calc(var(--r-lg) - 1px);
  }
  .prompt {
    color: var(--live);
    font-weight: 600;
    line-height: 1.5;
  }
  textarea {
    flex: 1;
    min-width: 0;
    border: 0;
    background: none;
    resize: none;
    outline: none;
    font-family: var(--font-mono);
    font-size: var(--text-md);
    line-height: 1.5;
    min-height: 4.5em;
    max-height: 320px;
    padding: 0;
  }
  textarea::placeholder {
    color: var(--ink-faint);
  }
  .stop {
    flex: none;
    padding: 0.2rem 0.7rem;
    font-size: var(--text-xs);
    letter-spacing: 0.02em;
    color: var(--err);
    border: 1px solid var(--err);
    border-radius: var(--r-sm);
  }
  .stop:hover {
    background: var(--err-soft);
  }
  .send {
    flex: none;
    padding: 0.2rem 0.8rem;
    font-size: var(--text-xs);
    letter-spacing: var(--track);
    text-transform: uppercase;
    font-weight: 600;
    color: var(--surface);
    background: var(--ink);
    border-radius: var(--r-sm);
  }
  .send:hover:not(:disabled) {
    background: var(--live-ink);
  }
  .send:disabled {
    opacity: 0.4;
    cursor: default;
  }
</style>
