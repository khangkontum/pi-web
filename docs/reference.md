# pi-web reference

Detailed reference for pi-web. The [README](../README.md) covers what it is
and how to run it; everything else lives here.

## Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `--addr` | `127.0.0.1:9999` | Listen address (keep on loopback unless a trusted proxy fronts it) |
| `--workspace` | current directory | Default agent working directory |
| `--session-dir` | `~/.pi/agent/sessions` | pi session storage directory |
| `--pi-bin` | `pi` | pi coding agent binary |
| `--update-url` | latest `release.json` | Release metadata URL polled for updates |
| `--update-interval` | `6h` | Update check interval (`0` disables the background check loop) |
| `--auto-update` | `false` | Apply newer releases automatically (seeds the persisted UI toggle) |
| `--version` | | Print version and exit |

## Security model

pi-web binds loopback and performs no authentication of its own; it trusts
every caller. Anything that can reach the socket can drive the agent — and the
agent can run commands. Expose it only through something that authenticates (a
reverse proxy, an SSH tunnel), or not at all. There are deliberately no
accounts, cookies, or TLS.

## HTTP API

`GET /version` returns `{service, version}`.

| Route | Purpose |
| --- | --- |
| `GET /api/sessions` | List sessions (stored + live) |
| `POST /api/sessions` | Create a session (optional `message`, `name`, `cwd`, `provider`+`modelId`, `thinking`) |
| `GET /api/sessions/{id}/events` | SSE stream of agent events (snapshot includes the session `cwd`) |
| `POST /api/sessions/{id}/message` | Send a message to a session |
| `POST /api/sessions/{id}/abort` | Abort the current turn |
| `POST /api/sessions/{id}/bash` | Run a shell command in the workspace |
| `POST /api/sessions/{id}/model` | Switch the session model (`{provider, modelId}`) |
| `POST /api/sessions/{id}/thinking` | Set reasoning effort (`{level}`: off/minimal/low/medium/high/xhigh) |
| `GET /api/models` | List models pi can use (`?refresh=1` bypasses the cache) |
| `GET /api/dirs` | List subdirectories of `?path=` for the new-session folder picker |
| `GET /api/git` | Git summary of `?base=` (defaults to the workspace) |
| `GET /api/file` | Read a file (relative paths resolve against `?base=`) |
| `GET /api/update` | Update status `{current, latest, available, autoUpdate, canUpdate, checkedAt}` |
| `POST /api/update/check` | Force an update check |
| `POST /api/update/apply` | Install the latest release and restart |
| `POST /api/update/auto` | Toggle auto-update (`{enabled}`, persisted) |

## Self-update

Updates are opt-in. Release builds check `--update-url` on `--update-interval`
and surface availability to the UI, but only *apply* automatically when
auto-update is enabled — via the `--auto-update` flag or the UI toggle.
Otherwise use the UI's **Check** / **Update & restart** buttons, or
`POST /api/update/apply`.

When an update is applied, pi-web downloads the binary for its platform,
verifies its sha256 against `checksums.txt` in memory, renames it over the
running executable (with a non-interactive `sudo` fallback when the install
directory is root-owned), and restarts — via exit under systemd, or by
re-execing itself elsewhere. Dev builds (`go install`, version `dev`) never
self-update. Any failure leaves the installed binary untouched.

The auto-update preference is persisted to `settings.json` under the user
config directory (honouring `XDG_CONFIG_HOME` on Linux) so it survives
restarts. This is the only state pi-web writes of its own.

## Running under systemd

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

## Releases

Tagging `vX.Y.Z` publishes a GitHub release with per-platform binaries,
`checksums.txt`, and `release.json`. The latest release metadata is always at
the stable URL:

```
https://github.com/khangkontum/pi-web/releases/latest/download/release.json
```

`release.json` carries `{version, commit, published_at, checksums_url,
download_urls}` — everything a client needs to check for and verify an update
without touching the GitHub API.
