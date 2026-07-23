<script lang="ts">
  // App settings: theme, the two updaters (pi-web itself and the installed
  // pi), and client preferences. Update state is fetched fresh on open.
  import Overlay from "./Overlay.svelte";
  import Toggle from "./Toggle.svelte";
  import { api, type PiStatus, type UpdateStatus } from "../lib/api";
  import { appSettings } from "../lib/app-settings.svelte";
  import { prefs } from "../lib/prefs.svelte";
  import { theme, type ThemeMode } from "../lib/theme.svelte";
  import { toasts } from "../lib/toasts.svelte";

  let { onClose }: { onClose: () => void } = $props();

  let appVersion = $state("…");
  let update = $state<UpdateStatus | null>(null);
  let pi = $state<PiStatus | null>(null);
  let busy = $state<string | null>(null);

  $effect(() => {
    api.version().then((v) => (appVersion = v.version)).catch(() => (appVersion = "?"));
    api.update().then((u) => (update = u)).catch(() => {});
    api.pi().then((p) => (pi = p)).catch(() => {});
  });

  async function act(name: string, fn: () => Promise<void>): Promise<void> {
    busy = name;
    try {
      await fn();
    } catch (err) {
      toasts.error(err instanceof Error ? err.message : String(err));
    } finally {
      busy = null;
    }
  }

  const themes: { mode: ThemeMode; label: string }[] = [
    { mode: "system", label: "system" },
    { mode: "light", label: "day" },
    { mode: "dark", label: "night" },
  ];
</script>

<Overlay title="settings" {onClose}>
  <section>
    <h3 class="label">theme</h3>
    <div class="seg" role="radiogroup" aria-label="Theme">
      {#each themes as t (t.mode)}
        <button
          type="button"
          role="radio"
          aria-checked={theme.mode === t.mode}
          class:on={theme.mode === t.mode}
          onclick={() => theme.set(t.mode)}
        >
          {t.label}
        </button>
      {/each}
    </div>
  </section>

  <section>
    <h3 class="label">pi-web</h3>
    <div class="row">
      <span class="ver">v{appVersion}</span>
      {#if update?.available}
        <span class="avail">→ {update.latest} available</span>
      {:else if update?.checkedAt}
        <span class="dim">up to date</span>
      {/if}
    </div>
    {#if update?.error}
      <p class="err">{update.error}</p>
    {/if}
    <div class="row">
      <button
        type="button"
        class="btn"
        disabled={busy !== null}
        onclick={() => act("check", async () => void (update = await api.checkUpdate()))}
      >
        {busy === "check" ? "checking…" : "check"}
      </button>
      {#if update?.available && update?.canUpdate}
        <button
          type="button"
          class="btn primary"
          disabled={busy !== null}
          onclick={() =>
            act("apply", async () => {
              update = await api.applyUpdate();
              toasts.show("Updating — pi-web restarts in a moment.");
            })}
        >
          {busy === "apply" ? "updating…" : "update & restart"}
        </button>
      {/if}
      <Toggle
        label="auto-update"
        checked={!!update?.autoUpdate}
        onChange={(on) => act("auto", async () => void (update = await api.setAutoUpdate(on)))}
      />
    </div>
  </section>

  <section>
    <h3 class="label">pi (coding agent)</h3>
    <div class="row">
      <span class="ver">v{pi?.current ?? "…"}</span>
      {#if pi?.available}
        <span class="avail">→ {pi.latest} available</span>
      {:else if pi?.latest}
        <span class="dim">up to date</span>
      {/if}
    </div>
    {#if pi?.approveSupported === false}
      <p class="err">installed pi predates --approve; sessions run without it</p>
    {/if}
    {#if pi?.error}
      <p class="err">{pi.error}</p>
    {/if}
    <div class="row">
      <button
        type="button"
        class="btn"
        disabled={busy !== null}
        onclick={() => act("picheck", async () => void (pi = await api.checkPi()))}
      >
        {busy === "picheck" ? "checking…" : "check"}
      </button>
      {#if pi?.available}
        <button
          type="button"
          class="btn primary"
          disabled={busy !== null}
          onclick={() =>
            act("piupdate", async () => {
              pi = await api.updatePi();
              toasts.show("pi upgraded — idle sessions moved to the new binary.");
            })}
        >
          {busy === "piupdate" ? "upgrading…" : "upgrade pi"}
        </button>
      {/if}
      <Toggle
        label="auto-upgrade"
        checked={!!(pi?.autoUpdate ?? pi?.autoUpdatePi)}
        onChange={(on) => act("piauto", async () => void (pi = await api.setAutoPi(on)))}
      />
    </div>
  </section>

  <section>
    <h3 class="label">conversation</h3>
    <Toggle
      label="collapse thinking by default"
      checked={appSettings.collapseThinking}
      onChange={(on) => act("thinking", () => appSettings.setCollapseThinking(on))}
    />
  </section>

  <section>
    <h3 class="label">sound</h3>
    <Toggle
      label="ping when a turn settles in a background tab"
      checked={prefs.settleSound}
      onChange={(on) => prefs.setSettleSound(on)}
    />
  </section>
</Overlay>

<style>
  section {
    margin-bottom: 1.4rem;
  }
  section:last-child {
    margin-bottom: 0;
  }
  h3 {
    margin: 0 0 0.5rem;
  }
  .seg {
    display: inline-flex;
    border: 1px solid var(--border-strong);
    border-radius: var(--r-sm);
    overflow: hidden;
  }
  .seg button {
    padding: 0.3rem 0.9rem;
    font-size: var(--text-sm);
    color: var(--ink-muted);
  }
  .seg button + button {
    border-left: 1px solid var(--border);
  }
  .seg button.on {
    background: var(--ink);
    color: var(--surface);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 0.9rem;
    margin-bottom: 0.5rem;
    flex-wrap: wrap;
  }
  .ver {
    font-weight: 600;
  }
  .avail {
    color: var(--live-ink);
    font-size: var(--text-sm);
  }
  .dim {
    color: var(--ink-faint);
    font-size: var(--text-sm);
  }
  .err {
    margin: 0 0 0.5rem;
    font-size: var(--text-sm);
    color: var(--err);
  }
  .btn {
    padding: 0.25rem 0.7rem;
    font-size: var(--text-sm);
    border: 1px solid var(--border-strong);
    border-radius: var(--r-sm);
    color: var(--ink-muted);
  }
  .btn:hover:not(:disabled) {
    color: var(--ink);
  }
  .btn.primary {
    background: var(--ink);
    border-color: var(--ink);
    color: var(--surface);
  }
  .btn.primary:hover:not(:disabled) {
    background: var(--live-ink);
  }
  .btn:disabled {
    opacity: 0.5;
  }
</style>
