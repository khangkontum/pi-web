// Reactive wrapper around the pure feed reducer. Owns the EventSource and
// exposes a deeply-reactive SessionView the components render. Also owns the
// "pending" state: a session that will be created on first message, carrying
// the folder/model/thinking/name chosen in the new-session flow.

import { api, type ImagePart } from "./api";
import { applyEvent, applySnapshot, emptyView, type SessionView } from "./feed";
import { openSession } from "./sse";
import { prefs } from "./prefs.svelte";
import { settlePing } from "./sound";
import { toasts } from "./toasts.svelte";
import type { PiEvent } from "./protocol";

export interface NewSessionPreset {
  cwd: string | null;
  name?: string;
  provider?: string;
  modelId?: string;
  thinking?: string;
}

export class SessionStore {
  view = $state<SessionView>(emptyView());
  connected = $state(false);
  streamError = $state<string | null>(null);
  // set while composing a session that has not been created yet
  pending = $state<NewSessionPreset | null>(null);

  // Side-channels for events that land outside the feed.
  onExternalChange: (() => void) | null = null; // piweb_fork → refresh the rail
  onSetEditorText: ((text: string) => void) | null = null; // extension set_editor_text

  #es: EventSource | null = null;

  get id(): string {
    return this.view.id;
  }

  get cwd(): string | null {
    return this.pending ? this.pending.cwd : this.view.cwd;
  }

  open(id: string): void {
    if (this.#es && this.view.id === id) return;
    this.close();
    this.view = emptyView();
    this.view.id = id;
    this.streamError = null;
    this.pending = null;
    this.#es = openSession(id, {
      onSnapshot: (snap) => {
        this.view = applySnapshot(snap);
        this.connected = true;
        this.streamError = null;
      },
      onEvent: (ev) => this.#handleEvent(ev),
      onStreamError: (detail) => {
        this.streamError = detail;
      },
    });
  }

  #handleEvent(ev: PiEvent): void {
    const wasStreaming = this.view.streaming;
    applyEvent(this.view, ev);
    if (ev.type === "piweb_fork") this.onExternalChange?.();
    if (ev.type === "extension_ui_request") {
      // notify carries `message` as a plain string (unlike pi message events)
      const notifyText = (ev as { message?: unknown }).message;
      if (ev.method === "notify" && typeof notifyText === "string") {
        const level = ev.notifyType === "warning" || ev.notifyType === "error" ? ev.notifyType : "info";
        toasts.show(notifyText, level);
      } else if (ev.method === "set_editor_text" && typeof ev.text === "string") {
        this.onSetEditorText?.(ev.text);
      }
    }
    if (wasStreaming && !this.view.streaming && document.hidden && prefs.settleSound) {
      settlePing();
    }
  }

  // beginNew clears the feed for a not-yet-created session with its presets.
  beginNew(preset: NewSessionPreset): void {
    this.close();
    this.view = emptyView();
    this.view.cwd = preset.cwd;
    this.streamError = null;
    this.pending = preset;
  }

  // send delivers a message, creating the pending session first if needed.
  // Returns the session id (fresh for a pending session) or null on failure.
  async send(message: string, images?: ImagePart[]): Promise<string | null> {
    try {
      if (this.pending) {
        const p = this.pending;
        const { id } = await api.createSession({
          message,
          cwd: p.cwd ?? undefined,
          name: p.name || undefined,
          provider: p.provider || undefined,
          modelId: p.modelId || undefined,
          thinking: p.thinking || undefined,
        });
        // images can't ride along on create; deliver them right after
        if (images?.length) await api.message(id, "", images);
        return id;
      }
      await api.message(this.view.id, message, images);
      return this.view.id;
    } catch (err) {
      toasts.error(err instanceof Error ? err.message : String(err));
      return null;
    }
  }

  // reopen forces a fresh child/snapshot for the current session (after fork).
  reopen(): void {
    const id = this.view.id;
    if (!id) return;
    this.#es?.close();
    this.#es = null;
    this.open(id);
  }

  close(): void {
    this.#es?.close();
    this.#es = null;
    this.connected = false;
  }
}

export const session = new SessionStore();
