package piweb

// Private terminals: interactive shells the agent never sees. Each terminal is
// a detached `pi-web dtach serve` child (Setsid) hosting a PTY behind a Unix
// socket, so shells survive pi-web restarts — including self-update's re-exec.
// The on-disk records here are process metadata (how to find the sockets
// again), not session state; pi's JSONL store is untouched. Lifecycle design
// derived from shelley (github.com/boldsoftware/shelley), Apache License 2.0 —
// see NOTICE.

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/khangkontum/pi-web/internal/dtach"
)

// terminalSession is the on-disk + in-memory record of one detached terminal.
type terminalSession struct {
	ID        string    `json:"id"`
	Cwd       string    `json:"cwd"`
	Shell     string    `json:"shell"`
	Socket    string    `json:"socket"`
	LogFile   string    `json:"logFile"`
	PID       int       `json:"pid"`
	CreatedAt time.Time `json:"createdAt"`
}

// terminalSpawner starts a dtach server hosting shellArgv on the socket. It
// must not return before the socket accepts connections is *not* required —
// callers attach with retry. The default spawns a detached `pi-web dtach
// serve` child; tests swap in an in-process variant (pid 0 = unkillable).
type terminalSpawner func(socket, logFile, cwd string, shellArgv []string, cols, rows uint16) (pid int, err error)

// terminalManager tracks detached terminals on disk and holds one draining
// "writer" attachment per live terminal for the input/resize POST handlers.
type terminalManager struct {
	dir     string
	exe     string
	shell   []string
	spawner terminalSpawner

	mu        sync.Mutex
	terminals map[string]*terminalSession
	writers   map[string]*dtach.Client
}

// newTerminalManager opens (or creates) the terminals directory and drops any
// stale records whose sockets are dead.
func newTerminalManager(dir string) (*terminalManager, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("terminals: mkdir %s: %w", dir, err)
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("terminals: locate pi-web executable: %w", err)
	}
	tm := &terminalManager{
		dir:       dir,
		exe:       exe,
		shell:     defaultShell(),
		terminals: make(map[string]*terminalSession),
		writers:   make(map[string]*dtach.Client),
	}
	tm.spawner = tm.spawnSubprocess
	tm.scan()
	return tm, nil
}

// defaultShell is the user's login shell as an interactive login invocation.
func defaultShell() []string {
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	return []string{sh, "-l"}
}

// scan loads records from disk, dropping any whose dtach socket is dead.
func (t *terminalManager) scan() {
	entries, err := os.ReadDir(t.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(t.dir, e.Name()))
		if err != nil {
			continue
		}
		var s terminalSession
		if err := json.Unmarshal(data, &s); err != nil || s.ID == "" {
			continue
		}
		if !socketAlive(s.Socket) {
			t.removeFiles(s.ID)
			continue
		}
		t.terminals[s.ID] = &s
	}
}

func socketAlive(path string) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	conn, err := net.DialTimeout("unix", path, 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (t *terminalManager) removeFiles(id string) {
	os.Remove(filepath.Join(t.dir, id+".json"))
	os.Remove(filepath.Join(t.dir, id+".sock"))
	os.Remove(filepath.Join(t.dir, id+".log"))
}

// list returns known live terminals, oldest first.
func (t *terminalManager) list() []*terminalSession {
	t.mu.Lock()
	out := make([]*terminalSession, 0, len(t.terminals))
	for _, s := range t.terminals {
		out = append(out, s)
	}
	t.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (t *terminalManager) get(id string) *terminalSession {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.terminals[id]
}

// create spawns a new detached shell in cwd and pins it open with the writer
// attachment (which also carries later input/resize).
func (t *terminalManager) create(cwd string, cols, rows uint16) (*terminalSession, error) {
	if cwd == "" || !isDir(cwd) {
		if wd, err := os.Getwd(); err == nil {
			cwd = wd
		} else {
			cwd = "/"
		}
	}
	id, err := newTerminalID()
	if err != nil {
		return nil, err
	}
	socket := filepath.Join(t.dir, id+".sock")
	logFile := filepath.Join(t.dir, id+".log")

	pid, err := t.spawner(socket, logFile, cwd, t.shell, cols, rows)
	if err != nil {
		return nil, err
	}

	// Attach immediately to pin the session open: while a client is connected
	// Serve won't tear down, even if the shell exits at once.
	dc, err := attachWithRetry(socket, 3*time.Second)
	if err != nil {
		return nil, fmt.Errorf("terminals: attach freshly spawned terminal: %w", err)
	}

	sess := &terminalSession{
		ID:        id,
		Cwd:       cwd,
		Shell:     t.shell[0],
		Socket:    socket,
		LogFile:   logFile,
		PID:       pid,
		CreatedAt: time.Now().UTC(),
	}
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		dc.Close()
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(t.dir, id+".json"), data, 0o600); err != nil {
		dc.Close()
		return nil, err
	}

	t.mu.Lock()
	t.terminals[id] = sess
	t.writers[id] = dc
	t.mu.Unlock()
	go t.drainWriter(id, dc)
	return sess, nil
}

// attach opens a fresh read attachment for an SSE stream. Every attachment
// gets the snapshot replayed, then live output.
func (t *terminalManager) attach(id string) (*dtach.Client, error) {
	s := t.get(id)
	if s == nil {
		return nil, fmt.Errorf("terminals: unknown terminal %q", id)
	}
	return dtach.Attach(s.Socket)
}

// writer returns the shared write attachment for input/resize, dialing one if
// needed (e.g. after a pi-web restart reattached to an existing terminal).
func (t *terminalManager) writer(id string) (*dtach.Client, error) {
	t.mu.Lock()
	if dc, ok := t.writers[id]; ok {
		t.mu.Unlock()
		return dc, nil
	}
	s := t.terminals[id]
	t.mu.Unlock()
	if s == nil {
		return nil, fmt.Errorf("terminals: unknown terminal %q", id)
	}
	dc, err := dtach.Attach(s.Socket)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	if existing, ok := t.writers[id]; ok {
		t.mu.Unlock()
		dc.Close()
		return existing, nil
	}
	t.writers[id] = dc
	t.mu.Unlock()
	go t.drainWriter(id, dc)
	return dc, nil
}

// drainWriter consumes the writer attachment's output so the dtach server's
// fan-out never blocks on it; on exit or socket death the terminal is
// forgotten.
func (t *terminalManager) drainWriter(id string, dc *dtach.Client) {
	for {
		mt, _, err := dc.Recv()
		if err != nil || mt == dtach.MsgExit {
			break
		}
	}
	t.mu.Lock()
	if t.writers[id] == dc {
		delete(t.writers, id)
	}
	alive := socketAlive(t.terminals[id].GetSocket())
	t.mu.Unlock()
	dc.Close()
	if !alive {
		t.forget(id)
	}
}

// input writes keystrokes to the terminal's PTY.
func (t *terminalManager) input(id string, data []byte) error {
	dc, err := t.writer(id)
	if err != nil {
		return err
	}
	return dc.SendInput(data)
}

// resize updates the terminal's PTY window size.
func (t *terminalManager) resize(id string, cols, rows uint16) error {
	dc, err := t.writer(id)
	if err != nil {
		return err
	}
	return dc.SendResize(cols, rows)
}

// kill terminates the terminal's process group and removes its files.
func (t *terminalManager) kill(id string) {
	t.mu.Lock()
	s := t.terminals[id]
	delete(t.terminals, id)
	dc := t.writers[id]
	delete(t.writers, id)
	t.mu.Unlock()
	if dc != nil {
		dc.Close()
	}
	if s == nil {
		return
	}
	if s.PID > 0 {
		// Signal the whole group: dtach server + shell + descendants.
		_ = syscall.Kill(-s.PID, syscall.SIGTERM)
	}
	t.removeFiles(id)
}

// forget drops a terminal from memory and disk without signalling; used once
// the underlying socket is observed dead.
func (t *terminalManager) forget(id string) {
	t.mu.Lock()
	delete(t.terminals, id)
	if dc, ok := t.writers[id]; ok {
		dc.Close()
		delete(t.writers, id)
	}
	t.mu.Unlock()
	t.removeFiles(id)
}

func newTerminalID() (string, error) {
	var b [9]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	out := make([]byte, len(b))
	for i, v := range b {
		out[i] = alphabet[int(v)%len(alphabet)]
	}
	return "t" + string(out), nil
}

// attachWithRetry dials the dtach socket until the deadline; a freshly
// spawned subprocess can race ahead of its accept loop.
func attachWithRetry(socket string, max time.Duration) (*dtach.Client, error) {
	deadline := time.Now().Add(max)
	for {
		dc, err := dtach.Attach(socket)
		if err == nil {
			return dc, nil
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// spawnSubprocess starts a detached `pi-web dtach serve` child (Setsid: its
// own process session) so the terminal survives pi-web exiting or restarting.
func (t *terminalManager) spawnSubprocess(socket, logFile, cwd string, shellArgv []string, cols, rows uint16) (int, error) {
	logF, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, fmt.Errorf("terminals: open log: %w", err)
	}
	defer logF.Close()

	args := []string{
		"dtach", "serve",
		"-s", socket,
		"-cwd", cwd,
		"-cols", fmt.Sprintf("%d", cols),
		"-rows", fmt.Sprintf("%d", rows),
		"--",
	}
	args = append(args, shellArgv...)
	cmd := exec.Command(t.exe, args...)
	cmd.Stdin = nil
	cmd.Stdout = logF
	cmd.Stderr = logF
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("terminals: start dtach: %w", err)
	}
	// Reap in the background so an exiting child never lingers as a zombie;
	// if pi-web exits first the orphan reparents to init, which reaps it.
	pid := cmd.Process.Pid
	go func() { _ = cmd.Wait() }()
	return pid, nil
}

// inProcessSpawner runs the dtach server inside the current process; the
// terminal dies with it. Tests only. The returned pid 0 makes kill a no-op.
func inProcessSpawner(socket, logFile, cwd string, shellArgv []string, cols, rows uint16) (int, error) {
	ready := make(chan struct{})
	go func() {
		_ = dtach.Serve(dtach.ServerOptions{
			SocketPath: socket,
			Command:    shellArgv[0],
			Args:       shellArgv[1:],
			Dir:        cwd,
			Cols:       cols,
			Rows:       rows,
			Ready:      ready,
		})
	}()
	<-ready
	return 0, nil
}

// GetSocket is a nil-tolerant socket accessor for drainWriter's post-death
// check (the record may already be forgotten).
func (s *terminalSession) GetSocket() string {
	if s == nil {
		return ""
	}
	return s.Socket
}

var errTerminalsDisabled = errors.New("terminals unavailable (no config directory)")
