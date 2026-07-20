// Wire shapes from pi's RPC stream and pi-web's snapshot/synthetic events.
// The authoritative source is the installed pi's docs/rpc.md; these mirror the
// subset the UI consumes. Kept deliberately loose (optional fields) so a newer
// pi adding fields or event types never breaks rendering.

export type Block =
  | { type: "text"; text: string }
  | { type: "thinking"; thinking?: string }
  | { type: "toolCall"; id: string; name: string; arguments?: Record<string, unknown> };

export interface ModelRef {
  provider?: string;
  id?: string;
  name?: string;
  contextWindow?: number;
}

export interface Usage {
  input?: number;
  output?: number;
}

export interface ContentBlock {
  type: string;
  text?: string;
}

export interface Message {
  role: "user" | "assistant" | "toolResult" | "bashExecution" | string;
  timestamp?: number;
  content?: string | Block[] | ContentBlock[];
  // assistant
  stopReason?: string;
  errorMessage?: string;
  usage?: Usage;
  // toolResult
  toolCallId?: string;
  toolName?: string;
  isError?: boolean;
  // bashExecution
  command?: string;
  output?: string;
  exitCode?: number;
}

export interface SessionState {
  model?: ModelRef | null;
  thinkingLevel?: string;
  isStreaming?: boolean;
  isCompacting?: boolean;
  steeringMode?: string;
  followUpMode?: string;
  autoCompactionEnabled?: boolean;
  sessionName?: string;
}

export interface Stats {
  tokens?: { input?: number; output?: number; total?: number };
  contextUsage?: { tokens?: number | null; contextWindow?: number; percent?: number | null };
  cost?: number | null;
}

export interface Snapshot {
  id?: string;
  cwd?: string;
  state?: SessionState;
  messages?: { messages?: Message[] };
  stats?: Stats | null;
}

export interface CompactionResult {
  summary?: string;
  tokensBefore?: number;
  estimatedTokensAfter?: number;
}

// Live event union: only the fields the UI acts on. Unknown event types must
// be ignored gracefully — a newer pi never breaks rendering.
export interface PiEvent {
  type: string;
  message?: Message;
  messages?: Message[];
  // tool execution
  toolCallId?: string;
  toolName?: string;
  args?: Record<string, unknown>;
  partialResult?: { content?: string | ContentBlock[] };
  isError?: boolean;
  // tool_execution_end carries {content}; synthetic piweb_bash carries
  // {output, exitCode}.
  result?: {
    content?: string | ContentBlock[];
    output?: string;
    exitCode?: number;
  } & CompactionResult;
  // queue_update
  steering?: string[];
  followUp?: string[];
  // compaction
  reason?: string;
  aborted?: boolean;
  willRetry?: boolean;
  errorMessage?: string;
  // auto retry
  attempt?: number;
  maxAttempts?: number;
  delayMs?: number;
  success?: boolean;
  finalError?: string;
  // extension_error
  extensionPath?: string;
  event?: string;
  error?: string;
  // extension_ui_request
  id?: string;
  method?: string;
  notifyType?: string;
  statusKey?: string;
  statusText?: string;
  widgetKey?: string;
  widgetLines?: string[];
  widgetPlacement?: string;
  title?: string;
  text?: string;
  // synthetic piweb_* events
  command?: string;
  model?: ModelRef;
  level?: string;
  entryId?: string;
}

// contentText flattens a message/tool content payload to plain text: a string
// passes through; an array keeps only text blocks.
export function contentText(content: unknown): string {
  if (typeof content === "string") return content;
  if (!Array.isArray(content)) return "";
  return content
    .filter((b): b is ContentBlock => !!b && (b as ContentBlock).type === "text")
    .map((b) => b.text ?? "")
    .join("\n");
}

export function toolPathOf(args?: Record<string, unknown>): string | null {
  if (!args) return null;
  const p = args.path ?? args.file_path;
  return typeof p === "string" ? p : null;
}

// toolSummaryText is the one-line label next to a tool name: the command for
// bash-like tools, the path for file tools, else compact JSON of the args.
export function toolSummaryText(args?: Record<string, unknown>): string {
  if (!args) return "";
  if (typeof args.command === "string") return args.command;
  const path = toolPathOf(args);
  if (path) return path;
  if (typeof args.pattern === "string") return args.pattern;
  try {
    const s = JSON.stringify(args);
    return s === "{}" ? "" : s;
  } catch {
    return "";
  }
}
