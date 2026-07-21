package piweb

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// sessionIdleTimeout is how long a pi child with no connected browsers and no
// running turn is kept alive before being reaped. Session state lives in pi's
// JSONL files, so reaping loses nothing.
const sessionIdleTimeout = 30 * time.Minute

// agentState mirrors the fields of pi's get_state response that pi-web needs.
type agentState struct {
	Model          json.RawMessage `json:"model"`
	ThinkingLevel  string          `json:"thinkingLevel"`
	IsStreaming    bool            `json:"isStreaming"`
	SessionFile    string          `json:"sessionFile"`
	SessionID      string          `json:"sessionId"`
	SessionName    string          `json:"sessionName"`
	MessageCount   int             `json:"messageCount"`
	PendingCount   int             `json:"pendingMessageCount"`
	IsCompacting   bool            `json:"isCompacting"`
	SteeringMode   string          `json:"steeringMode"`
	FollowUpMode   string          `json:"followUpMode"`
	AutoCompaction bool            `json:"autoCompactionEnabled"`
}

// session is one live pi RPC child plus the browsers subscribed to it.
type session struct {
	id   string
	file string
	cwd  string

	mu         sync.Mutex
	rpc        *rpcClient
	subs       map[chan []byte]struct{}
	lastActive time.Time
	streaming  bool
	// recycleWhenSettled marks a mid-turn child to be closed once its turn
	// settles, so it respawns on a freshly upgraded pi.
	recycleWhenSettled bool
}

func (s *session) subscribe() chan []byte {
	ch := make(chan []byte, 256)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.lastActive = time.Now()
	s.mu.Unlock()
	return ch
}

func (s *session) unsubscribe(ch chan []byte) {
	s.mu.Lock()
	if _, ok := s.subs[ch]; ok {
		delete(s.subs, ch)
		close(ch)
	}
	s.lastActive = time.Now()
	s.mu.Unlock()
}

// broadcast fans an event line out to every subscriber. Slow subscribers are
// dropped rather than allowed to stall the pi read loop.
func (s *session) broadcast(raw []byte) {
	var head struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(raw, &head)

	var recycle bool
	s.mu.Lock()
	switch head.Type {
	case "agent_start", "turn_start":
		s.streaming = true
	// agent_settled is the primary settle signal; agent_end and turn_end are
	// accepted as cheap skew insurance against pi renaming the event.
	case "agent_settled", "agent_end", "turn_end":
		s.streaming = false
		if s.recycleWhenSettled {
			s.recycleWhenSettled = false
			recycle = true
		}
	}
	s.lastActive = time.Now()
	for ch := range s.subs {
		select {
		case ch <- raw:
		default:
			delete(s.subs, ch)
			close(ch)
		}
	}
	rpc := s.rpc
	s.mu.Unlock()

	// Close after releasing the lock and after subscribers have seen the
	// settle event: their SSE streams drop, the browser reconnects, and the
	// supervisor respawns the session on the upgraded pi. Closing runs off the
	// read-loop goroutine (broadcast is called on it) to avoid deadlock.
	if recycle && rpc != nil {
		go rpc.close()
	}
}

func (s *session) touch() {
	s.mu.Lock()
	s.lastActive = time.Now()
	s.mu.Unlock()
}

func (s *session) idleSince() (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idle := len(s.subs) == 0 && !s.streaming
	return s.lastActive, idle
}

// flagSupporter reports whether the installed pi accepts a given CLI flag.
// piManager implements it; supervisor depends on this seam, not the concrete
// type, so it can spawn pi without an optional flag the installed pi lacks.
type flagSupporter interface {
	supportsFlag(flag string) bool
}

// supervisor owns the set of live pi children, keyed by pi session id.
type supervisor struct {
	cfg Config
	pi  flagSupporter

	mu       sync.Mutex
	sessions map[string]*session
	// liveCh is closed and replaced whenever a child spawns, so cold event
	// streams can promote themselves to live without polling.
	liveCh chan struct{}

	stop chan struct{}
	wg   sync.WaitGroup
}

func newSupervisor(cfg Config) *supervisor {
	sv := &supervisor{
		cfg:      cfg,
		sessions: make(map[string]*session),
		liveCh:   make(chan struct{}),
		stop:     make(chan struct{}),
	}
	sv.wg.Add(1)
	go sv.reapLoop()
	return sv
}

// piCommand assembles the child argv. Project-local files are trusted with
// --approve: the workspace belongs to the same VM user pi runs as, and a
// silent default-deny would ignore workspace AGENTS.md and extensions. When
// the installed pi is too old to know --approve, it is omitted rather than
// passed and rejected — version skew degrades, it never takes a session down.
func (sv *supervisor) piCommand(sessionRef string) []string {
	cmd := append([]string{}, sv.cfg.PiCommand...)
	cmd = append(cmd, "--mode", "rpc")
	if sv.pi == nil || sv.pi.supportsFlag("approve") {
		cmd = append(cmd, "--approve")
	}
	if sv.cfg.SessionDir != "" {
		cmd = append(cmd, "--session-dir", sv.cfg.SessionDir)
	}
	if sessionRef != "" {
		cmd = append(cmd, "--session", sessionRef)
	}
	return cmd
}

// spawn starts a pi child in workDir and resolves its identity via get_state.
// An empty workDir falls back to the server's configured workspace.
func (sv *supervisor) spawn(ctx context.Context, sessionRef, workDir string) (*session, error) {
	if workDir == "" {
		workDir = sv.cfg.Workspace
	}
	s := &session{
		subs:       make(map[chan []byte]struct{}),
		lastActive: time.Now(),
		cwd:        workDir,
	}
	rpc, err := startRPCClient(sv.piCommand(sessionRef), workDir, os.Environ(), s.broadcast)
	if err != nil {
		return nil, err
	}
	s.rpc = rpc

	stateCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var st agentState
	if err := rpc.call(stateCtx, map[string]any{"type": "get_state"}, &st); err != nil {
		rpc.close()
		return nil, fmt.Errorf("query pi session state: %w", err)
	}
	if st.SessionID == "" {
		rpc.close()
		return nil, fmt.Errorf("pi reported no session id (is session persistence disabled?)")
	}
	s.id = st.SessionID
	s.file = st.SessionFile
	s.mu.Lock()
	s.streaming = st.IsStreaming
	s.mu.Unlock()
	return s, nil
}

// create starts a brand-new session in cwd (empty cwd uses the workspace).
func (sv *supervisor) create(ctx context.Context, cwd string) (*session, error) {
	s, err := sv.spawn(ctx, "", cwd)
	if err != nil {
		return nil, err
	}
	sv.mu.Lock()
	sv.sessions[s.id] = s
	sv.signalLiveLocked()
	sv.mu.Unlock()
	return s, nil
}

// lookup returns the live session for id, or nil — it never spawns one.
func (sv *supervisor) lookup(id string) *session {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	if s, ok := sv.sessions[id]; ok && s.rpc.alive() {
		return s
	}
	return nil
}

// liveSignal returns a channel that is closed the next time any session goes
// live. Waiters re-fetch the channel and re-check lookup after each close.
func (sv *supervisor) liveSignal() <-chan struct{} {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	return sv.liveCh
}

// signalLiveLocked wakes cold event streams after a session is registered.
// Callers hold sv.mu.
func (sv *supervisor) signalLiveLocked() {
	close(sv.liveCh)
	sv.liveCh = make(chan struct{})
}

// get returns the live session for id, starting a pi child resuming that
// session if none is running. The id is a pi session UUID; pi resolves it to
// the stored JSONL file.
func (sv *supervisor) get(ctx context.Context, id string) (*session, error) {
	sv.mu.Lock()
	s, ok := sv.sessions[id]
	sv.mu.Unlock()
	if ok && s.rpc.alive() {
		return s, nil
	}

	// Resume by file path when we can find it: pi's bare-id lookup misses
	// legacy per-project layouts, and a path avoids the interactive
	// "fork into current directory?" prompt for project-scoped sessions.
	ref := id
	workDir := ""
	if path, cwd, found := sessionFileByID(sv.cfg.SessionDir, id); found {
		ref = path
		if isDir(cwd) {
			workDir = cwd
		}
	}

	s, err := sv.spawn(ctx, ref, workDir)
	if err != nil {
		return nil, err
	}
	if s.id != id {
		s.rpc.close()
		return nil, fmt.Errorf("pi resolved session %q to %q; refusing mismatched resume", id, s.id)
	}
	sv.mu.Lock()
	if existing, ok := sv.sessions[id]; ok && existing.rpc.alive() {
		sv.mu.Unlock()
		s.rpc.close()
		return existing, nil
	}
	sv.sessions[id] = s
	sv.signalLiveLocked()
	sv.mu.Unlock()
	return s, nil
}

// isDir reports whether path exists and is a directory.
func isDir(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// live reports the session ids with a running pi child.
func (sv *supervisor) live() map[string]bool {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	out := make(map[string]bool, len(sv.sessions))
	for id, s := range sv.sessions {
		if s.rpc.alive() {
			out[id] = true
		}
	}
	return out
}

func (sv *supervisor) reapLoop() {
	defer sv.wg.Done()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-sv.stop:
			return
		case <-ticker.C:
			sv.reapIdle()
		}
	}
}

func (sv *supervisor) reapIdle() {
	sv.mu.Lock()
	var victims []*session
	for id, s := range sv.sessions {
		if !s.rpc.alive() {
			delete(sv.sessions, id)
			continue
		}
		if last, idle := s.idleSince(); idle && time.Since(last) > sessionIdleTimeout {
			victims = append(victims, s)
			delete(sv.sessions, id)
		}
	}
	sv.mu.Unlock()
	for _, s := range victims {
		s.rpc.close()
	}
}

// recycleIdle closes every child that is not mid-turn so it respawns on a
// freshly upgraded pi; children mid-turn are flagged to recycle when their
// turn settles. Called after a successful pi upgrade.
func (sv *supervisor) recycleIdle() {
	sv.mu.Lock()
	var victims []*session
	for id, s := range sv.sessions {
		if !s.rpc.alive() {
			delete(sv.sessions, id)
			continue
		}
		s.mu.Lock()
		if s.streaming {
			s.recycleWhenSettled = true
			s.mu.Unlock()
			continue
		}
		s.mu.Unlock()
		victims = append(victims, s)
		delete(sv.sessions, id)
	}
	sv.mu.Unlock()
	for _, s := range victims {
		s.rpc.close()
	}
}

// closeAll terminates every child; used on shutdown.
func (sv *supervisor) closeAll() {
	close(sv.stop)
	sv.wg.Wait()
	sv.mu.Lock()
	sessions := make([]*session, 0, len(sv.sessions))
	for id, s := range sv.sessions {
		sessions = append(sessions, s)
		delete(sv.sessions, id)
	}
	sv.mu.Unlock()
	for _, s := range sessions {
		s.rpc.close()
	}
}
