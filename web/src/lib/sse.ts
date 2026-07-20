// EventSource lifecycle for a session's SSE stream. The server sends a
// `snapshot` event with full state first, then `pi` events; the browser's
// EventSource auto-reconnects on drop and the server re-snapshots, so the UI
// survives reconnects invisibly.

import type { PiEvent, Snapshot } from "./protocol";

export interface StreamHandlers {
  onSnapshot: (snap: Snapshot) => void;
  onEvent: (ev: PiEvent) => void;
  onStreamError?: (detail: string) => void;
}

export function openSession(id: string, handlers: StreamHandlers): EventSource {
  const es = new EventSource(`/api/sessions/${encodeURIComponent(id)}/events`);

  es.addEventListener("snapshot", (msg) => {
    try {
      handlers.onSnapshot(JSON.parse((msg as MessageEvent).data));
    } catch {
      /* ignore malformed frame */
    }
  });

  es.addEventListener("pi", (msg) => {
    try {
      handlers.onEvent(JSON.parse((msg as MessageEvent).data));
    } catch {
      /* ignore malformed frame */
    }
  });

  es.addEventListener("error", (msg) => {
    // server-sent "error" frames carry data; transport errors do not. A
    // CONNECTING readyState is a normal auto-reconnect and stays silent; a
    // CLOSED one is fatal (e.g. the session failed to open) and must not
    // leave the operator staring at silence.
    const data = (msg as MessageEvent).data;
    if (data) {
      handlers.onStreamError?.(String(data));
    } else if (es.readyState === EventSource.CLOSED) {
      handlers.onStreamError?.("event stream closed — the session could not be opened");
    }
  });

  return es;
}
