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
}

// Main is the pi-web entry point. It returns a process exit code.
func Main(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pi-web", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", DefaultAddr, "listen address (keep on loopback unless a trusted proxy fronts it)")
	workspace := fs.String("workspace", "", "agent working directory (default: current directory)")
	sessionDir := fs.String("session-dir", "", "pi session storage directory (default: ~/.pi/agent/sessions)")
	piBin := fs.String("pi-bin", "pi", "pi coding agent binary")
	showVersion := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintf(stdout, "pi-web %s (protocol %d)\n", Version, Protocol)
		return 0
	}

	cfg := Config{
		Addr:      *addr,
		Workspace: *workspace,
		PiCommand: []string{*piBin},
		Version:   Version,
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

// Run serves pi-web until ctx is cancelled.
func Run(ctx context.Context, cfg Config, logw io.Writer) error {
	sv := newSupervisor(cfg)
	defer sv.closeAll()

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           newServer(cfg, sv),
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
