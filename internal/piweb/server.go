package piweb

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"time"
)

//go:embed ui
var uiFS embed.FS

// Protocol is bumped when the browser-facing API changes shape; /version
// reports it so clients can check compatibility.
const Protocol = 1

type server struct {
	cfg Config
	sv  *supervisor
	mux *http.ServeMux
}

func newServer(cfg Config, sv *supervisor) *server {
	s := &server{cfg: cfg, sv: sv, mux: http.NewServeMux()}

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
	s.mux.HandleFunc("GET /api/git", s.handleGit)
	s.mux.HandleFunc("GET /api/file", s.handleFile)
	return s
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service":  "pi-web",
		"protocol": Protocol,
		"version":  s.cfg.Version,
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
		Message string `json:"message"`
		Name    string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	sess, err := s.sv.create(r.Context())
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
	info, err := readGitInfo(r.Context(), s.cfg.Workspace)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *server) handleFile(w http.ResponseWriter, r *http.Request) {
	view, err := readFileView(s.cfg.Workspace, r.URL.Query().Get("path"))
	if err != nil {
		httpError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
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
