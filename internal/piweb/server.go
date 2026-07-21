package piweb

import (
	"context"
	"embed"
	"encoding/base64"
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

	"github.com/khangkontum/pi-web/internal/dtach"
)

//go:embed all:ui/dist
var uiFS embed.FS

// uiNotBuilt is served when the embedded UI has no index.html — i.e. the Go
// binary was built without first building web/ into ui/dist. The Go binary is
// still the whole product; it must run, not panic, in that state.
const uiNotBuilt = `<!doctype html>
<meta charset="utf-8">
<title>pi-web — UI not built</title>
<body style="font:16px system-ui,sans-serif;max-width:40rem;margin:4rem auto;padding:0 1rem">
<h1>UI not built</h1>
<p>This binary was compiled without the web UI. Run <code>mise run build</code>
(or build <code>web/</code> into <code>internal/piweb/ui/dist</code>) and rebuild.</p>
<p>The JSON API under <code>/api</code> and <code>/version</code> still works.</p>
</body>`

// thinkingLevels are the reasoning-effort settings pi accepts, in order.
var thinkingLevels = []string{"off", "minimal", "low", "medium", "high", "xhigh"}

type server struct {
	cfg Config
	sv  *supervisor
	upd *updater
	pi  *piManager
	tm  *terminalManager
	mux *http.ServeMux

	modelsMu    sync.Mutex
	modelsCache []modelInfo
	modelsAt    time.Time
}

func newServer(cfg Config, sv *supervisor, upd *updater, pi *piManager, tm *terminalManager) *server {
	s := &server{cfg: cfg, sv: sv, upd: upd, pi: pi, tm: tm, mux: http.NewServeMux()}

	s.mux.Handle("GET /", uiHandler())

	s.mux.HandleFunc("GET /version", s.handleVersion)
	s.mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	s.mux.HandleFunc("POST /api/sessions", s.handleCreateSession)
	s.mux.HandleFunc("GET /api/sessions/{id}/events", s.handleEvents)
	s.mux.HandleFunc("POST /api/sessions/{id}/message", s.handleMessage)
	s.mux.HandleFunc("POST /api/sessions/{id}/abort", s.handleAbort)
	s.mux.HandleFunc("POST /api/sessions/{id}/bash", s.handleBash)
	s.mux.HandleFunc("POST /api/sessions/{id}/model", s.handleSetModel)
	s.mux.HandleFunc("POST /api/sessions/{id}/thinking", s.handleSetThinking)
	s.mux.HandleFunc("GET /api/sessions/{id}/commands", s.handleCommands)
	s.mux.HandleFunc("GET /api/sessions/{id}/fork-messages", s.handleForkMessages)
	s.mux.HandleFunc("POST /api/sessions/{id}/fork", s.handleFork)
	s.mux.HandleFunc("POST /api/sessions/{id}/compact", s.handleCompact)
	s.mux.HandleFunc("POST /api/sessions/{id}/compaction-auto", s.handleCompactionAuto)
	s.mux.HandleFunc("POST /api/sessions/{id}/retry-abort", s.handleRetryAbort)
	s.mux.HandleFunc("POST /api/sessions/{id}/steering", s.handleSteering)
	s.mux.HandleFunc("POST /api/sessions/{id}/follow-up", s.handleFollowUp)
	s.mux.HandleFunc("GET /api/models", s.handleModels)
	s.mux.HandleFunc("GET /api/dirs", s.handleDirs)
	s.mux.HandleFunc("GET /api/tree", s.handleTree)
	s.mux.HandleFunc("GET /api/files", s.handleFiles)
	s.mux.HandleFunc("GET /api/git", s.handleGit)
	s.mux.HandleFunc("GET /api/git/log", s.handleGitLog)
	s.mux.HandleFunc("GET /api/git/diff", s.handleGitDiff)
	s.mux.HandleFunc("GET /api/file", s.handleFile)
	s.mux.HandleFunc("GET /api/raw", s.handleRaw)
	s.mux.HandleFunc("POST /api/terminals", s.handleTerminalCreate)
	s.mux.HandleFunc("GET /api/terminals", s.handleTerminalList)
	s.mux.HandleFunc("GET /api/terminals/{id}/stream", s.handleTerminalStream)
	s.mux.HandleFunc("POST /api/terminals/{id}/input", s.handleTerminalInput)
	s.mux.HandleFunc("POST /api/terminals/{id}/resize", s.handleTerminalResize)
	s.mux.HandleFunc("DELETE /api/terminals/{id}", s.handleTerminalKill)
	s.mux.HandleFunc("GET /api/update", s.handleUpdateStatus)
	s.mux.HandleFunc("POST /api/update/check", s.handleUpdateCheck)
	s.mux.HandleFunc("POST /api/update/apply", s.handleUpdateApply)
	s.mux.HandleFunc("POST /api/update/auto", s.handleUpdateAuto)
	s.mux.HandleFunc("GET /api/pi", s.handlePiStatus)
	s.mux.HandleFunc("POST /api/pi/check", s.handlePiCheck)
	s.mux.HandleFunc("POST /api/pi/update", s.handlePiUpdate)
	s.mux.HandleFunc("POST /api/pi/auto", s.handlePiAuto)
	return s
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// uiHandler serves the embedded SPA, falling back to the "UI not built" page
// when the dist FS holds only its placeholder (no index.html).
func uiHandler() http.Handler {
	dist, err := fs.Sub(uiFS, "ui/dist")
	if err != nil {
		panic("piweb: embedded ui/dist missing: " + err.Error())
	}
	return uiHandlerFS(dist)
}

func uiHandlerFS(dist fs.FS) http.Handler {
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(uiNotBuilt))
		})
	}
	return http.FileServerFS(dist)
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
		if err := s.sendPrompt(ctx, sess, req.Message, nil); err != nil {
			httpError(w, http.StatusBadGateway, err)
			return
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": sess.id, "file": sess.file})
}

// handleEvents is the SSE stream: a snapshot of the full session first, then
// live pi events, plus synthetic pi-web events (operator bash, stats). A
// session with no running child is served cold — the snapshot is read
// straight from its JSONL file and no pi process is spawned until something
// interacts with the session; the stream then promotes itself to the live
// child and re-snapshots.
func (s *server) handleEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httpError(w, http.StatusBadRequest, fmt.Errorf("missing session id"))
		return
	}
	fl, canFlush := w.(http.Flusher)
	if !canFlush {
		httpError(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}

	// Grab the live signal before checking for a child: a session going live
	// between the two is caught by either the lookup or the signal.
	liveCh := s.sv.liveSignal()
	sess := s.sv.lookup(id)
	if sess == nil {
		if s.serveColdEvents(w, r, fl, id, liveCh) {
			return
		}
		// Not cold-renderable (no stored file, unknown session version,
		// unreadable): resume a child, which handles anything pi can.
		var err error
		sess, err = s.sv.get(r.Context(), id)
		if err != nil {
			httpError(w, http.StatusBadGateway, err)
			return
		}
	}

	writeSSEHeaders(w)
	s.streamSession(w, r, fl, sess)
}

// serveColdEvents renders a stored session from its JSONL file. It reports
// false — before writing anything — when the file cannot be cold-rendered,
// so the caller falls back to spawning. After the cold snapshot the stream
// idles; when the session goes live (first message, fork, operator bash) it
// re-snapshots from the child and continues live on the same connection.
func (s *server) serveColdEvents(w http.ResponseWriter, r *http.Request, fl http.Flusher, id string, liveCh <-chan struct{}) bool {
	path, _, ok := sessionFileByID(s.cfg.SessionDir, id)
	if !ok {
		return false
	}
	snapshot, err := readColdSnapshot(path)
	if err != nil {
		return false
	}

	writeSSEHeaders(w)
	writeSSE(w, "snapshot", snapshot)
	fl.Flush()

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return true
		case <-heartbeat.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		case <-liveCh:
			liveCh = s.sv.liveSignal()
			if sess := s.sv.lookup(id); sess != nil {
				s.streamSession(w, r, fl, sess)
				return true
			}
		}
	}
}

// streamSession snapshots a live session and forwards its events until the
// client disconnects or the subscription is dropped.
func (s *server) streamSession(w http.ResponseWriter, r *http.Request, fl http.Flusher, sess *session) {
	sub := sess.subscribe()
	defer sess.unsubscribe(sub)

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

func writeSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
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

// promptImage is one attached image in a message, forwarded to pi's prompt
// command as an ImageContent block.
type promptImage struct {
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

func (s *server) handleMessage(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.session(w, r)
	if !ok {
		return
	}
	var req struct {
		Message string        `json:"message"`
		Images  []promptImage `json:"images"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Message == "" && len(req.Images) == 0 {
		httpError(w, http.StatusBadRequest, fmt.Errorf("empty message"))
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
	defer cancel()
	if err := s.sendPrompt(ctx, sess, req.Message, req.Images); err != nil {
		httpError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

// sendPrompt delivers a prompt, steering it into the queue when the agent is
// mid-stream instead of failing. Images are forwarded as pi ImageContent
// blocks.
func (s *server) sendPrompt(ctx context.Context, sess *session, message string, images []promptImage) error {
	sess.touch()
	var st agentState
	if err := sess.rpc.call(ctx, map[string]any{"type": "get_state"}, &st); err != nil {
		return err
	}
	cmd := map[string]any{"type": "prompt", "message": message}
	if len(images) > 0 {
		blocks := make([]map[string]any, 0, len(images))
		for _, img := range images {
			blocks = append(blocks, map[string]any{
				"type":     "image",
				"data":     img.Data,
				"mimeType": img.MimeType,
			})
		}
		cmd["images"] = blocks
	}
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

// handleGitLog serves structured commit history for the git overlay's graph
// view; a non-repository base is the normal empty state.
func (s *server) handleGitLog(w http.ResponseWriter, r *http.Request) {
	commits, err := readGitLog(r.Context(), s.base(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"commits": commits})
}

// handleGitDiff serves a unified patch: the working tree (staged, unstaged,
// untracked) with no ?ref=, one commit's patch with ?ref=<hash|HEAD>, or a
// single file's working-tree patch with ?path=<file> (for the file preview).
func (s *server) handleGitDiff(w http.ResponseWriter, r *http.Request) {
	if path := strings.TrimSpace(r.URL.Query().Get("path")); path != "" {
		diff, err := readGitFileDiff(r.Context(), s.base(r), path)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, diff)
		return
	}
	ref := strings.TrimSpace(r.URL.Query().Get("ref"))
	diff, err := readGitDiff(r.Context(), s.base(r), ref)
	if err != nil {
		status := http.StatusNotFound
		if !gitRefPattern.MatchString(ref) {
			status = http.StatusBadRequest
		}
		httpError(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, diff)
}

// terminals gates the private-terminal handlers on the manager existing; it
// is nil when no config directory could be resolved.
func (s *server) terminals(w http.ResponseWriter) (*terminalManager, bool) {
	if s.tm == nil {
		httpError(w, http.StatusServiceUnavailable, errTerminalsDisabled)
		return nil, false
	}
	return s.tm, true
}

// handleTerminalCreate spawns a detached interactive shell. Nothing about
// terminals is ever broadcast to session subscribers: unlike the `!` bash,
// a private terminal stays outside the agent's session context entirely.
func (s *server) handleTerminalCreate(w http.ResponseWriter, r *http.Request) {
	tm, ok := s.terminals(w)
	if !ok {
		return
	}
	var req struct {
		Cwd  string `json:"cwd"`
		Cols uint16 `json:"cols"`
		Rows uint16 `json:"rows"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Cwd) == "" {
		req.Cwd = s.cfg.Workspace
	}
	sess, err := tm.create(req.Cwd, req.Cols, req.Rows)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": sess.ID, "cwd": sess.Cwd, "createdAt": sess.CreatedAt})
}

func (s *server) handleTerminalList(w http.ResponseWriter, r *http.Request) {
	tm, ok := s.terminals(w)
	if !ok {
		return
	}
	type dto struct {
		ID        string    `json:"id"`
		Cwd       string    `json:"cwd"`
		Shell     string    `json:"shell"`
		CreatedAt time.Time `json:"createdAt"`
	}
	out := []dto{}
	for _, t := range tm.list() {
		out = append(out, dto{ID: t.ID, Cwd: t.Cwd, Shell: t.Shell, CreatedAt: t.CreatedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"terminals": out})
}

// handleTerminalStream is the SSE side of a terminal attachment: `attached`,
// then the scrollback `snapshot`, then live `output` (both base64), and
// finally `exit` with the shell's exit code.
func (s *server) handleTerminalStream(w http.ResponseWriter, r *http.Request) {
	tm, ok := s.terminals(w)
	if !ok {
		return
	}
	id := r.PathValue("id")
	dc, err := tm.attach(id)
	if err != nil {
		httpError(w, http.StatusNotFound, err)
		return
	}
	defer dc.Close()
	fl, canFlush := w.(http.Flusher)
	if !canFlush {
		httpError(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	writeSSE(w, "attached", fmt.Appendf(nil, `{"id":%q}`, id))
	fl.Flush()

	type frame struct {
		t       dtach.MsgType
		payload []byte
	}
	frames := make(chan frame, 16)
	go func() {
		defer close(frames)
		for {
			t, p, err := dc.Recv()
			if err != nil {
				return
			}
			frames <- frame{t, p}
			if t == dtach.MsgExit {
				return
			}
		}
	}()

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		case f, ok := <-frames:
			if !ok {
				// Socket died without a clean exit (e.g. the child was
				// SIGKILLed); report it as an exit so the UI settles.
				writeSSE(w, "exit", []byte(`{"code":-1}`))
				fl.Flush()
				return
			}
			switch f.t {
			case dtach.MsgSnapshot, dtach.MsgOutput:
				event := "output"
				if f.t == dtach.MsgSnapshot {
					event = "snapshot"
				}
				data, err := json.Marshal(base64.StdEncoding.EncodeToString(f.payload))
				if err == nil {
					writeSSE(w, event, data)
					fl.Flush()
				}
			case dtach.MsgExit:
				code, _ := dtach.DecodeExit(f.payload)
				writeSSE(w, "exit", fmt.Appendf(nil, `{"code":%d}`, code))
				fl.Flush()
				tm.forget(id)
				return
			}
		}
	}
}

func (s *server) handleTerminalInput(w http.ResponseWriter, r *http.Request) {
	tm, ok := s.terminals(w)
	if !ok {
		return
	}
	var req struct {
		Data string `json:"data"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := tm.input(r.PathValue("id"), []byte(req.Data)); err != nil {
		httpError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) handleTerminalResize(w http.ResponseWriter, r *http.Request) {
	tm, ok := s.terminals(w)
	if !ok {
		return
	}
	var req struct {
		Cols uint16 `json:"cols"`
		Rows uint16 `json:"rows"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Cols == 0 || req.Rows == 0 {
		httpError(w, http.StatusBadRequest, fmt.Errorf("cols and rows must be positive"))
		return
	}
	if err := tm.resize(r.PathValue("id"), req.Cols, req.Rows); err != nil {
		httpError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) handleTerminalKill(w http.ResponseWriter, r *http.Request) {
	tm, ok := s.terminals(w)
	if !ok {
		return
	}
	tm.kill(r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) handleFile(w http.ResponseWriter, r *http.Request) {
	view, err := readFileView(s.base(r), r.URL.Query().Get("path"))
	if err != nil {
		httpError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// handleFiles returns a flat, base-relative file index for a client-side fuzzy
// finder. It prefers `git ls-files` and falls back to a bounded walk; truncated
// reports whether the cap was hit.
func (s *server) handleFiles(w http.ResponseWriter, r *http.Request) {
	files, truncated, err := listFiles(r.Context(), s.base(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files, "truncated": truncated})
}

// handleTree lists the immediate children (dirs and files) of ?path= for the
// file explorer, defaulting to the workspace.
func (s *server) handleTree(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		path = s.cfg.Workspace
	}
	path = filepath.Clean(path)
	entries, err := readTree(path)
	if err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	parent := filepath.Dir(path)
	if parent == path {
		parent = ""
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "parent": parent, "entries": entries})
}

// handleRaw serves a file's raw bytes with a detected content type, for images,
// PDFs, and audio the file viewer cannot render as text. Relative paths resolve
// against ?base=.
func (s *server) handleRaw(w http.ResponseWriter, r *http.Request) {
	raw, err := readRawFile(s.base(r), r.URL.Query().Get("path"))
	if err != nil {
		httpError(w, http.StatusNotFound, err)
		return
	}
	w.Header().Set("Content-Type", raw.ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw.Data)
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

// handleCommands lists the slash commands the session's pi accepts (extension
// commands, prompt templates, skills) via pi's get_commands, for the
// composer's `/` autocomplete.
func (s *server) handleCommands(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.session(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
	defer cancel()
	var data json.RawMessage
	if err := sess.rpc.call(ctx, map[string]any{"type": "get_commands"}, &data); err != nil {
		httpError(w, http.StatusBadGateway, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// handleForkMessages lists the user messages available to fork from, via pi's
// get_fork_messages command.
func (s *server) handleForkMessages(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.session(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
	defer cancel()
	var data json.RawMessage
	if err := sess.rpc.call(ctx, map[string]any{"type": "get_fork_messages"}, &data); err != nil {
		httpError(w, http.StatusBadGateway, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// handleFork forks the session at entryId via pi's fork command, then
// broadcasts a piweb_fork event so other connected browsers refresh.
func (s *server) handleFork(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.session(w, r)
	if !ok {
		return
	}
	var req struct {
		EntryID string `json:"entryId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.EntryID == "" {
		httpError(w, http.StatusBadRequest, fmt.Errorf("entryId is required"))
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
	defer cancel()
	sess.touch()
	var data json.RawMessage
	if err := sess.rpc.call(ctx, map[string]any{"type": "fork", "entryId": req.EntryID}, &data); err != nil {
		httpError(w, http.StatusBadGateway, err)
		return
	}
	if event, err := json.Marshal(map[string]any{"type": "piweb_fork", "entryId": req.EntryID}); err == nil {
		sess.broadcast(event)
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": data})
}

// handleCompact manually compacts the session context via pi's compact command.
func (s *server) handleCompact(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.session(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Minute)
	defer cancel()
	sess.touch()
	var data json.RawMessage
	if err := sess.rpc.call(ctx, map[string]any{"type": "compact"}, &data); err != nil {
		httpError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": data})
}

// handleCompactionAuto toggles pi's automatic compaction.
func (s *server) handleCompactionAuto(w http.ResponseWriter, r *http.Request) {
	s.setEnabled(w, r, "set_auto_compaction")
}

// handleRetryAbort cancels an in-progress auto-retry via pi's abort_retry.
func (s *server) handleRetryAbort(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.session(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
	defer cancel()
	if err := sess.rpc.call(ctx, map[string]any{"type": "abort_retry"}, nil); err != nil {
		httpError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleSteering sets how steering messages are delivered (set_steering_mode).
func (s *server) handleSteering(w http.ResponseWriter, r *http.Request) {
	s.setMode(w, r, "set_steering_mode")
}

// handleFollowUp sets how follow-up messages are delivered (set_follow_up_mode).
func (s *server) handleFollowUp(w http.ResponseWriter, r *http.Request) {
	s.setMode(w, r, "set_follow_up_mode")
}

// setEnabled is the shared body for commands taking a single {enabled} bool.
func (s *server) setEnabled(w http.ResponseWriter, r *http.Request, command string) {
	sess, ok := s.session(w, r)
	if !ok {
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
	defer cancel()
	if err := sess.rpc.call(ctx, map[string]any{"type": command, "enabled": req.Enabled}, nil); err != nil {
		httpError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": req.Enabled})
}

// setMode is the shared body for commands taking a single {mode} string, valid
// values "all" or "one-at-a-time".
func (s *server) setMode(w http.ResponseWriter, r *http.Request, command string) {
	sess, ok := s.session(w, r)
	if !ok {
		return
	}
	var req struct {
		Mode string `json:"mode"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Mode != "all" && req.Mode != "one-at-a-time" {
		httpError(w, http.StatusBadRequest, fmt.Errorf("mode must be \"all\" or \"one-at-a-time\""))
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
	defer cancel()
	if err := sess.rpc.call(ctx, map[string]any{"type": command, "mode": req.Mode}, nil); err != nil {
		httpError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mode": req.Mode})
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

// handlePiStatus returns the installed-pi version state that drives the UI's
// version-skew banner.
func (s *server) handlePiStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.pi.status())
}

func (s *server) handlePiCheck(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 1*time.Minute)
	defer cancel()
	if _, err := s.pi.check(ctx); err != nil {
		httpError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, s.pi.status())
}

// handlePiUpdate upgrades pi via its own installer, re-probes flags, and
// recycles idle children onto the new binary.
func (s *server) handlePiUpdate(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 6*time.Minute)
	defer cancel()
	if err := s.pi.applyUpgrade(ctx); err != nil {
		httpError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, s.pi.status())
}

func (s *server) handlePiAuto(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.pi.setAuto(req.Enabled); err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, s.pi.status())
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
