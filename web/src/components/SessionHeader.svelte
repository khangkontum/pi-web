<script lang="ts" module>
  // model list is shared app-wide; `pi --list-models` is slow so the server
  // caches it and we fetch once
  let modelsPromise: Promise<ModelInfo[]> | null = null;
  function loadModels(): Promise<ModelInfo[]> {
    modelsPromise ??= api.models().then((r) => r.models ?? []);
    return modelsPromise;
  }
</script>

<script lang="ts">
  // Session chrome: identity on the left (name, cwd, run state), the model
  // and thinking pickers plus app buttons on the right.
  import Dropdown from "./Dropdown.svelte";
  import { api, type ModelInfo } from "../lib/api";
  import { session } from "../lib/session.svelte";
  import { toasts } from "../lib/toasts.svelte";

  let {
    onToggleRail,
    onToggleExplorer,
    onSettings,
    onHelp,
  }: {
    onToggleRail: () => void;
    onToggleExplorer: () => void;
    onSettings: () => void;
    onHelp: () => void;
  } = $props();

  const view = $derived(session.view);

  let models = $state<ModelInfo[]>([]);
  $effect(() => {
    loadModels()
      .then((m) => (models = m))
      .catch(() => {
        /* models stay empty; picker shows nothing to pick */
      });
  });

  const modelItems = $derived(
    models.map((m) => ({
      value: `${m.provider} ${m.model}`,
      label: m.model,
      hint: m.provider,
    })),
  );

  const currentModelKey = $derived.by(() => {
    if (session.pending) {
      const p = session.pending;
      return p.provider && p.modelId ? `${p.provider} ${p.modelId}` : null;
    }
    return view.model?.provider && view.model?.id ? `${view.model.provider} ${view.model.id}` : null;
  });

  const modelLabel = $derived.by(() => {
    if (session.pending) return session.pending.modelId ?? "default model";
    return view.model?.id ?? "model";
  });

  const THINKING = ["off", "minimal", "low", "medium", "high", "xhigh"];
  const thinkingItems = THINKING.map((l) => ({ value: l, label: l }));
  const thinkingLevel = $derived(
    session.pending ? (session.pending.thinking ?? "off") : view.thinkingLevel,
  );

  async function pickModel(key: string): Promise<void> {
    // key is "<provider> <modelId>"; the id may itself contain spaces
    const sp = key.indexOf(" ");
    const provider = key.slice(0, sp);
    const modelId = key.slice(sp + 1);
    if (session.pending) {
      session.pending.provider = provider;
      session.pending.modelId = modelId;
      return;
    }
    try {
      await api.setModel(session.id, provider, modelId);
      view.model = { provider, id: modelId };
    } catch (err) {
      toasts.error(err instanceof Error ? err.message : String(err));
    }
  }

  async function pickThinking(level: string): Promise<void> {
    if (session.pending) {
      session.pending.thinking = level;
      return;
    }
    try {
      await api.setThinking(session.id, level);
      view.thinkingLevel = level;
    } catch (err) {
      toasts.error(err instanceof Error ? err.message : String(err));
    }
  }

  const name = $derived(
    view.sessionName || (session.pending ? "new session" : session.id ? session.id.slice(0, 8) : "pi-web"),
  );
  const cwd = $derived(session.cwd ?? "");
</script>

<header class="header">
  <button type="button" class="icon rail-toggle" title="Toggle rail (⌘B)" onclick={onToggleRail}>
    ☰
  </button>

  <div class="who">
    <span class="name">{name}</span>
    {#if cwd}<span class="cwd" title={cwd}>{cwd}</span>{/if}
  </div>

  {#if view.streaming}
    <span class="run"><span class="pulse"></span></span>
  {/if}

  <div class="tools">
    {#if session.id || session.pending}
      <Dropdown
        label="Model"
        items={modelItems}
        value={currentModelKey}
        buttonText={modelLabel}
        align="right"
        onSelect={pickModel}
      />
      <Dropdown
        label="Thinking level"
        items={thinkingItems}
        value={thinkingLevel}
        buttonText={`think ${thinkingLevel}`}
        align="right"
        onSelect={pickThinking}
      />
    {/if}
    {#if session.id || session.pending}
      <button type="button" class="icon" title="File explorer (⌘E)" onclick={onToggleExplorer}>
        ⌸
      </button>
    {/if}
    <button type="button" class="icon" title="Keyboard shortcuts (?)" onclick={onHelp}>?</button>
    <button type="button" class="icon" title="Settings (⌘,)" onclick={onSettings}>⚙</button>
  </div>
</header>

<style>
  .header {
    display: flex;
    align-items: center;
    gap: 0.8rem;
    padding: 0.5rem 1rem;
    border-bottom: 1px solid var(--border);
    background: var(--surface);
    min-height: 2.6rem;
  }
  .rail-toggle {
    display: none;
  }
  @media (max-width: 900px) {
    .rail-toggle {
      display: inline-flex;
    }
  }
  .who {
    display: flex;
    align-items: baseline;
    gap: 0.7em;
    min-width: 0;
  }
  .name {
    font-weight: 600;
    font-size: var(--text-md);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 16rem;
  }
  .cwd {
    font-size: var(--text-xs);
    color: var(--ink-faint);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    direction: rtl;
    text-align: left;
    max-width: 22rem;
  }
  .run {
    flex: none;
  }
  .tools {
    margin-left: auto;
    display: flex;
    align-items: center;
    gap: 0.45rem;
  }
  .icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 1.7rem;
    height: 1.7rem;
    font-size: var(--text-md);
    color: var(--ink-muted);
    border: 1px solid transparent;
    border-radius: var(--r-sm);
  }
  .icon:hover {
    color: var(--ink);
    background: var(--surface-2);
  }
  @media (max-width: 700px) {
    .cwd {
      display: none;
    }
  }
</style>
