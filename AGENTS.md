# pi-web Agent Guide

pi-web is a single-binary web front end for the `pi` coding agent: an
embedded chat UI plus a small JSON API. It drives `pi --mode rpc`
(LF-delimited JSONL over stdio) as one child process per live session.

> **Migration status:** the repo is moving from a vanilla-JS UI to the
> Svelte stack described here. Until `plan.md` is fully implemented, some
> sections below describe the target state, not the current tree. `plan.md`
> is the implementation driver; this file is the rulebook.

## Project Layout

- `cmd/pi-web/` — CLI entrypoint; all logic lives in `internal/piweb`.
- `internal/piweb/piweb.go` — config, flags, `Main`/`Run`, the `Version`
  constant.
- `internal/piweb/server.go` — HTTP routes.
- `internal/piweb/supervisor.go` — child-process lifecycle, one pi per live
  session.
- `internal/piweb/rpc.go` — JSONL RPC client over the child's stdio.
- `internal/piweb/sessions.go` — listing and reading pi's JSONL session
  store.
- `internal/piweb/workspace.go` — workspace git/file/bash helpers behind the
  API.
- `internal/piweb/update.go` — pi-web self-update from GitHub releases.
- `internal/piweb/piupdate.go` — pi version management (probe, check,
  upgrade).
- `internal/piweb/ui/dist/` — the **built** UI embedded with
  `go:embed all:ui/dist`. Gitignored except a placeholder; produced by the
  web build.
- `web/` — the UI source: a Vite + Svelte 5 single-page app managed with
  bun. No SvelteKit.

## Core Commands

- Full build: `mise run build` (bun install + vite build into
  `internal/piweb/ui/dist`, then `go build ./...`).
- Go only: `go build ./...` — must always compile and run even when the UI
  has never been built; it then serves a plain "UI not built" page.
- Dev loop: `mise run dev` — runs the Go server (`:9999`) and the Vite dev
  server together; open the Vite URL for hot reload (it proxies `/api` and
  `/version` to the Go server).
- Test: `go test ./...` and `bun run test` (vitest) in `web/`.
- Vet/format: `go vet ./...`; `gofmt -l .` must print nothing.
- Tool versions (Go, bun) are pinned in `mise.toml`.

## Architecture Invariants

Keep these true — they are the product:

- **Stateless by design.** Sessions are pi's JSONL files under the session
  directory. pi-web must never grow its own database, cache file, or
  duplicate session state. If pi-web dies, `pi` in a terminal resumes the
  same sessions untouched. The one sanctioned exception is `settings.go`: a
  single small preference file (auto-update toggles for pi-web and pi)
  under the user config dir. It holds no session state; keep it that
  narrow, and do not use it as a foothold for caches or duplicated pi
  state.
- **Loopback trust model.** pi-web binds loopback and authenticates nobody.
  Do not add auth, TLS, accounts, or session cookies — access control is the
  job of whatever fronts it. Decline features that only make sense with
  authentication. Corollary: pi-web never stores or edits provider API keys
  — key management stays terminal-side.
- **Go module is stdlib only.** Zero third-party Go dependencies. Keep it
  that way unless something is genuinely impossible with the standard
  library, and say so in the PR when it happens.
- **UI is a compiled Svelte app; the Go binary is still the whole product.**
  `web/` builds with bun + Vite into `internal/piweb/ui/dist`, which is
  embedded via `go:embed`. Built output is never committed; CI builds it.
  npm dependencies are pragmatic — use well-maintained libraries where they
  save real work (streamdown, shiki, virtual list) — but remember this UI
  can invoke `/api/.../bash`: rendered model output is attacker-influenced,
  so markdown/HTML rendering must go through streamdown's hardened path and
  nothing may bypass it into `innerHTML`.
- **One child per live session.** The supervisor owns pi processes. The RPC
  client speaks LF-delimited JSON over stdio: responses carry the request
  id, everything else is an event. Event callbacks run on the single read
  loop and must not block. Session switching is done by opening another
  session's child, never via pi's `switch_session`.
- **Self-update never breaks the installed binary.** `update.go` verifies
  the sha256 in memory before touching disk, always applies via `.new` +
  rename (ETXTBSY-safe), uses only `sudo -n` (never prompts), and treats
  any failure as "keep running the current version". Dev builds
  (unparsable version) must never self-update. Updater tests stay hermetic:
  inject `exePath`/`apply`/`restart`, never touch the real executable or
  sudo.
- **pi version skew is eliminated, not tolerated.** pi-web keeps the
  installed pi current rather than accumulating compatibility shims:
  - Boot: probe `pi --help` once, cache the supported flag set. Only pass
    flags (e.g. `--approve`) the installed pi supports.
  - Check: side-effect-free version check against the npm registry
    (`@earendil-works/pi-coding-agent`), compared to `pi --version`.
  - Apply: shell out to `pi update pi` (pi owns its install mechanism —
    never reimplement npm), then re-run `pi --version` and re-probe flags.
  - Degrade, never die: if pi is too old and upgrade fails, spawn without
    the unsupported flag and surface a persistent UI banner with the
    reason. A stale pi must never take every session down.
  - After a successful upgrade, recycle all non-streaming children (their
    SSE streams drop; browsers auto-reconnect onto fresh children).
    Children mid-turn finish on the old binary and are recycled when
    settled.
  - Like the self-updater, all of this is injectable and hermetically
    tested: fake the probe, the registry, and the `pi update` subprocess.

## The HTTP Contract

The browser-facing API is served to the embedded UI (same binary, always
version-matched). There is no separate API version number: the UI ships with
its server, so it cannot mismatch, and no external client is a supported use
case.

- `GET /version` returns `{service, version}`.
- Routes are registered in `newServer` — keep that the single place paths
  are spelled out, and keep the API table in `docs/reference.md` in sync in
  the same change. The README stays thin (what it is + usage); route/contract
  detail lives in `docs/reference.md`.

## The pi RPC Contract

Do not copy pi's protocol documentation into this repo — it skews. The
authoritative spec ships with the installed pi:
`$(npm root -g)/@earendil-works/pi-coding-agent/docs/rpc.md` (plus
`security.md` for the project-trust model behind `--approve`). When
implementing against an event or command, read the shipped doc for the pi
version you target, and code to the newest pi — the updater keeps
deployments current.

## Testing

- Server tests use `net/http/httptest` against `newServer`.
- RPC, supervisor, and updater tests are hermetic: they fake the pi
  process/registry and need no network and no real `pi` binary. Keep new
  tests that way.
- When changing generated responses or SSE framing, add or update a test
  that asserts the exact wire shape.
- UI tests are vitest, pure logic only (parsers, reducers, stores): no
  component or browser tests. The Go wire-shape tests are the contract
  tests.

## Releases

- CI (`.github/workflows/ci.yml`) runs gofmt/vet, the web build (bun
  install + vite build), vitest, and go build/test on pushes and PRs. The
  web build runs before `go build` so the embedded UI is real, not the
  placeholder.
- Tagging `vX.Y.Z` triggers `.github/workflows/release.yml`: the web build
  once (assets are platform-independent), then cross-compiled binaries
  (linux/darwin × amd64/arm64, CGO off), `checksums.txt`, and
  `release.json`, published as GitHub release assets.
- `Version` in `internal/piweb/piweb.go` is a var stamped at release time
  via `-ldflags -X`; dev builds report `dev`. Do not hand-edit it per
  release — the tag is the version.
- `release.json` is the update-check contract (stable URL:
  `releases/latest/download/release.json`). If you change its shape, treat
  it like an API change: additive when possible, and update
  `docs/reference.md`.

## Style

- Go-style simplicity: explicit control flow, small composed pieces, no
  clever abstractions — in Go and in Svelte alike.
- Exported names say what they do; short locals are fine.
- Comments state constraints the code cannot show — nothing else.
- UI: custom-styled controls over native `<select>`/pickers, matching the
  app's look; keep components small and composed.
