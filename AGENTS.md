# pi-web Agent Guide

pi-web is a single-binary web front end for the `pi` coding agent: an
embedded chat UI plus a small JSON API. It drives `pi --mode rpc`
(LF-delimited JSONL over stdio) as one child process per live session.

## Project Layout

- `cmd/pi-web/` — CLI entrypoint; all logic lives in `internal/piweb`.
- `internal/piweb/piweb.go` — config, flags, `Main`/`Run`, the `Version`
  constant.
- `internal/piweb/server.go` — HTTP routes and the `Protocol` constant.
- `internal/piweb/supervisor.go` — child-process lifecycle, one pi per live
  session.
- `internal/piweb/rpc.go` — JSONL RPC client over the child's stdio.
- `internal/piweb/sessions.go` — listing and reading pi's JSONL session
  store.
- `internal/piweb/workspace.go` — workspace git/file/bash helpers behind the
  API.
- `internal/piweb/ui/` — embedded static UI (`index.html`, `app.js`,
  `style.css`).

## Core Commands

- Build: `go build ./...`
- Test: `go test ./...`
- Vet: `go vet ./...`
- Format: `gofmt -l .` must print nothing.
- The Go version is pinned in `mise.toml` for mise users; a plain Go
  toolchain of the same version behaves identically.

## Architecture Invariants

Keep these true — they are the product:

- **Stateless by design.** Sessions are pi's JSONL files under the session
  directory. pi-web must never grow its own database, cache file, or
  duplicate session state. If pi-web dies, `pi` in a terminal resumes the
  same sessions untouched.
- **Loopback trust model.** pi-web binds loopback and authenticates nobody.
  Do not add auth, TLS, accounts, or session cookies — access control is the
  job of whatever fronts it. Decline features that only make sense with
  authentication.
- **Stdlib only.** The module has zero third-party dependencies. Keep it
  that way unless something is genuinely impossible with the standard
  library, and say so in the PR when it happens.
- **No UI build step.** `ui/` is plain HTML/CSS/JS embedded with `go:embed`.
  No frameworks, bundlers, or transpilers.
- **One child per live session.** The supervisor owns pi processes. The RPC
  client speaks LF-delimited JSON over stdio: responses carry the request
  id, everything else is an event. Event callbacks run on the single read
  loop and must not block.

## The HTTP Contract

The browser-facing API is a public contract:

- `GET /version` returns `{service, protocol, version}`.
- Bump `Protocol` in `server.go` whenever any route, request, or response
  changes shape. Clients use it to detect incompatibility; a silent shape
  change is a breaking bug.
- Routes are registered in `newServer` — keep that the single place paths
  are spelled out, and keep the README's API table in sync in the same
  change.

## Testing

- Server tests use `net/http/httptest` against `newServer`.
- RPC and supervisor tests are hermetic: they fake the pi process and need
  no network and no real `pi` binary. Keep new tests that way.
- When changing generated responses or SSE framing, add or update a test
  that asserts the exact wire shape.

## Style

- Go-style simplicity: explicit control flow, small composed pieces, no
  clever abstractions.
- Exported names say what they do; short locals are fine.
- Comments state constraints the code cannot show — nothing else.
