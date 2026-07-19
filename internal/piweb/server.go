package piweb

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed ui
var uiFS embed.FS

// thinkingLevels are the reasoning-effort settings pi accepts, in order.
var thinkingLevels = []string{"off", "minimal", "low", "medium", "high", "xhigh"}

type server struct {
	cfg Config
	sv  *supervisor
	upd *updater
	mux *http.ServeMux

	modelsMu    sync.Mutex
	modelsCache []modelInfo
	modelsAt    time.Time
}

func newServer(cfg Config, sv *supervisor, upd *updater) *server {
	s := &server{cfg: cfg, sv: sv, upd: upd, mux: http.NewServeMux()}

	ui, err := fs.Sub(uiFS, "ui")
	if err != nil {
		panic("piweb: embedded ui missing: " + err.Error())
	}
	s.mux.Handle("GET /", http.FileServerFS(ui))

	s.mux.HandleFunc("GET /version", s.handleVersion)
	s.mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	s.mux.HandleFunc("POST /api/sessions", s.handleCreateSession)
	s.mux.HandleFunc("GET /api/sessions/{id}/events", s.handleEvents)
	s.mux.HandleFunc("POST /api/sessions/{id}/message", s.handleMessage)
	s.mux.HandleFunc("POST /api/sessions/{id}/abort", s.handleAbort)
	s.mux.HandleFunc("POST /api/sessions/{id}/bash", s.handleBash)
	s.mux.HandleFunc("POST /api/sessions/{id}/model", s.handleSetModel)
	s.mux.HandleFunc("POST /api/sessions/{id}/thinking", s.handleSetThinking)
	s.mux.HandleFunc("GET /api/models", s.handleModels)
	s.mux.HandleFunc("GET /api/dirs", s.handleDirs)
	s.mux.HandleFunc("GET /api/git", s.handleGit)
	s.mux.HandleFunc("GET /api/file", s.handleFile)
	s.mux.HandleFunc("GET /api/update", s.handleUpdateStatus)
	s.mux.HandleFunc("POST /api/update/check", s.handleUpdateCheck)
	s.mux.HandleFunc("POST /api/update/apply", s.handleUpdateApply)
	s.mux.HandleFunc("POST /api/update/auto", s.handleUpdateAuto)
	return s
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "pi-web",
		"version": s.cfg.Version,
	})
}

func (s *server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := listSessions(s.cfg.SessionDir)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	live := s.sv.live()
	for i := range sessions {
		sessions[i].Live = live[sessions[i].ID]
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

func (s *server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Message  string `json:"message"`
		Name     string `json:"name"`
		Cwd      string `json:"cwd"`
		Provider string `json:"provider"`
		ModelID  string `json:"modelId"`
		Thinking string `json:"thinking"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	cwd := strings.TrimSpace(req.Cwd)
	if cwd != "" && !isDir(cwd) {
		httpError(w, http.StatusBadRequest, fmt.Errorf("not a directory: %s", cwd))
		return
	}
	sess, err := s.sv.create(r.Context(), cwd)
	if err != nil {
		httpError(w, http.StatusBadGateway, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
	defer cancel()
	if req.Name != "" {
		if err := sess.rpc.call(ctx, map[string]any{"type": "set_session_name", "name": req.Name}, nil); err != nil {
			httpError(w, http.StatusBadGateway, err)
			return
		}
	}
	if req.Provider != "" && req.ModelID != "" {
		if err := sess.rpc.call(ctx, map[string]any{"type": "set_model", "provider": req.Provider, "modelId": req.ModelID}, nil); err != nil {
			httpError(w, http.StatusBadGateway, err)
			return
		}
	}
	if req.Thinking != "" {
		if !validThinking(req.Thinking) {
			httpError(w, http.StatusBadRequest, fmt.Errorf("invalid thinking level: %s", req.Thinking))
			return
		}
		if err := sess.rpc.call(ctx, map[string]any{"type": "set_thinking_level", "level": req.Thinking}, nil); err != nil {
			httpError(w, http.StatusBadGateway, err)
			return
		}
	}
	if req.Message != "" {
		if err := s.sendPrompt(ctx, sess, req.Message); err != nil {
			httpError(w, http.StatusBadGateway, err)
			return
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": sess.id, "file": sess.file})
}

// handleEvents is the SSE stream: a snapshot of the full session first, then
// live pi events, plus synthetic pi-web events (operator bash, stats).
func (s *server) handleEvents(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.session(w, r)
	if !ok {
		return
	}
	fl, canFlush := w.(http.Flusher)
	if !canFlush {
		httpError(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}

	sub := sess.subscribe()
	defer sess.unsubscribe(sub)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	snapshot, err := s.snapshot(r.Context(), sess)
	if err != nil {
		writeSSE(w, "error", fmt.Appendf(nil, "%q", err.Error()))
		fl.Flush()
		return
	}
	writeSSE(w, "snapshot", snapshot)
	fl.Flush()

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		case raw, ok := <-sub:
			if !ok {
				return
			}
			writeSSE(w, "pi", raw)
			fl.Flush()
		}
	}
}

// snapshot collects state, full message history, and stats in one payload so
// a (re)connecting browser can render the session without replaying events.
func (s *server) snapshot(ctx context.Context, sess *session) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var state json.RawMessage
	if err := sess.rpc.call(ctx, map[string]any{"type": "get_state"}, &state); err != nil {
		return nil, err
	}
	var messages json.RawMessage
	if err := sess.rpc.call(ctx, map[string]any{"type": "get_messages"}, &messages); err != nil {
		return nil, err
	}
	var stats json.RawMessage
	if err := sess.rpc.call(ctx, map[string]any{"type": "get_session_stats"}, &stats); err != nil {
		// Stats are display-only; a session with no assistant turns yet may
		// not have them.
		stats = json.RawMessage("null")
	}
	return json.Marshal(map[string]any{
		"id":       sess.id,
		"cwd":      sess.cwd,
		"state":    state,
		"messages": messages,
		"stats":    stats,
	})
}

func (s *server) handleMessage(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.session(w, r)
	if !ok {
		return
	}
	var req struct {
		Message string `json:"message"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Message == "" {
		httpError(w, http.StatusBadRequest, fmt.Errorf("empty message"))
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
	defer cancel()
	if err := s.sendPrompt(ctx, sess, req.Message); err != nil {
		httpError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

// sendPrompt delivers a prompt, steering it into the queue when the agent is
// mid-stream instead of failing.
func (s *server) sendPrompt(ctx context.Context, sess *session, message string) error {
	sess.touch()
	var st agentState
	if err := sess.rpc.call(ctx, map[string]any{"type": "get_state"}, &st); err != nil {
		return err
	}
	cmd := map[string]any{"type": "prompt", "message": message}
	if st.IsStreaming {
		cmd["streamingBehavior"] = "steer"
	}
	return sess.rpc.call(ctx, cmd, nil)
}

func (s *server) handleAbort(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.session(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
	defer cancel()
	if err := sess.rpc.call(ctx, map[string]any{"type": "abort"}, nil); err != nil {
		httpError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleBash runs an operator `!` command through pi's own bash RPC command,
// so the execution lands in the session context and the agent sees it on the
// next prompt. The result is broadcast to all subscribers as a pi-web event.
func (s *server) handleBash(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.session(w, r)
	if !ok {
		return
	}
	var req struct {
		Command string `json:"command"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Command == "" {
		httpError(w, http.StatusBadRequest, fmt.Errorf("empty command"))
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Minute)
	defer cancel()
	sess.touch()
	var result json.RawMessage
	if err := sess.rpc.call(ctx, map[string]any{"type": "bash", "command": req.Command}, &result); err != nil {
		httpError(w, http.StatusBadGateway, err)
		return
	}
	event, err := json.Marshal(map[string]any{
		"type":    "piweb_bash",
		"command": req.Command,
		"result":  result,
	})
	if err == nil {
		sess.broadcast(event)
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": result})
}

func (s *server) handleGit(w http.ResponseWriter, r *http.Request) {
	info, err := readGitInfo(r.Context(), s.base(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *server) handleFile(w http.ResponseWriter, r *http.Request) {
	view, err := readFileView(s.base(r), r.URL.Query().Get("path"))
	if err != nil {
		httpError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// base returns the directory a git/file request resolves against: the active
// session's cwd (from the ?base= query) when given, else the workspace.
func (s *server) base(r *http.Request) string {
	if b := strings.TrimSpace(r.URL.Query().Get("base")); b != "" {
		return b
	}
	return s.cfg.Workspace
}

// handleModels lists the models pi can use. The result is cached briefly
// because it spawns `pi --list-models`.
func (s *server) handleModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.models(r.Context(), r.URL.Query().Get("refresh") != "")
	if err != nil {
		httpError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

func (s *server) models(ctx context.Context, refresh bool) ([]modelInfo, error) {
	s.modelsMu.Lock()
	fresh := s.modelsCache != nil && time.Since(s.modelsAt) < 5*time.Minute
	cached := s.modelsCache
	s.modelsMu.Unlock()
	if !refresh && fresh {
		return cached, nil
	}
	models, err := listModels(ctx, s.cfg.PiCommand, s.cfg.Workspace)
	if err != nil {
		return nil, err
	}
	s.modelsMu.Lock()
	s.modelsCache = models
	s.modelsAt = time.Now()
	s.modelsMu.Unlock()
	return models, nil
}

// handleSetModel switches the live session's model via pi's set_model RPC and
// broadcasts the change so every connected browser updates.
func (s *server) handleSetModel(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.session(w, r)
	if !ok {
		return
	}
	var req struct {
		Provider string `json:"provider"`
		ModelID  string `json:"modelId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Provider == "" || req.ModelID == "" {
		httpError(w, http.StatusBadRequest, fmt.Errorf("provider and modelId are required"))
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
	defer cancel()
	var model json.RawMessage
	if err := sess.rpc.call(ctx, map[string]any{"type": "set_model", "provider": req.Provider, "modelId": req.ModelID}, &model); err != nil {
		httpError(w, http.StatusBadGateway, err)
		return
	}
	if event, err := json.Marshal(map[string]any{"type": "piweb_model", "model": model}); err == nil {
		sess.broadcast(event)
	}
	writeJSON(w, http.StatusOK, map[string]any{"model": model})
}

// handleSetThinking sets the live session's reasoning effort.
func (s *server) handleSetThinking(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.session(w, r)
	if !ok {
		return
	}
	var req struct {
		Level string `json:"level"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if !validThinking(req.Level) {
		httpError(w, http.StatusBadRequest, fmt.Errorf("invalid thinking level: %s", req.Level))
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
	defer cancel()
	if err := sess.rpc.call(ctx, map[string]any{"type": "set_thinking_level", "level": req.Level}, nil); err != nil {
		httpError(w, http.StatusBadGateway, err)
		return
	}
	if event, err := json.Marshal(map[string]any{"type": "piweb_thinking", "level": req.Level}); err == nil {
		sess.broadcast(event)
	}
	writeJSON(w, http.StatusOK, map[string]any{"level": req.Level})
}

// handleDirs lists immediate subdirectories of a path for the new-session
// folder picker. Under the loopback trust model any readable path is allowed.
func (s *server) handleDirs(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		path = s.cfg.Workspace
	}
	path = filepath.Clean(path)
	entries, err := os.ReadDir(path)
	if err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	dirs := []string{}
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)
	parent := filepath.Dir(path)
	if parent == path {
		parent = ""
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "parent": parent, "dirs": dirs})
}

func (s *server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.upd.status())
}

func (s *server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Minute)
	defer cancel()
	if _, err := s.upd.check(ctx); err != nil {
		httpError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, s.upd.status())
}

// handleUpdateApply installs the latest release, replies, then restarts into
// it. Dev builds and "already current" are refused/reported without touching
// the binary.
func (s *server) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	if !s.upd.canUpdate() {
		httpError(w, http.StatusBadRequest, fmt.Errorf("self-update is disabled for this build"))
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Minute)
	defer cancel()
	version, err := s.upd.installLatest(ctx)
	if err != nil {
		httpError(w, http.StatusBadGateway, err)
		return
	}
	if version == "" {
		writeJSON(w, http.StatusOK, map[string]any{"applied": false, "current": s.upd.version})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"applied": true, "version": version})
	if fl, ok := w.(http.Flusher); ok {
		fl.Flush()
	}
	// Restart after the response has been flushed so the browser sees the ack.
	go func() {
		time.Sleep(500 * time.Millisecond)
		s.upd.restart()
	}()
}

func (s *server) handleUpdateAuto(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.upd.setAuto(req.Enabled); err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, s.upd.status())
}

func validThinking(level string) bool {
	return slices.Contains(thinkingLevels, level)
}

func (s *server) session(w http.ResponseWriter, r *http.Request) (*session, bool) {
	id := r.PathValue("id")
	if id == "" {
		httpError(w, http.StatusBadRequest, fmt.Errorf("missing session id"))
		return nil, false
	}
	sess, err := s.sv.get(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusBadGateway, err)
		return nil, false
	}
	return sess, true
}

func writeSSE(w http.ResponseWriter, event string, data []byte) {
	fmt.Fprintf(w, "event: %s\n", event)
	fmt.Fprint(w, "data: ")
	w.Write(data)
	fmt.Fprint(w, "\n\n")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 10<<20)).Decode(v); err != nil {
		httpError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return false
	}
	return true
}

func httpError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"error": err.Error()})
}
