// Private-terminal client plumbing: SSE attach (mirroring sse.ts), base64
// decoding for PTY bytes, and an xterm theme assembled from the app's ANSI
// palette tokens so the terminal speaks the same two-voices design as the
// rest of the chrome. The terminal never touches the pi session stream —
// that separation is the whole feature.

export interface TerminalHandlers {
  onSnapshot: (data: Uint8Array) => void;
  onOutput: (data: Uint8Array) => void;
  onExit: (code: number) => void;
}

export function openTerminal(id: string, handlers: TerminalHandlers): EventSource {
  const es = new EventSource(`/api/terminals/${encodeURIComponent(id)}/stream`);

  es.addEventListener("snapshot", (msg) => {
    try {
      handlers.onSnapshot(base64ToBytes(JSON.parse((msg as MessageEvent).data)));
    } catch {
      /* ignore malformed frame */
    }
  });

  es.addEventListener("output", (msg) => {
    try {
      handlers.onOutput(base64ToBytes(JSON.parse((msg as MessageEvent).data)));
    } catch {
      /* ignore malformed frame */
    }
  });

  es.addEventListener("exit", (msg) => {
    try {
      const { code } = JSON.parse((msg as MessageEvent).data);
      handlers.onExit(typeof code === "number" ? code : -1);
    } catch {
      handlers.onExit(-1);
    }
    es.close();
  });

  return es;
}

export function base64ToBytes(b64: string): Uint8Array {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

// xtermTheme reads the resolved --ansi-* / ink / code-bg tokens at call time;
// callers re-invoke it when data-theme flips.
export function xtermTheme(): Record<string, string> {
  const style = getComputedStyle(document.documentElement);
  const v = (name: string) => style.getPropertyValue(name).trim();
  return {
    foreground: v("--ink"),
    background: v("--code-bg"),
    cursor: v("--ink"),
    cursorAccent: v("--code-bg"),
    selectionBackground: v("--surface-3"),
    black: v("--ansi-0"),
    red: v("--ansi-1"),
    green: v("--ansi-2"),
    yellow: v("--ansi-3"),
    blue: v("--ansi-4"),
    magenta: v("--ansi-5"),
    cyan: v("--ansi-6"),
    white: v("--ansi-7"),
    brightBlack: v("--ansi-8"),
    brightRed: v("--ansi-9"),
    brightGreen: v("--ansi-10"),
    brightYellow: v("--ansi-11"),
    brightBlue: v("--ansi-12"),
    brightMagenta: v("--ansi-13"),
    brightCyan: v("--ansi-14"),
    brightWhite: v("--ansi-15"),
  };
}
