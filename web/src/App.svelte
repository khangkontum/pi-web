<script lang="ts">
  // The desk: rail | (banner / header / feed+explorer / state bar / composer),
  // with the spine living inside the feed. Routing decides the open session;
  // overlays are app-local state.
  import Composer from "./components/Composer.svelte";
  import Feed from "./components/Feed.svelte";
  import FilePreview from "./components/FilePreview.svelte";
  import FilesPane from "./components/FilesPane.svelte";
  import HelpPopover from "./components/HelpPopover.svelte";
  import NewSessionOverlay from "./components/NewSessionOverlay.svelte";
  import PiBanner from "./components/PiBanner.svelte";
  import Rail from "./components/Rail.svelte";
  import SessionHeader from "./components/SessionHeader.svelte";
  import SettingsPanel from "./components/SettingsPanel.svelte";
  import StateBar from "./components/StateBar.svelte";
  import Toasts from "./components/Toasts.svelte";
  import { rail } from "./lib/rail.svelte";
  import { router } from "./lib/router.svelte";
  import { session } from "./lib/session.svelte";
  import { matchChord } from "./lib/shortcuts";

  let railOpen = $state(true);
  let drawerOpen = $state(false);
  let explorerOpen = $state(false);
  let newOpen = $state(false);
  let settingsOpen = $state(false);
  let helpOpen = $state(false);
  let preview = $state<{ path: string; base: string | null } | null>(null);

  let composer = $state<ReturnType<typeof Composer> | null>(null);

  $effect(() => {
    rail.start();
    session.onExternalChange = () => rail.refresh();
    return () => {
      rail.stop();
      session.close();
    };
  });

  // the route owns which session is open
  $effect(() => {
    const r = router.route;
    if (r.kind === "session") {
      session.open(r.id);
      drawerOpen = false;
    } else if (r.kind === "new") {
      newOpen = true;
    }
  });

  // extension setTitle wins; otherwise the session names the tab
  $effect(() => {
    const v = session.view;
    document.title = v.title || (v.sessionName ? `${v.sessionName} — pi-web` : "pi-web");
  });

  function openFile(path: string): void {
    preview = { path, base: session.cwd };
  }

  function anyOverlayOpen(): boolean {
    return newOpen || settingsOpen || helpOpen || preview !== null;
  }

  function onKeydown(e: KeyboardEvent): void {
    const chord = matchChord(e);
    if (chord) {
      e.preventDefault();
      if (chord === "new") newOpen = true;
      else if (chord === "rail") {
        railOpen = !railOpen;
        drawerOpen = !drawerOpen;
      } else if (chord === "explorer") explorerOpen = !explorerOpen;
      else if (chord === "settings") settingsOpen = !settingsOpen;
      else if (chord === "stop") composer?.abortTurn();
      return;
    }
    const target = e.target as HTMLElement;
    const typing = target.tagName === "TEXTAREA" || target.tagName === "INPUT";
    if (e.key === "?" && !typing && !anyOverlayOpen()) {
      e.preventDefault();
      helpOpen = true;
    }
  }
</script>

<svelte:window onkeydown={onKeydown} />

<div class="app">
  <div class="rail-slot" class:hidden={!railOpen} class:drawer-open={drawerOpen}>
    <Rail
      onNew={() => (newOpen = true)}
      onNewIn={(cwd) => {
        session.beginNew({ cwd });
        router.home();
        drawerOpen = false;
        composer?.focus();
      }}
    />
  </div>
  {#if drawerOpen}
    <button type="button" class="drawer-scrim" aria-label="Close rail" onclick={() => (drawerOpen = false)}
    ></button>
  {/if}

  <div class="main">
    <PiBanner />
    <SessionHeader
      onToggleRail={() => (drawerOpen = !drawerOpen)}
      onToggleExplorer={() => (explorerOpen = !explorerOpen)}
      onSettings={() => (settingsOpen = true)}
      onHelp={() => (helpOpen = true)}
    />
    <div class="content">
      <Feed onOpenFile={openFile} />
      {#if explorerOpen && session.cwd}
        <FilesPane root={session.cwd} onOpenFile={openFile} onClose={() => (explorerOpen = false)} />
      {/if}
    </div>
    <div class="dock">
      <StateBar />
      <Composer bind:this={composer} />
    </div>
  </div>
</div>

<Toasts />

{#if newOpen}
  <NewSessionOverlay onClose={() => (newOpen = false)} />
{/if}
{#if settingsOpen}
  <SettingsPanel onClose={() => (settingsOpen = false)} />
{/if}
{#if helpOpen}
  <HelpPopover onClose={() => (helpOpen = false)} />
{/if}
{#if preview}
  <FilePreview path={preview.path} base={preview.base} onClose={() => (preview = null)} />
{/if}

<style>
  .app {
    display: flex;
    height: 100%;
  }
  .rail-slot {
    width: var(--rail-w);
    flex: none;
    height: 100%;
  }
  .rail-slot.hidden {
    display: none;
  }
  .main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    height: 100%;
  }
  .content {
    flex: 1;
    min-height: 0;
    display: flex;
  }
  .content > :global(.feed) {
    flex: 1;
    min-width: 0;
  }
  .dock {
    border-top: 1px solid var(--border);
    background: var(--bg);
  }
  .drawer-scrim {
    display: none;
  }

  @media (max-width: 900px) {
    .rail-slot,
    .rail-slot.hidden {
      display: block;
      position: fixed;
      z-index: 50;
      top: 0;
      bottom: 0;
      left: 0;
      transform: translateX(-100%);
      transition: transform 160ms ease;
      box-shadow: none;
    }
    .rail-slot.drawer-open {
      transform: translateX(0);
      box-shadow: var(--shadow);
    }
    .drawer-scrim {
      display: block;
      position: fixed;
      inset: 0;
      z-index: 49;
      background: var(--scrim);
      border: 0;
    }
  }
</style>
