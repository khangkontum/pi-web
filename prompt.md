# Rebuild the pi-web frontend from scratch

## Mission

The current Svelte UI in `web/src/` is a half-finished parity port. Delete it
and rebuild the frontend clean, in one coherent design, implementing the full
feature set below. You own every visual and structural choice — be creative.
The backend is done; this is a frontend-only job.

Read `AGENTS.md` first — it is the rulebook and wins over everything else,
including this file. `plan.md` has historical design context; where this file
and plan.md disagree about UI scope, this file wins.

## What pi-web is

A single Go binary that serves a web UI for driving `pi` (a coding agent CLI)
sessions. The Go server supervises one `pi --mode rpc` child process per live
session and exposes a small JSON API plus an SSE event stream. The UI is a
Vite + Svelte 5 SPA (bun-managed, no SvelteKit) in `web/`, built into
`internal/piweb/ui/dist` and embedded into the binary via `go:embed`.

Think of the product as a console for watching and steering autonomous coding
agents: long streaming sessions, heavy tool/terminal output, multiple projects
at once, often left open for hours. Readability under streaming is the core
experience.

## Fresh start — what to keep, what to burn

- **Burn:** everything in `web/src/`. Do not port the old components or CSS.
- **Keep:** the toolchain wiring — `web/package.json` scripts, `vite.config.ts`
  (build output goes to `../internal/piweb/ui/dist`, dev server proxies `/api`
  and `/version` to the Go server), `svelte.config.js`, `tsconfig.json`,
  `mise run build` / `mise run dev`. Adjust them if you need to; don't break
  the embed contract (`go build ./...` must always work, UI built or not).
- **Reference, don't copy:** the old `web/src/lib/` (protocol.ts, feed.ts,
  api.ts, sse.ts) encodes hard-won knowledge of the wire shapes and reducer
  behavior. Reuse the ideas or the code as you see fit — but if you keep code,
  own it fully and re-test it.
- npm deps are pragmatic: use well-maintained libraries where they earn their
  keep. The Go module stays zero-dependency (you shouldn't need to touch Go at
  all — if you find a genuine backend gap, write it up in a `TODO-backend.md`
  instead of hacking around it).

## The contract you build against

- **HTTP API:** every route is registered in `internal/piweb/server.go` and
  documented in `docs/reference.md`. The backend is feature-complete: sessions
  (create/list/message-with-images/abort/bash/model/thinking), fork
  (`fork-messages` + `fork`), compaction (`compact`, `compaction-auto`),
  retry (`retry-abort`), steering (`steering`, `follow-up`), workspace
  (`/api/dirs`, `/api/tree`, `/api/files`, `/api/file`, `/api/raw`,
  `/api/git`), self-update (`/api/update/*`), and pi version management
  (`/api/pi/*`).
- **SSE:** `GET /api/sessions/{id}/events` sends a `snapshot` event (full
  state + messages + stats) first, then streams pi events. `EventSource`
  auto-reconnect re-snapshots; the UI must survive reconnects invisibly.
- **pi events:** the authoritative protocol spec ships with the installed pi:
  `$(npm root -g)/@earendil-works/pi-coding-agent/docs/rpc.md`. Read it —
  do not trust protocol shapes copied into old files. Unknown event types must
  be ignored gracefully (a newer pi must never break rendering).
- **Synthetic events:** the server broadcasts `piweb_bash`, `piweb_model`,
  `piweb_thinking`, `piweb_fork` so multiple open browsers stay in sync.

## Security invariant (non-negotiable)

Rendered model output is attacker-influenced, and this UI can invoke
`/api/sessions/{id}/bash` — feed XSS equals shell RCE. All markdown/HTML
rendering goes through svelte-streamdown's hardened path. Nothing ever
bypasses it into `innerHTML`/`{@html}`. ANSI and diff renderers build DOM via
Svelte templates, never HTML strings.

## Features (all required)

Session basics
- Rail of sessions grouped by working directory (project headers), newest
  first; live indicator on sessions with an active child; new-session flow:
  pick a folder (via `/api/dirs`), optionally model/thinking/name, session is
  created on first message.
- Feed renders the snapshot then live events: user/assistant messages,
  thinking blocks, tool calls updating in place (`tool_execution_start/
  update/end` keyed by toolCallId), errors. Sticky scroll: follow output only
  while the user is near the bottom; never yank them up while reading history.
- Composer: Enter sends, Shift+Enter newlines, `isComposing` guard so IME
  (Vietnamese/CJK) commits never send early. `!` prefix runs an operator bash
  command in the session cwd. Stop button aborts a streaming turn.
- Model picker and thinking-level picker; changes broadcast to other browsers.

Rendering quality
- Markdown for assistant and user text via svelte-streamdown,
  streaming-aware, with syntax-highlighted code blocks (shiki through
  streamdown).
- ANSI → styled output for tool/bash text: implement an SGR-subset parser
  (colors, bold, dim, reset at minimum), unit-tested with vitest.
- Edit/write tool calls render as diffs (derive old/new from the tool
  arguments), unit-tested.
- Virtualized feed: a 1000-message session must scroll smoothly. Use a
  maintained Svelte virtual-list library or write a small windowing component.
- A minimap or equivalent overview affordance for long sessions, derived from
  the message model (not the DOM) so it works with virtualization.

Telemetry & agent lifecycle (no silent stalls — every state is visible)
- State bar: input/output tokens, context %, cost, and a compaction indicator.
- Queue contents when messages are queued; compaction start/end with the
  summary rendered; auto-retry countdown with an abort-retry button;
  extension errors surfaced.
- Steering mode and follow-up mode controls.
- Compact-now button + auto-compaction toggle.
- Passive extension UI: toasts (`notify`), a status line (`setStatus`),
  widget strip (`setWidget`, both placements), document title (`setTitle`).
  Dialog-style extension requests (`select`/`confirm`/`input`/`editor`) are
  auto-cancelled server-side — do not build dialogs.

Working with files
- `@` in the composer opens a fuzzy file finder fed by `/api/files`
  (matching is client-side); selecting inserts the path as text.
- File explorer pane from `/api/tree`; clicking opens a preview — text via
  `/api/file`, images/PDF/audio via `/api/raw`.
- Tool calls that reference a file link to that preview.

Input
- Image input: paste and drag-drop into the composer, thumbnail chips before
  send, sent as base64 `images[]` on the message endpoint.
- Per-session composer drafts in localStorage; survive reload and session
  switches.
- A keyboard-shortcut map, documented in a small help popover.

Sessions as objects
- Fork: an affordance on each user message → `fork-messages`/`fork`; the
  forked session appears in the rail and opens.
- Hash routing (`#/session/<id>`) so sessions are linkable and back/forward
  work.

App chrome
- Settings panel (not the rail): theme control, pi-web version/check/apply +
  auto-update toggle, pi version/check/update + auto-update toggle
  (`/api/update/*`, `/api/pi/*`).
- Persistent (dismiss-per-session) banner when `/api/pi` reports pi is
  outdated with an upgrade error — show the reason.
- Light and dark themes as full peers: `prefers-color-scheme` default +
  manual toggle.
- Mobile: a real responsive layout (rail as drawer, usable composer).
- Optional audio ping when a turn settles while the tab is in the background.

## Explicitly out of scope

Extension dialogs, command palette/slash-command picker, git worktrees,
skills/plugin management, API-key management (violates the loopback trust
model), model connection tests, session clone/export/tabs, switch_session.

## Design: you decide

You own the aesthetic and the layout. Requirements are few and hard:

- Custom-styled controls everywhere — no native `<select>`, no native pickers.
- Both themes first-class; all colors/type/spacing as CSS custom-property
  tokens; components never hard-code values.
- Optimized for long-form reading of streaming mono-heavy content; respects
  `prefers-reduced-motion`; keyboard-accessible with visible focus.
- Fonts must be self-hosted (the binary runs offline) — `@fontsource-*`
  packages, no CDN links.

Beyond that: be opinionated. Give it a real identity — this is a console for
autonomous coding agents, not a generic chat app. Avoid the stock AI-product
look; make deliberate typographic and color choices and carry them through
every surface (empty states, overlays, scrollbars, selection). Motion should
communicate agent state, not decorate.

## Quality bar

- `bun run test` (vitest): pure-logic tests only — reducers, parsers (ANSI,
  diff, fuzzy match), stores. No component/browser tests.
- `bun run check` (svelte-check) passes.
- `mise run build` produces the embedded binary; `mise run dev` gives the
  hot-reload loop against a running Go server.
- Small, composed components; explicit control flow; no clever abstractions.
  Names say what things do. Comments only for constraints the code can't show.

## Definition of done

- Old `web/src` fully replaced; no dead files left.
- Every feature above works end-to-end against the real backend
  (`mise run dev` + a real pi session).
- vitest suite green, svelte-check green, `mise run build` green,
  `go test ./...` still green (you shouldn't have touched Go).
- A 1000-message session scrolls smoothly; a mid-stream SSE reconnect
  re-renders seamlessly; a second browser window stays in sync.
