# pi-web

A web front end for the `pi` coding agent in one small binary: an embedded
chat UI plus a JSON API, driving `pi --mode rpc` (LF-delimited JSONL over
stdio) as a child process.

pi-web keeps no state of its own. Sessions are pi's JSONL files in the pi
session directory, so anything you start in the browser can be picked up from
`pi` in a terminal, and vice versa.

## Install

Prebuilt binaries for Linux and macOS (amd64/arm64) are on the
[releases page](https://github.com/khangkontum/pi-web/releases):

```bash
curl -Lo pi-web "https://github.com/khangkontum/pi-web/releases/latest/download/pi-web_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')" && chmod +x pi-web
```

Or with Go:

```bash
go install github.com/khangkontum/pi-web/cmd/pi-web@latest
```

Requires the `pi` CLI on `PATH` (or point `--pi-bin` at it).

## Usage

```bash
pi-web --workspace ~/code/myproject
```

Then open http://127.0.0.1:9999 and start a session. Pick a working folder,
model, and reasoning effort from the UI; sessions are shared with the `pi` CLI
in a terminal.

> pi-web binds loopback and authenticates nobody — anything that can reach the
> socket can drive the agent. Expose it only behind something that
> authenticates (a reverse proxy, an SSH tunnel), or not at all.

Run `pi-web --help` for all flags. See [docs/reference.md](docs/reference.md)
for the HTTP API, self-update, deployment, and release details.

## Development

The UI is a Vite + Svelte 5 app in `web/`, built into
`internal/piweb/ui/dist` and embedded with `go:embed`. Build the whole product
with [mise](https://mise.jdx.dev):

```bash
mise run build   # bun install + vite build + go build
mise run dev      # Vite dev server (hot reload), proxying /api to a Go server
mise run test     # go test ./... + vitest
```

`go build ./...` on its own still compiles and runs without ever building the
UI — it then serves a plain "UI not built" page. Tool versions (Go, bun) are
pinned in `mise.toml`.

## License

[MIT](LICENSE)
