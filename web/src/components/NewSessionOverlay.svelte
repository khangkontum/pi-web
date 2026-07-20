<script lang="ts">
  // New-session flow: browse to a folder (via /api/dirs), optionally preset
  // name/model/thinking. Nothing is created here — the session is born on the
  // first message.
  import Dropdown from "./Dropdown.svelte";
  import Overlay from "./Overlay.svelte";
  import { api, type DirListing, type ModelInfo } from "../lib/api";
  import { router } from "../lib/router.svelte";
  import { session } from "../lib/session.svelte";

  let { onClose }: { onClose: () => void } = $props();

  let listing = $state<DirListing | null>(null);
  let dirError = $state<string | null>(null);
  let name = $state("");
  let provider = $state("");
  let modelId = $state("");
  let thinking = $state("");

  let models = $state<ModelInfo[]>([]);
  $effect(() => {
    api
      .models()
      .then((r) => (models = r.models ?? []))
      .catch(() => {});
  });

  async function browse(path?: string | null): Promise<void> {
    try {
      listing = await api.dirs(path);
      dirError = null;
    } catch (err) {
      dirError = err instanceof Error ? err.message : String(err);
    }
  }

  $effect(() => {
    browse(session.cwd);
  });

  const modelItems = $derived([
    { value: "", label: "pi default", hint: "" },
    ...models.map((m) => ({ value: `${m.provider} ${m.model}`, label: m.model, hint: m.provider })),
  ]);

  const thinkingItems = [
    { value: "", label: "pi default" },
    ...["off", "minimal", "low", "medium", "high", "xhigh"].map((l) => ({ value: l, label: l })),
  ];

  function start(): void {
    session.beginNew({
      cwd: listing?.path ?? null,
      name: name.trim() || undefined,
      provider: provider || undefined,
      modelId: modelId || undefined,
      thinking: thinking || undefined,
    });
    router.home();
    onClose();
  }
</script>

<Overlay title="new session" {onClose}>
  <div class="field">
    <span class="label">folder</span>
    <div class="picker">
      <div class="path-row">
        <button
          type="button"
          class="up"
          disabled={!listing?.parent}
          title="Up one folder"
          onclick={() => browse(listing?.parent)}
        >
          ↑
        </button>
        <span class="path" title={listing?.path}>{listing?.path ?? "…"}</span>
      </div>
      {#if dirError}
        <div class="note">{dirError}</div>
      {:else}
        <div class="dirs">
          {#each listing?.dirs ?? [] as d (d)}
            <button type="button" class="dir" onclick={() => browse(`${listing?.path}/${d}`)}>
              {d}/
            </button>
          {/each}
          {#if (listing?.dirs ?? []).length === 0}
            <div class="note">no subfolders</div>
          {/if}
        </div>
      {/if}
    </div>
  </div>

  <label class="field">
    <span class="label">name <em>optional</em></span>
    <input type="text" bind:value={name} placeholder="what this session is for" />
  </label>

  <div class="field">
    <span class="label">model <em>optional</em></span>
    <Dropdown
      label="Model"
      items={modelItems}
      value={provider && modelId ? `${provider} ${modelId}` : ""}
      buttonText={modelId || "pi default"}
      onSelect={(key) => {
        if (!key) {
          provider = "";
          modelId = "";
          return;
        }
        const sp = key.indexOf(" ");
        provider = key.slice(0, sp);
        modelId = key.slice(sp + 1);
      }}
    />
  </div>

  <div class="field">
    <span class="label">thinking <em>optional</em></span>
    <Dropdown
      label="Thinking level"
      items={thinkingItems}
      value={thinking}
      buttonText={thinking || "pi default"}
      onSelect={(l) => (thinking = l)}
    />
  </div>

  <div class="actions">
    <button type="button" class="start" onclick={start}>
      start in {listing?.path?.split("/").pop() || "folder"}
    </button>
  </div>
</Overlay>

<style>
  .field {
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
    margin-bottom: 1rem;
  }
  .field em {
    font-style: normal;
    text-transform: none;
    letter-spacing: 0;
    color: var(--ink-faint);
  }
  .picker {
    border: 1px solid var(--border);
    border-radius: var(--r-md);
    overflow: hidden;
  }
  .path-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.4rem 0.6rem;
    border-bottom: 1px solid var(--border);
    background: var(--surface-2);
  }
  .up {
    padding: 0 0.4rem;
    border: 1px solid var(--border);
    border-radius: var(--r-sm);
    background: var(--surface);
  }
  .up:disabled {
    opacity: 0.4;
  }
  .path {
    font-size: var(--text-sm);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    direction: rtl;
    text-align: left;
  }
  .dirs {
    max-height: 200px;
    overflow-y: auto;
    padding: 0.3rem;
  }
  .dir {
    display: block;
    width: 100%;
    padding: 0.25rem 0.5rem;
    font-size: var(--text-sm);
    text-align: left;
    color: var(--ink-muted);
    border-radius: var(--r-sm);
  }
  .dir:hover {
    background: var(--accent-hover);
    color: var(--ink);
  }
  .note {
    padding: 0.4rem 0.6rem;
    font-size: var(--text-sm);
    color: var(--ink-faint);
  }
  input {
    padding: 0.45rem 0.6rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--r-sm);
    outline: none;
  }
  input:focus {
    border-color: var(--live);
  }
  .actions {
    display: flex;
    justify-content: flex-end;
  }
  .start {
    padding: 0.4rem 1rem;
    font-size: var(--text-sm);
    font-weight: 600;
    color: var(--surface);
    background: var(--ink);
    border-radius: var(--r-sm);
  }
  .start:hover {
    background: var(--live-ink);
  }
</style>
