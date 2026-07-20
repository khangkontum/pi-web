// Pure reducer over pi's snapshot + event stream. No Svelte, no DOM — this is
// the contract, and it is unit-tested directly. The Svelte layer wraps a
// SessionView in $state and forwards events here.
//
// Assistant messages carry inline blocks (text / thinking / toolCall); tool
// blocks are keyed by toolCallId so execution events update them in place;
// toolResult / tool_execution_end finish them. Unknown event types fall
// through — a newer pi must never break rendering.

import {
  contentText,
  toolPathOf,
  type Block,
  type Message,
  type ModelRef,
  type PiEvent,
  type Snapshot,
  type Stats,
} from "./protocol";

export type ToolStatus = "running" | "done" | "error";

export interface ToolRun {
  id: string;
  name: string;
  arguments?: Record<string, unknown>;
  path: string | null;
  output: string;
  status: ToolStatus;
}

export interface UserItem {
  kind: "user";
  ts?: number;
  text: string;
  imageCount: number;
}

export interface AssistantItem {
  kind: "assistant";
  ts?: number;
  blocks: Block[];
  streaming: boolean;
  error?: string;
  usage?: { input?: number; output?: number };
}

export interface BashItem {
  kind: "bash";
  command: string;
  output: string;
  exitCode: number;
}

// A tool that started without a preceding assistant toolCall block (defensive
// path). Normal tools render inline inside the assistant item.
export interface ToolItem {
  kind: "tool";
  id: string;
}

export interface CompactionItem {
  kind: "compaction";
  reason: string;
  running: boolean;
  summary?: string;
  tokensBefore?: number;
  tokensAfter?: number;
  aborted?: boolean;
  error?: string;
}

export interface NoticeItem {
  kind: "notice";
  level: "error" | "warn" | "info";
  title: string;
  detail?: string;
}

export type FeedItem = UserItem | AssistantItem | BashItem | ToolItem | CompactionItem | NoticeItem;

export interface RetryState {
  attempt: number;
  maxAttempts: number;
  delayMs: number;
  errorMessage?: string;
  // wall-clock ms when the event arrived; the countdown renders against this
  receivedAt: number;
}

export interface SessionView {
  id: string;
  cwd: string | null;
  sessionName: string;
  model: ModelRef | null;
  thinkingLevel: string;
  streaming: boolean;
  compacting: boolean;
  steeringMode: string;
  followUpMode: string;
  autoCompaction: boolean;
  stats: Stats | null;
  items: FeedItem[];
  tools: Record<string, ToolRun>;
  // index of the in-flight assistant item, or -1
  streamingIndex: number;
  queue: { steering: string[]; followUp: string[] };
  retry: RetryState | null;
  // extension passive UI: setStatus lines and setWidget strips, keyed
  statusLines: Record<string, string>;
  widgetsAbove: Record<string, string[]>;
  widgetsBelow: Record<string, string[]>;
  title: string | null;
}

export function emptyView(): SessionView {
  return {
    id: "",
    cwd: null,
    sessionName: "",
    model: null,
    thinkingLevel: "off",
    streaming: false,
    compacting: false,
    steeringMode: "all",
    followUpMode: "one-at-a-time",
    autoCompaction: true,
    stats: null,
    items: [],
    tools: {},
    streamingIndex: -1,
    queue: { steering: [], followUp: [] },
    retry: null,
    statusLines: {},
    widgetsAbove: {},
    widgetsBelow: {},
    title: null,
  };
}

function asBlocks(content: Message["content"]): Block[] {
  if (!Array.isArray(content)) {
    const text = contentText(content);
    return text ? [{ type: "text", text }] : [];
  }
  return content as Block[];
}

function countImages(content: Message["content"]): number {
  if (!Array.isArray(content)) return 0;
  return content.filter((b) => (b as { type?: string }).type === "image").length;
}

// registerTools ensures every toolCall block in an assistant message has a
// live tool entry (created "running"; finished later by toolResult / _end).
function registerTools(view: SessionView, blocks: Block[]): void {
  for (const b of blocks) {
    if (b.type === "toolCall" && b.id && !view.tools[b.id]) {
      view.tools[b.id] = {
        id: b.id,
        name: b.name,
        arguments: b.arguments,
        path: toolPathOf(b.arguments),
        output: "",
        status: "running",
      };
    }
  }
}

function finishTool(
  view: SessionView,
  id: string | undefined,
  text: string | null,
  isError?: boolean,
): void {
  if (!id) return;
  const tool = view.tools[id];
  if (!tool) return;
  if (text !== null) tool.output = text;
  tool.status = isError ? "error" : "done";
}

export function appendMessage(view: SessionView, message: Message): void {
  if (!message || !message.role) return;
  switch (message.role) {
    case "user": {
      const text = contentText(message.content);
      const last = view.items[view.items.length - 1];
      // pi may surface a user message via more than one event; consecutive
      // identical appends are the same message.
      if (last && last.kind === "user" && last.text === text && last.ts === message.timestamp) {
        return;
      }
      view.items.push({
        kind: "user",
        ts: message.timestamp,
        text,
        imageCount: countImages(message.content),
      });
      break;
    }
    case "assistant": {
      const blocks = asBlocks(message.content);
      registerTools(view, blocks);
      view.items.push({
        kind: "assistant",
        ts: message.timestamp,
        blocks,
        streaming: false,
        error: message.stopReason === "error" ? message.errorMessage : undefined,
        usage: message.usage,
      });
      break;
    }
    case "toolResult":
      finishTool(view, message.toolCallId, contentText(message.content), message.isError);
      break;
    case "bashExecution":
      view.items.push({
        kind: "bash",
        command: message.command ?? "",
        output: message.output ?? "",
        exitCode: message.exitCode ?? 0,
      });
      break;
    default:
      break;
  }
}

export function applySnapshot(snap: Snapshot): SessionView {
  const next = emptyView();
  next.id = snap.id ?? "";
  next.cwd = snap.cwd ?? null;
  const st = snap.state ?? {};
  next.model = st.model ?? null;
  next.thinkingLevel = st.thinkingLevel ?? "off";
  next.sessionName = st.sessionName ?? "";
  next.streaming = !!st.isStreaming;
  next.compacting = !!st.isCompacting;
  next.steeringMode = st.steeringMode ?? "all";
  next.followUpMode = st.followUpMode ?? "one-at-a-time";
  next.autoCompaction = st.autoCompactionEnabled ?? true;
  // deep-copy: the reducer accumulates usage into stats in place and must
  // never mutate the caller's snapshot object
  next.stats = snap.stats ? (JSON.parse(JSON.stringify(snap.stats)) as Stats) : null;
  for (const m of snap.messages?.messages ?? []) appendMessage(next, m);
  return next;
}

function startStreamingAssistant(view: SessionView, ts?: number): AssistantItem {
  const item: AssistantItem = { kind: "assistant", ts, blocks: [], streaming: true };
  view.items.push(item);
  view.streamingIndex = view.items.length - 1;
  return item;
}

function streamingAssistant(view: SessionView): AssistantItem | null {
  const item = view.items[view.streamingIndex];
  return item && item.kind === "assistant" ? item : null;
}

// accumulateUsage keeps the state bar live between snapshots: pi only pushes
// stats in the snapshot, so completed assistant messages update them locally.
// The next snapshot (reconnect) replaces these estimates with the real thing.
function accumulateUsage(view: SessionView, message: Message): void {
  const u = message.usage as
    | {
        input?: number;
        output?: number;
        cacheRead?: number;
        cacheWrite?: number;
        cost?: { total?: number };
      }
    | undefined;
  if (!u) return;
  const stats: Stats = view.stats ?? {};
  const tokens = (stats.tokens ??= {});
  tokens.input = (tokens.input ?? 0) + (u.input ?? 0);
  tokens.output = (tokens.output ?? 0) + (u.output ?? 0);
  if (u.cost?.total) stats.cost = (stats.cost ?? 0) + u.cost.total;
  // last-response usage approximates the current context size
  const ctxTokens = (u.input ?? 0) + (u.cacheRead ?? 0) + (u.cacheWrite ?? 0) + (u.output ?? 0);
  if (ctxTokens > 0) {
    const ctx = (stats.contextUsage ??= {});
    ctx.tokens = ctxTokens;
    const window = ctx.contextWindow ?? view.model?.contextWindow;
    if (window) {
      ctx.contextWindow = window;
      ctx.percent = Math.min(100, Math.round((ctxTokens / window) * 100));
    }
  }
  view.stats = stats;
}

function settle(view: SessionView): void {
  view.streaming = false;
  view.streamingIndex = -1;
  view.retry = null;
}

// applyEvent mutates the view for one live pi/synthetic event.
export function applyEvent(view: SessionView, ev: PiEvent): void {
  switch (ev.type) {
    case "agent_start":
      view.streaming = true;
      break;
    // agent_settled is pi's settle signal; agent_end is the documented
    // completion event — either ends the run. turn_end does not (more turns
    // may follow within one run).
    case "agent_settled":
    case "agent_end":
      settle(view);
      break;
    case "message_start":
      if (ev.message?.role === "assistant") {
        startStreamingAssistant(view, ev.message.timestamp);
      } else if (ev.message?.role === "user") {
        appendMessage(view, ev.message);
      }
      break;
    case "message_update":
      if (ev.message?.role === "assistant") {
        const item = streamingAssistant(view) ?? startStreamingAssistant(view, ev.message.timestamp);
        item.blocks = asBlocks(ev.message.content);
        item.streaming = true;
        registerTools(view, item.blocks);
      }
      break;
    case "message_end":
      if (ev.message?.role === "assistant") {
        const item = streamingAssistant(view);
        if (item) {
          item.blocks = asBlocks(ev.message.content);
          item.streaming = false;
          item.usage = ev.message.usage;
          item.error = ev.message.stopReason === "error" ? ev.message.errorMessage : undefined;
          registerTools(view, item.blocks);
          view.streamingIndex = -1;
        } else {
          appendMessage(view, ev.message);
        }
        accumulateUsage(view, ev.message);
      } else if (ev.message && ev.message.role !== "user") {
        // user messages already landed via message_start
        appendMessage(view, ev.message);
      }
      break;
    case "tool_execution_start":
      if (ev.toolCallId && !view.tools[ev.toolCallId]) {
        view.tools[ev.toolCallId] = {
          id: ev.toolCallId,
          name: ev.toolName ?? "tool",
          arguments: ev.args,
          path: toolPathOf(ev.args),
          output: "",
          status: "running",
        };
        view.items.push({ kind: "tool", id: ev.toolCallId });
      }
      break;
    case "tool_execution_update":
      if (ev.toolCallId && view.tools[ev.toolCallId] && ev.partialResult) {
        view.tools[ev.toolCallId].output = contentText(ev.partialResult.content);
      }
      break;
    case "tool_execution_end":
      finishTool(view, ev.toolCallId, ev.result ? contentText(ev.result.content) : null, ev.isError);
      break;
    case "queue_update":
      view.queue = { steering: ev.steering ?? [], followUp: ev.followUp ?? [] };
      break;
    case "compaction_start":
      view.compacting = true;
      view.items.push({ kind: "compaction", reason: ev.reason ?? "manual", running: true });
      break;
    case "compaction_end": {
      view.compacting = false;
      let item = [...view.items].reverse().find(
        (i): i is CompactionItem => i.kind === "compaction" && i.running,
      );
      if (!item) {
        item = { kind: "compaction", reason: ev.reason ?? "manual", running: true };
        view.items.push(item);
      }
      item.running = false;
      item.aborted = ev.aborted;
      item.error = ev.errorMessage;
      if (ev.result) {
        item.summary = ev.result.summary;
        item.tokensBefore = ev.result.tokensBefore;
        item.tokensAfter = ev.result.estimatedTokensAfter;
      }
      // per rpc.md, context usage is unknown until the next assistant response
      if (!ev.aborted && !ev.errorMessage && view.stats?.contextUsage) {
        view.stats.contextUsage.tokens = null;
        view.stats.contextUsage.percent = null;
      }
      break;
    }
    case "auto_retry_start":
      view.retry = {
        attempt: ev.attempt ?? 1,
        maxAttempts: ev.maxAttempts ?? 1,
        delayMs: ev.delayMs ?? 0,
        errorMessage: ev.errorMessage,
        receivedAt: Date.now(),
      };
      break;
    case "auto_retry_end":
      view.retry = null;
      if (ev.success === false) {
        view.items.push({
          kind: "notice",
          level: "error",
          title: `retry failed after ${ev.attempt ?? "?"} attempts`,
          detail: ev.finalError,
        });
      }
      break;
    case "extension_error":
      view.items.push({
        kind: "notice",
        level: "error",
        title: `extension error${ev.event ? ` (${ev.event})` : ""}`,
        detail: [ev.extensionPath, ev.error].filter(Boolean).join("\n"),
      });
      break;
    case "extension_ui_request":
      switch (ev.method) {
        case "setStatus":
          if (ev.statusKey) {
            if (ev.statusText === undefined || ev.statusText === null) {
              delete view.statusLines[ev.statusKey];
            } else {
              view.statusLines[ev.statusKey] = ev.statusText;
            }
          }
          break;
        case "setWidget":
          if (ev.widgetKey) {
            const target = ev.widgetPlacement === "belowEditor" ? view.widgetsBelow : view.widgetsAbove;
            if (!ev.widgetLines) delete target[ev.widgetKey];
            else target[ev.widgetKey] = ev.widgetLines;
          }
          break;
        case "setTitle":
          view.title = ev.title ?? null;
          break;
        default:
          // notify / set_editor_text are handled by the store layer (toast,
          // composer); dialog methods are auto-cancelled server-side.
          break;
      }
      break;
    case "piweb_bash":
      if (ev.result || ev.command !== undefined) {
        const r = (ev.result ?? {}) as { output?: string; exitCode?: number };
        view.items.push({
          kind: "bash",
          command: ev.command ?? "",
          output: r.output ?? "",
          exitCode: r.exitCode ?? 0,
        });
      }
      break;
    case "piweb_model":
      if (ev.model) view.model = ev.model;
      break;
    case "piweb_thinking":
      if (ev.level) view.thinkingLevel = ev.level;
      break;
    default:
      break;
  }
}
