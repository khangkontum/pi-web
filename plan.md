# pi-web v2 — settled design & implementation plan

**Status:** design settled 2026-07-20 (kaan). This file is the
implementation driver; `AGENTS.md` is the rulebook. When they disagree,
AGENTS.md wins. Delete sections here as they land.

**Shape of the release:** one big-bang release — the UI is rewritten as a
Vite + Svelte 5 SPA *and* the kept feature set below ships with it. The Go
API grows additively; the Go-side invariants (stateless, loopback,
stdlib-only module, one child per session) are unchanged.

**Protocol reference:** do NOT trust protocol shapes copied into old
versions of this file. Read the shipped spec for the installed pi:
`$(npm root -g)/@earendil-works/pi-coding-agent/docs/rpc.md` (verified
locally at pi 0.80.1). `security.md` there explains `--approve` /
project trust. Code to the newest pi; the updater (workstream B1) keeps
deployments current.

---

## Settled decisions (summary)

| Area | Decision |
|---|---|
| UI stack | Vite + Svelte 5 SPA (no SvelteKit), bun-managed, hash-routed (`#/session/<id>`) |
| Build wiring | `web/` → `vite build` → `internal/piweb/ui/dist` (gitignored), `go:embed all:ui/dist`; bare `go build` always compiles and serves a "UI not built" page |
| npm deps | Pragmatic: well-maintained libs where they earn their keep (streamdown, shiki, virtual list). Go module stays zero-dep |
| Markdown | streamdown (Svelte flavor) — hardened for untrusted streaming AI output. Nothing bypasses it into `innerHTML` (feed XSS = bash RCE) |
| pi version skew | Eliminated, not tolerated: boot-time flag probe, npm-registry check, `pi update pi` apply, degrade + banner on failure, recycle non-streaming children after upgrade |
| Auto-update | Two independent toggles (pi-web, pi) persisted in settings.json; update controls move into a settings panel behind a settings button |
| Migration | Big bang: parity port + all kept features in one release |
| UI testing | vitest for pure logic only; Go wire-shape tests remain the contract |

---

## Workstream A — toolchain & scaffold

1. **`web/` scaffold**: bun + Vite + Svelte 5. Pin bun in `mise.toml`.
   `vite build` outputs to `../internal/piweb/ui/dist`.
2. **Embed wiring**: change `server.go` to `//go:embed all:ui/dist`.
   Commit a placeholder (`.gitkeep`) so the embed target always exists;
   gitignore the rest of `dist/`. When the embedded FS has no
   `index.html`, serve a plain "UI not built — run `mise run build`" page
   instead of a broken app.
3. **mise tasks**: `mise run build` (bun install + vite build + go build),
   `mise run dev` (vite dev server with proxy for `/api`, `/version` → Go
   backend on its usual port).
4. **CI** (`.github/workflows/ci.yml`): add bun setup, `bun install`,
   `vite build`, `bun run test` (vitest) before the existing
   gofmt/vet/build/test steps.
5. **Release** (`.github/workflows/release.yml`): web build once, then the
   existing cross-compile matrix embeds the same `dist/`.
6. Delete the old vanilla UI (`internal/piweb/ui/index.html`, `app.js`,
   `style.css`) once parity is reached.

## Workstream B — Go backend

### B1. pi version management (`piupdate.go`, new)

Mirror the structure of `update.go` (injectable collaborators, hermetic
tests — fake the probe, the registry, and the subprocess):

- **Probe**: at boot run `pi --help`, parse the flag list, cache it.
  `supervisor.piCommand()` consults it and only appends `--approve` (and
  any future optional flag) when supported.
- **Check** (side-effect-free): GET
  `https://registry.npmjs.org/@earendil-works/pi-coding-agent/latest`,
  compare `version` against `pi --version` output. Same cadence pattern as
  the self-updater (initial delay + interval loop).
- **Apply**: run `pi update pi`; treat any failure as keep-current. On
  success re-run `pi --version` and re-probe flags.
- **Degrade + banner**: status object exposes
  `{current, latest, available, error, approveSupported}`. When
  `approveSupported` is false, children spawn without the flag and the UI
  shows a persistent banner ("pi vX.Y is outdated — update failed:
  <reason>"). Never refuse to start sessions over pi age.
- **Recycle after upgrade**: close every child that is not mid-turn
  (including ones with subscribers — SSE drops, `EventSource`
  auto-reconnects, `supervisor.get()` respawns on the new binary and the
  snapshot re-renders). Mid-turn children are recycled on `agent_settled`.
- **Settings**: `settings.go` becomes
  `{autoUpdate, autoUpdatePi}` — nothing more.
- **Endpoints**: `GET /api/pi` (status), `POST /api/pi/check`,
  `POST /api/pi/update`, `POST /api/pi/auto {enabled}`.

### B2. New/changed session endpoints

All thin wrappers over RPC commands (shapes: shipped `docs/rpc.md`):

- `POST /api/sessions/{id}/message` — add optional
  `images: [{data, mimeType}]` (base64), forwarded on the `prompt`
  command's `images[]`.
- `GET  /api/sessions/{id}/fork-messages` → `get_fork_messages`.
- `POST /api/sessions/{id}/fork {entryId}` → `fork`. Broadcast a
  `piweb_*` event so other browsers refresh, mirroring `piweb_model`.
- `POST /api/sessions/{id}/compact` → `compact`;
  `POST /api/sessions/{id}/compaction-auto {enabled}` →
  `set_auto_compaction`.
- `POST /api/sessions/{id}/retry-abort` → `abort_retry`.
- `POST /api/sessions/{id}/steering {mode}` → `set_steering_mode`;
  `POST /api/sessions/{id}/follow-up {mode}` → `set_follow_up_mode`.

### B3. Workspace endpoints (explorer + fuzzy finder)

- `GET /api/files?base=<dir>` — recursive file index for the fuzzy
  finder. Prefer `git ls-files` (respects .gitignore) with a bounded
  `WalkDir` fallback; cap count and depth. Fuzzy matching itself is
  client-side.
- Extend `GET /api/dirs` to also return files (or add `GET /api/tree`)
  for the explorer pane.
- `GET /api/raw?path=&base=` — raw bytes with correct Content-Type for
  image/PDF/audio preview. Path resolution reuses the existing
  `readFileView` base+path rules (loopback trust: any readable path).

### B4. Event handling

- Forward-compat: treat `agent_settled` *and* `agent_end`/`turn_end` as
  stream-settling signals in `session.broadcast` state tracking (cheap
  skew insurance even with the updater).
- Everything else already flows: `queue_update`, `compaction_*`,
  `auto_retry_*`, `extension_error`, and passive
  `extension_ui_request` methods (`notify`, `setStatus`, `setWidget`,
  `setTitle`) are events the client renders. Keep the existing auto-cancel
  for dialog methods (`select`/`confirm`/`input`/`editor`) — real dialogs
  are deferred.
- Every new endpoint gets a `docs/reference.md` row and an httptest case
  in the same change; SSE-visible changes get exact-wire-shape tests.

## Workstream C — Svelte parity port

Reproduce current behavior 1:1 against the (extended) API before layering
features. The current `app.js` is the spec; notable behaviors to keep:

- SSE lifecycle: `snapshot` event renders full state/messages/stats, then
  `pi` events stream; `EventSource` auto-reconnect re-snapshots.
- Sticky scroll only when near bottom; tool blocks keyed by `toolCallId`
  updating in place (`tool_execution_start/update/end`); `!` prefix runs
  operator bash; new-session flow (folder picker → create on first
  message, with model/thinking/name presets); model & thinking custom
  dropdowns (no native `<select>`); file-view overlay; mobile drawer;
  `piweb_bash` / `piweb_model` / `piweb_thinking` broadcasts.
- Update controls move from the rail into a **settings panel** behind a
  settings button (pi-web version/check/apply + auto toggle, pi
  version/check/update + auto toggle, theme).

## Workstream D — features (all in the bang)

| Feature | Notes / acceptance |
|---|---|
| Markdown (#2) | streamdown for assistant + user text, streaming-aware; code blocks highlighted (shiki via streamdown) |
| ANSI → HTML (#3) | SGR subset parser for tool/bash output; vitest-covered; builds DOM, no innerHTML |
| Diff rendering (#4) | edit/write tool calls render as diffs (old/new from tool args); vitest-covered |
| Virtualization (#7) | windowed feed; 1000-message session scrolls smoothly; find a maintained Svelte virtual-list lib or write a small one |
| Grouped rail (#1) | sessions grouped by `cwd` with project headers, newest-first within groups |
| Full state bar (#8) | tokens + context% (exists) + cost + compaction indicator from `get_session_stats`/`get_state` |
| Event coverage (#9+#35) | queue contents, compaction start/end + summary rendering, retry countdown, extension errors — no more silent stalls |
| Fork (#10) | per-user-message fork affordance → `fork-messages`/`fork`; forked session opens in rail |
| Image input (#17) | paste + drag-drop images in composer; thumbnails before send; sent via `prompt images[]` |
| @-refs + fuzzy (#15+#16) | `@` in composer opens fuzzy file popup fed by `/api/files`; inserts path text |
| Explorer + preview (#14) | tree pane from `/api/tree`; text via `/api/file`, img/PDF/audio via `/api/raw` |
| Drafts (#30) | composer text persisted per-session in localStorage; survives reload/switch |
| Shortcuts + IME (#31) | `isComposing` guard on Enter (Vietnamese/CJK), shortcut map documented in a help popover |
| Minimap (#29) | derived from the message model (not DOM) so it coexists with virtualization |
| Theme (#32) | CSS variables, `prefers-color-scheme` + manual toggle in settings panel |
| Mobile (#33) | carry existing drawer/responsive layout through the rewrite properly |
| Audio (#34) | optional ping on `agent_settled` in background tabs |
| Compaction control (#37) | compact-now button + auto toggle |
| Auto-retry control (#38) | countdown UI + abort-retry button |
| Steering modes (#36) | steer/follow-up mode pickers in settings panel |
| Passive extension UI (#24a) | toasts (`notify`), status line (`setStatus`), widget strip (`setWidget`, both placements), document title (`setTitle`) |
| Old-pi banner (B1) | persistent, dismiss-per-session, reason string from `/api/pi` |

## Deferred (explicitly out of the bang)

- **Extension dialogs (#24b)** — needs first-answer-wins routing across N
  browsers + auto-cancel timeout when none connected. Keep auto-cancel.
- **Command palette (#25)** — `/name` text already reaches pi; picker later.
- **Git worktrees (#13)** — real backend surface; later.
- **Snapshot pagination** — snapshot still ships full history; revisit only
  if real sessions hurt (virtualization fixes rendering, not payload).

## Cut (do not implement; decided 2026-07-20)

- Skills mgmt (#22), plugin mgmt (#23) — out of product scope for now.
- API-key management (#26) — conflicts with the loopback trust model
  (now an AGENTS.md invariant).
- Model connection test (#19) — side-effectful; errors surface on use.
- Clone (#11), export HTML (#21), tabs (#28), copy-last (#41) — slimness.
- switch_session (#12) — redundant: pi-web opens one child per session.

## Suggested implementation order

1. A1–A3 (scaffold, embed, tasks) — repo builds both ways.
2. C (parity port) against the unchanged API, verified side-by-side with
   the old UI before deleting it (A6).
3. B1 (pi updater + probe + banner) — fixes the `--approve` outage class.
4. B2–B4 + D features, each: endpoint + httptest + reference.md row +
   UI + (where logic) vitest, in one change per feature.
5. A4–A5 (CI/release wiring) can land with 1; keep green throughout.

**Definition of done:** old UI deleted; `go build` from bare clone works
(placeholder page); `mise run build` produces the full binary; CI green
(gofmt, vet, go test, vitest, web build); every route in `newServer` has a
`docs/reference.md` row; a session on a VM with pi ≤0.78 degrades with the
banner instead of breaking.
