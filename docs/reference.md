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
| `POST /api/sessions/{id}/message` | Send a message to a session (optional `images: [{data, mimeType}]`, base64) |
| `POST /api/sessions/{id}/abort` | Abort the current turn |
| `POST /api/sessions/{id}/bash` | Run a shell command in the workspace |
| `POST /api/sessions/{id}/model` | Switch the session model (`{provider, modelId}`) |
| `POST /api/sessions/{id}/thinking` | Set reasoning effort (`{level}`: off/minimal/low/medium/high/xhigh) |
| `GET /api/sessions/{id}/fork-messages` | List user messages available to fork from (`get_fork_messages`) |
| `POST /api/sessions/{id}/fork` | Fork the session at `{entryId}`; broadcasts a `piweb_fork` event |
| `POST /api/sessions/{id}/compact` | Manually compact the session context (`compact`) |
| `POST /api/sessions/{id}/compaction-auto` | Toggle automatic compaction (`{enabled}`) |
| `POST /api/sessions/{id}/retry-abort` | Abort an in-progress auto-retry (`abort_retry`) |
| `POST /api/sessions/{id}/steering` | Set steering-message delivery (`{mode}`: all/one-at-a-time) |
| `POST /api/sessions/{id}/follow-up` | Set follow-up-message delivery (`{mode}`: all/one-at-a-time) |
| `GET /api/models` | List models pi can use (`?refresh=1` bypasses the cache) |
| `GET /api/dirs` | List subdirectories of `?path=` for the new-session folder picker |
| `GET /api/tree` | List immediate children (dirs + files) of `?path=` for the file explorer |
| `GET /api/files` | Flat file index of `?base=` for a fuzzy finder (`git ls-files`, bounded walk fallback) |
| `GET /api/git` | Git summary of `?base=` (defaults to the workspace) |
| `GET /api/file` | Read a file as text (relative paths resolve against `?base=`) |
| `GET /api/raw` | Serve a file's raw bytes with a detected content type (`?path=`, relative to `?base=`) |
| `GET /api/update` | pi-web update status `{current, latest, available, autoUpdate, canUpdate, checkedAt}` |
| `POST /api/update/check` | Force a pi-web update check |
| `POST /api/update/apply` | Install the latest pi-web release and restart |
| `POST /api/update/auto` | Toggle pi-web auto-update (`{enabled}`, persisted) |
| `GET /api/pi` | Installed-pi status `{current, latest, available, autoUpdate, approveSupported, checkedAt}` |
| `POST /api/pi/check` | Force a pi version check against the npm registry |
| `POST /api/pi/update` | Upgrade pi via `pi update pi`, re-probe flags, and recycle idle children |
| `POST /api/pi/auto` | Toggle pi auto-upgrade (`{enabled}`, persisted) |

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

The auto-update preferences (`autoUpdate` for pi-web, `autoUpdatePi` for the pi
coding agent) are persisted to `settings.json` under the user config directory
(honouring `XDG_CONFIG_HOME` on Linux) so they survive restarts. This is the
only state pi-web writes of its own.

## pi version management

pi-web eliminates pi version skew rather than tolerating it. At boot it probes
`pi --help` once and caches the supported flag set; the supervisor only passes
optional flags (currently `--approve`) the installed pi understands, so an old
pi degrades — running without the flag and surfacing it via `approveSupported`
in `GET /api/pi` — rather than taking a session down.

`GET /api/pi` also reports whether a newer pi is published: a side-effect-free
check compares `pi --version` to the npm registry's latest
`@earendil-works/pi-coding-agent`. `POST /api/pi/update` (or the auto-upgrade
toggle) shells out to `pi update pi` — pi owns its install mechanism, so pi-web
never reimplements npm — then re-runs `pi --version`, re-probes flags, and
recycles idle children onto the upgraded binary. Children mid-turn finish on the
old binary and recycle when their turn settles. Any failure keeps the current pi
running.

## File endpoints

`GET /api/files` returns a flat, base-relative file index for a client-side
fuzzy finder. It prefers `git ls-files` (respecting `.gitignore`); outside a git
repository it falls back to a bounded `WalkDir` that skips dot-directories and
common heavy directories (`node_modules`, `dist`, `build`, `target`, `vendor`,
`.venv`, `__pycache__`). Both paths cap at 20000 files (`truncated: true` when
hit); the walk fallback also caps at depth 12.

`GET /api/raw` serves a file's raw bytes with a content type resolved from its
extension (falling back to sniffing the first bytes), for images, PDFs, and
audio the text file viewer cannot render. Like `/api/file`, relative paths
resolve against `?base=`; under the loopback trust model any readable path is
allowed.

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
