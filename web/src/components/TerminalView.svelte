<script lang="ts">
  // One xterm instance attached to one private terminal. Output arrives over
  // SSE (EventSource auto-reconnects; the server replays the dtach scrollback
  // as a snapshot, so reconnects and reopens repaint cleanly). Input and
  // resize go out as POSTs. xterm renders into its own canvas/DOM — nothing
  // here goes near innerHTML.
  import { FitAddon } from "@xterm/addon-fit";
  import { Terminal } from "@xterm/xterm";
  import "@xterm/xterm/css/xterm.css";
  import { api } from "../lib/api";
  import { openTerminal, xtermTheme } from "../lib/terminal";

  let {
    id,
    active,
    onExit,
  }: {
    id: string;
    active: boolean;
    onExit: (code: number) => void;
  } = $props();

  let host = $state<HTMLElement | null>(null);
  let term: Terminal | null = null;
  let fit: FitAddon | null = null;
  let lastCols = 0;
  let lastRows = 0;

  function sendSize(): void {
    if (!term) return;
    if (term.cols === lastCols && term.rows === lastRows) return;
    lastCols = term.cols;
    lastRows = term.rows;
    api.terminalResize(id, term.cols, term.rows).catch(() => {
      /* terminal may have exited */
    });
  }

  $effect(() => {
    if (!host) return;
    term = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: getComputedStyle(document.documentElement).getPropertyValue("--font-mono"),
      scrollback: 10000,
      theme: xtermTheme(),
    });
    fit = new FitAddon();
    term.loadAddon(fit);
    term.open(host);
    fit.fit();

    term.onData((data) => {
      api.terminalInput(id, data).catch(() => {
        /* terminal may have exited */
      });
    });

    const es = openTerminal(id, {
      onSnapshot: (data) => {
        term?.reset();
        term?.write(data);
      },
      onOutput: (data) => term?.write(data),
      onExit: (code) => {
        term?.write(`\r\n\x1b[2m[exited with code ${code}]\x1b[0m\r\n`);
        onExit(code);
      },
    });

    const ro = new ResizeObserver(() => {
      fit?.fit();
      sendSize();
    });
    ro.observe(host);
    sendSize();

    // keep the palette in step with light/dark flips
    const mo = new MutationObserver(() => {
      if (term) term.options.theme = xtermTheme();
    });
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });

    return () => {
      mo.disconnect();
      ro.disconnect();
      es.close();
      term?.dispose();
      term = null;
      fit = null;
    };
  });

  $effect(() => {
    if (active && term) {
      fit?.fit();
      sendSize();
      term.focus();
    }
  });
</script>

<div class="term" class:hidden={!active} bind:this={host}></div>

<style>
  .term {
    height: 100%;
    padding: 0.35rem 0.5rem 0.2rem;
    background: var(--code-bg);
  }
  .term.hidden {
    display: none;
  }
  .term :global(.xterm) {
    height: 100%;
  }
</style>
