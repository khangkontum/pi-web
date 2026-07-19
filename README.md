# pi-web

A web front end for the `pi` coding agent in one small binary: an embedded
chat UI plus a JSON API, driving `pi --mode rpc` (LF-delimited JSONL over
stdio) as a child process.

pi-web keeps no state of its own. Sessions are pi's JSONL files in the pi
session directory, so anything you start in the browser can be picked up from
`pi` in a terminal, and vice versa.

## Install

```bash
go install github.com/khangkontum/pi-web/cmd/pi-web@latest
```

Requires the `pi` CLI on `PATH` (or point `--pi-bin` at it).

## Usage

```bash
pi-web --workspace ~/code/myproject
```

Then open http://127.0.0.1:9999.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--addr` | `127.0.0.1:9999` | Listen address (keep on loopback unless a trusted proxy fronts it) |
| `--workspace` | current directory | Agent working directory |
| `--session-dir` | `~/.pi/agent/sessions` | pi session storage directory |
| `--pi-bin` | `pi` | pi coding agent binary |
| `--version` | | Print version and exit |

### Security model

pi-web binds loopback and performs no authentication of its own; it trusts
every caller. Anything that can reach the socket can drive the agent — and
the agent can run commands. Expose it only through something that
authenticates (a reverse proxy, an SSH tunnel), or not at all.

### Running under systemd

```ini
[Unit]
Description=pi-web
After=network-online.target

[Service]
User=you
WorkingDirectory=/home/you/workspace
ExecStart=/usr/local/bin/pi-web --workspace /home/you/workspace
Restart=on-failure
RestartSec=2

[Install]
WantedBy=multi-user.target
```

## HTTP API

`GET /version` returns `{service, protocol, version}`. `protocol` is bumped
whenever the API changes shape; clients should check it before assuming
compatibility.

| Route | Purpose |
| --- | --- |
| `GET /api/sessions` | List sessions (stored + live) |
| `POST /api/sessions` | Create a session, optionally with a first message |
| `GET /api/sessions/{id}/events` | SSE stream of agent events |
| `POST /api/sessions/{id}/message` | Send a message to a session |
| `POST /api/sessions/{id}/abort` | Abort the current turn |
| `POST /api/sessions/{id}/bash` | Run a shell command in the workspace |
| `GET /api/git` | Workspace git summary |
| `GET /api/file` | Read a workspace file |

## Development

```bash
go build ./...
go test ./...
```

The UI is plain HTML/CSS/JS embedded with `go:embed` — there is no build
step. Tool versions are pinned in `mise.toml` for [mise](https://mise.jdx.dev)
users; a plain Go toolchain works the same.

## License

[MIT](LICENSE)
