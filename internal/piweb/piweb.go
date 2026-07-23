// Package piweb implements pi-web: a small HTTP server that serves an
// embedded chat UI and drives the pi coding agent through its RPC mode
// (`pi --mode rpc`, LF-delimited JSONL over stdio).
//
// pi-web holds no conversation state of its own. Sessions are pi's JSONL
// files under the pi session directory; the same sessions are resumable from
// the pi CLI in a terminal. pi-web binds loopback and trusts its callers —
// authentication is the job of whatever fronts it (a reverse proxy, an SSH
// tunnel, or nothing on a single-user machine).
package piweb

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/khangkontum/pi-web/internal/dtach"
)

// Version identifies the pi-web build; it is reported by /version and by
// --version. Release builds override it via
// -ldflags "-X github.com/khangkontum/pi-web/internal/piweb.Version=v1.2.3".
var Version = "dev"

// DefaultAddr is the default loopback listen address.
const DefaultAddr = "127.0.0.1:9999"

// Config carries the runtime settings for the pi-web server.
type Config struct {
	Addr       string
	Workspace  string
	SessionDir string
	// PiCommand is the base argv for the pi coding agent; supervisor flags
	// (--mode rpc, --session, ...) are appended.
	PiCommand []string
	Version   string
	// UpdateURL is the release.json endpoint polled for new versions.
	UpdateURL string
	// UpdateInterval is the update check cadence; 0 disables the background
	// check loop (manual checks via the API still work).
	UpdateInterval time.Duration
	// AutoUpdate seeds whether a newer release is applied automatically. The
	// persisted preference in SettingsPath overrides it when present.
	AutoUpdate bool
	// SettingsPath is where app preferences are persisted; empty keeps choices
	// in memory only.
	SettingsPath string
	// TerminalDir holds private-terminal records and sockets (process
	// metadata for reattaching to detached shells, not session state); empty
	// disables the terminal feature.
	TerminalDir string
}

// Main is the pi-web entry point. It returns a process exit code.
func Main(args []string, stdout, stderr io.Writer) int {
	// Hidden subcommand: `pi-web dtach serve ...` is the detached child that
	// hosts one private terminal's PTY. Not part of the public CLI surface.
	if len(args) > 0 && args[0] == "dtach" {
		return dtachMain(args[1:], stderr)
	}

	fs := flag.NewFlagSet("pi-web", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", DefaultAddr, "listen address (keep on loopback unless a trusted proxy fronts it)")
	workspace := fs.String("workspace", "", "agent working directory (default: current directory)")
	sessionDir := fs.String("session-dir", "", "pi session storage directory (default: ~/.pi/agent/sessions)")
	piBin := fs.String("pi-bin", "pi", "pi coding agent binary")
	updateURL := fs.String("update-url", DefaultUpdateURL, "release metadata URL polled for updates")
	updateInterval := fs.Duration("update-interval", DefaultUpdateInterval, "update check interval (0 disables the background check loop)")
	autoUpdate := fs.Bool("auto-update", false, "apply newer releases automatically (seeds the persisted UI toggle)")
	showVersion := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintf(stdout, "pi-web %s\n", Version)
		return 0
	}

	cfg := Config{
		Addr:           *addr,
		Workspace:      *workspace,
		PiCommand:      []string{*piBin},
		Version:        Version,
		UpdateURL:      *updateURL,
		UpdateInterval: *updateInterval,
		AutoUpdate:     *autoUpdate,
		SettingsPath:   defaultSettingsPath(),
		TerminalDir:    defaultTerminalDir(),
	}
	if cfg.Workspace == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(stderr, "pi-web: resolve working directory: %v\n", err)
			return 1
		}
		cfg.Workspace = wd
	}
	cfg.SessionDir = *sessionDir
	if cfg.SessionDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(stderr, "pi-web: resolve home directory: %v\n", err)
			return 1
		}
		cfg.SessionDir = filepath.Join(home, ".pi", "agent", "sessions")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := Run(ctx, cfg, stderr); err != nil {
		fmt.Fprintf(stderr, "pi-web: %v\n", err)
		return 1
	}
	return 0
}

// defaultTerminalDir keeps terminal process metadata next to settings.json;
// short paths matter (Unix socket sun_path is 104 bytes on darwin).
func defaultTerminalDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "pi-web", "terminals")
}

// dtachMain implements `pi-web dtach serve -s SOCK -cwd DIR -cols N -rows N
// -- argv...`: the blocking PTY host re-exec'd as a detached child.
func dtachMain(args []string, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "serve" {
		fmt.Fprintln(stderr, "usage: pi-web dtach serve -s SOCKET [-cwd DIR] [-cols N] [-rows N] -- CMD [ARGS...]")
		return 2
	}
	fs := flag.NewFlagSet("pi-web dtach serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	socket := fs.String("s", "", "unix socket path")
	cwd := fs.String("cwd", "", "working directory")
	cols := fs.Uint("cols", 80, "initial columns")
	rows := fs.Uint("rows", 24, "initial rows")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	argv := fs.Args()
	if *socket == "" || len(argv) == 0 {
		fmt.Fprintln(stderr, "pi-web dtach serve: -s and a command are required")
		return 2
	}
	env := append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	err := dtach.Serve(dtach.ServerOptions{
		SocketPath: *socket,
		Command:    argv[0],
		Args:       argv[1:],
		Dir:        *cwd,
		Env:        env,
		Cols:       uint16(*cols),
		Rows:       uint16(*rows),
	})
	if err != nil {
		fmt.Fprintf(stderr, "pi-web dtach: %v\n", err)
		return 1
	}
	return 0
}

// Run serves pi-web until ctx is cancelled.
func Run(ctx context.Context, cfg Config, logw io.Writer) error {
	sv := newSupervisor(cfg)
	defer sv.closeAll()

	upd := newUpdater(cfg, logw)
	if !upd.canUpdate() {
		fmt.Fprintf(logw, "pi-web: self-update disabled for %s build\n", cfg.Version)
	} else if cfg.UpdateInterval > 0 {
		go upd.run(ctx)
	}

	// piManager keeps the installed pi current and tells the supervisor which
	// optional flags pi supports. Link the two, probe once at boot, then run
	// the background version check.
	pi := newPiManager(cfg, logw)
	sv.pi = pi
	pi.recycle = sv.recycleIdle
	pi.bootProbe(ctx)
	if cfg.UpdateInterval > 0 {
		go pi.run(ctx)
	}

	// Private terminals are detached children; the manager only rediscovers
	// them. A failure here degrades the feature, never the server.
	var tm *terminalManager
	if cfg.TerminalDir != "" {
		var err error
		if tm, err = newTerminalManager(cfg.TerminalDir); err != nil {
			fmt.Fprintf(logw, "pi-web: terminals disabled: %v\n", err)
			tm = nil
		}
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           newServer(cfg, sv, upd, pi, tm),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()
	fmt.Fprintf(logw, "pi-web %s listening on %s (workspace %s)\n", Version, cfg.Addr, cfg.Workspace)

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}
