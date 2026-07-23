// Typed fetch wrappers over pi-web's JSON API (one row per route in
// docs/reference.md). Errors surface the server's {error} field.

async function request<T>(path: string, opts?: RequestInit): Promise<T> {
  const resp = await fetch(path, opts);
  if (!resp.ok) {
    let detail = `${path}: HTTP ${resp.status}`;
    try {
      const body = await resp.json();
      if (body?.error) detail = body.error;
    } catch {
      /* non-JSON error body */
    }
    throw new Error(detail);
  }
  if (resp.status === 204) return undefined as T;
  return (await resp.json()) as T;
}

function postJSON<T>(path: string, body: unknown): Promise<T> {
  return request<T>(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body ?? {}),
  });
}

const sid = (id: string) => encodeURIComponent(id);
const q = (params: Record<string, string | null | undefined>) => {
  const search = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v) search.set(k, v);
  }
  const s = search.toString();
  return s ? `?${s}` : "";
};

export interface SessionSummary {
  id: string;
  path: string;
  cwd: string;
  title: string;
  updatedAt: string;
  live: boolean;
}

export interface ModelInfo {
  provider: string;
  model: string;
  context?: string;
  thinking?: boolean;
  images?: boolean;
}

export interface GitInfo {
  repo: boolean;
  branch?: string;
  dirtyCount?: number;
  graph?: string;
  /** workspace-relative path → one-letter status (M, A, D, R, U, ?) */
  changes?: Record<string, string>;
}

export interface GitCommit {
  hash: string;
  parents: string[];
  refs: string;
  author: string;
  date: string;
  subject: string;
}

export interface GitDiff {
  patch: string;
  truncated?: boolean;
}

export interface DirListing {
  path: string;
  parent?: string;
  dirs?: string[];
}

export interface TreeEntry {
  name: string;
  dir: boolean;
}

export interface TreeListing {
  path: string;
  parent?: string;
  entries?: TreeEntry[];
}

export interface FileView {
  path: string;
  content: string;
  truncated?: boolean;
  binary?: boolean;
  size?: number;
}

export interface TerminalInfo {
  id: string;
  cwd: string;
  shell?: string;
  createdAt: string;
}

export interface UpdateStatus {
  current?: string;
  latest?: string;
  available?: boolean;
  canUpdate?: boolean;
  autoUpdate?: boolean;
  error?: string;
  checkedAt?: string;
  applied?: boolean;
}

export interface PiStatus {
  current?: string;
  latest?: string;
  available?: boolean;
  autoUpdate?: boolean;
  autoUpdatePi?: boolean;
  approveSupported?: boolean;
  error?: string;
  checkedAt?: string;
}

export interface AppSettings {
  collapseThinking: boolean;
}

export interface CreateSessionBody {
  message?: string;
  name?: string;
  cwd?: string;
  provider?: string;
  modelId?: string;
  thinking?: string;
}

export interface ImagePart {
  data: string;
  mimeType: string;
}

export interface ForkMessage {
  entryId: string;
  text: string;
}

export interface CommandInfo {
  name: string;
  description?: string;
  source: "extension" | "prompt" | "skill";
  location?: "user" | "project" | "path";
  path?: string;
}

export const api = {
  version: () => request<{ service: string; version: string }>("/version"),

  listSessions: () => request<{ sessions: SessionSummary[] }>("/api/sessions"),
  createSession: (body: CreateSessionBody) =>
    postJSON<{ id: string; file?: string }>("/api/sessions", body),
  message: (id: string, message: string, images?: ImagePart[]) =>
    postJSON<void>(`/api/sessions/${sid(id)}/message`, images?.length ? { message, images } : { message }),
  abort: (id: string) => postJSON<void>(`/api/sessions/${sid(id)}/abort`, {}),
  bash: (id: string, command: string) => postJSON<void>(`/api/sessions/${sid(id)}/bash`, { command }),
  setModel: (id: string, provider: string, modelId: string) =>
    postJSON<void>(`/api/sessions/${sid(id)}/model`, { provider, modelId }),
  setThinking: (id: string, level: string) =>
    postJSON<void>(`/api/sessions/${sid(id)}/thinking`, { level }),
  commands: (id: string) =>
    request<{ commands: CommandInfo[] }>(`/api/sessions/${sid(id)}/commands`),
  forkMessages: (id: string) =>
    request<{ messages: ForkMessage[] }>(`/api/sessions/${sid(id)}/fork-messages`),
  fork: (id: string, entryId: string) =>
    postJSON<{ result: unknown }>(`/api/sessions/${sid(id)}/fork`, { entryId }),
  compact: (id: string) => postJSON<{ result: unknown }>(`/api/sessions/${sid(id)}/compact`, {}),
  setAutoCompaction: (id: string, enabled: boolean) =>
    postJSON<void>(`/api/sessions/${sid(id)}/compaction-auto`, { enabled }),
  abortRetry: (id: string) => postJSON<void>(`/api/sessions/${sid(id)}/retry-abort`, {}),
  setSteering: (id: string, mode: string) =>
    postJSON<void>(`/api/sessions/${sid(id)}/steering`, { mode }),
  setFollowUp: (id: string, mode: string) =>
    postJSON<void>(`/api/sessions/${sid(id)}/follow-up`, { mode }),

  models: (refresh = false) =>
    request<{ models: ModelInfo[] }>(`/api/models${refresh ? "?refresh=1" : ""}`),
  dirs: (path?: string | null) => request<DirListing>(`/api/dirs${q({ path })}`),
  tree: (path?: string | null) => request<TreeListing>(`/api/tree${q({ path })}`),
  files: (base?: string | null) =>
    request<{ files: string[]; truncated?: boolean }>(`/api/files${q({ base })}`),
  git: (base?: string | null) => request<GitInfo>(`/api/git${q({ base })}`),
  gitLog: (base?: string | null) => request<{ commits: GitCommit[] }>(`/api/git/log${q({ base })}`),
  gitDiff: (base?: string | null, ref?: string | null, path?: string | null) =>
    request<GitDiff>(`/api/git/diff${q({ base, ref, path })}`),
  file: (path: string, base?: string | null) => request<FileView>(`/api/file${q({ path, base })}`),
  rawUrl: (path: string, base?: string | null) => `/api/raw${q({ path, base })}`,

  terminals: () => request<{ terminals: TerminalInfo[] }>("/api/terminals"),
  createTerminal: (cwd?: string | null) =>
    postJSON<{ id: string; cwd?: string }>("/api/terminals", cwd ? { cwd } : {}),
  terminalInput: (id: string, data: string) =>
    postJSON<void>(`/api/terminals/${sid(id)}/input`, { data }),
  terminalResize: (id: string, cols: number, rows: number) =>
    postJSON<void>(`/api/terminals/${sid(id)}/resize`, { cols, rows }),
  killTerminal: (id: string) => request<void>(`/api/terminals/${sid(id)}`, { method: "DELETE" }),

  settings: () => request<AppSettings>("/api/settings"),
  setSettings: (settings: AppSettings) => postJSON<AppSettings>("/api/settings", settings),

  update: () => request<UpdateStatus>("/api/update"),
  checkUpdate: () => postJSON<UpdateStatus>("/api/update/check", {}),
  applyUpdate: () => postJSON<UpdateStatus>("/api/update/apply", {}),
  setAutoUpdate: (enabled: boolean) => postJSON<UpdateStatus>("/api/update/auto", { enabled }),

  pi: () => request<PiStatus>("/api/pi"),
  checkPi: () => postJSON<PiStatus>("/api/pi/check", {}),
  updatePi: () => postJSON<PiStatus>("/api/pi/update", {}),
  setAutoPi: (enabled: boolean) => postJSON<PiStatus>("/api/pi/auto", { enabled }),
};
