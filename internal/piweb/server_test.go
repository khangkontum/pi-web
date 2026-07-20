package piweb

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestServer(t *testing.T) (*httptest.Server, Config) {
	t.Helper()
	cfg := helperConfig(t)
	sv := newSupervisor(cfg)
	t.Cleanup(sv.closeAll)
	pi := newTestPiManager(t, cfg, sv)
	ts := httptest.NewServer(newServer(cfg, sv, newUpdater(cfg, testWriter{t}), pi))
	t.Cleanup(ts.Close)
	return ts, cfg
}

// newTestPiManager builds a piManager with fake collaborators (no pi binary,
// no network) reporting a probed pi that supports --approve, wired to recycle
// the supervisor's children.
func newTestPiManager(t *testing.T, cfg Config, sv *supervisor) *piManager {
	t.Helper()
	pi := newPiManager(cfg, testWriter{t})
	pi.probe = func(context.Context) (string, map[string]bool, error) {
		return "0.80.1", map[string]bool{"approve": true, "mode": true}, nil
	}
	pi.registry = func(context.Context) (string, error) { return "0.80.1", nil }
	pi.upgrade = func(context.Context) error { return nil }
	pi.recycle = sv.recycleIdle
	sv.pi = pi
	pi.bootProbe(context.Background())
	return pi
}

func postJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decodeBody(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func TestVersionEndpoint(t *testing.T) {
	ts, _ := newTestServer(t)
	resp, err := http.Get(ts.URL + "/version")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Service string `json:"service"`
		Version string `json:"version"`
	}
	decodeBody(t, resp, &v)
	if v.Service != "pi-web" || v.Version == "" {
		t.Fatalf("unexpected version payload: %+v", v)
	}
}

func TestCreateSessionAndStream(t *testing.T) {
	ts, _ := newTestServer(t)

	resp := postJSON(t, ts.URL+"/api/sessions", map[string]any{"message": "hello"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create session: status %d", resp.StatusCode)
	}
	var created struct {
		ID string `json:"id"`
	}
	decodeBody(t, resp, &created)
	if created.ID == "" {
		t.Fatal("expected a session id")
	}

	stream, err := http.Get(ts.URL + "/api/sessions/" + created.ID + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if ct := stream.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("unexpected content type %q", ct)
	}

	events := readSSE(t, stream, 1, 10*time.Second)
	if len(events) == 0 || events[0].name != "snapshot" {
		t.Fatalf("first event should be snapshot, got %q", events[0].name)
	}
	var snap struct {
		ID       string `json:"id"`
		Messages struct {
			Messages []json.RawMessage `json:"messages"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(events[0].data), &snap); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if snap.ID != created.ID {
		t.Fatalf("snapshot id %q != created id %q", snap.ID, created.ID)
	}
	if len(snap.Messages.Messages) == 0 {
		t.Fatal("snapshot should include message history from the stub")
	}
}

func TestMessageAndBashFlow(t *testing.T) {
	ts, _ := newTestServer(t)

	resp := postJSON(t, ts.URL+"/api/sessions", map[string]any{})
	var created struct {
		ID string `json:"id"`
	}
	decodeBody(t, resp, &created)

	// Subscribe first so the prompt's events and the bash broadcast are seen.
	stream, err := http.Get(ts.URL + "/api/sessions/" + created.ID + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()

	msgResp := postJSON(t, ts.URL+"/api/sessions/"+created.ID+"/message", map[string]any{"message": "do a thing"})
	if msgResp.StatusCode != http.StatusAccepted {
		t.Fatalf("message: status %d", msgResp.StatusCode)
	}
	msgResp.Body.Close()

	bashResp := postJSON(t, ts.URL+"/api/sessions/"+created.ID+"/bash", map[string]any{"command": "uname -a"})
	if bashResp.StatusCode != http.StatusOK {
		t.Fatalf("bash: status %d", bashResp.StatusCode)
	}
	var bashOut struct {
		Result struct {
			Output   string `json:"output"`
			ExitCode int    `json:"exitCode"`
		} `json:"result"`
	}
	decodeBody(t, bashResp, &bashOut)
	if bashOut.Result.Output != "ran: uname -a" || bashOut.Result.ExitCode != 0 {
		t.Fatalf("unexpected bash result: %+v", bashOut.Result)
	}

	// snapshot + agent_start + message_update + agent_settled + piweb_bash
	var sawUpdate, sawBashEvent bool
	events := readSSE(t, stream, 5, 10*time.Second)
	for _, ev := range events {
		if ev.name != "pi" {
			continue
		}
		var head struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal([]byte(ev.data), &head)
		switch head.Type {
		case "message_update":
			sawUpdate = true
		case "piweb_bash":
			sawBashEvent = true
		}
	}
	if !sawUpdate {
		t.Error("expected a message_update event from the prompt")
	}
	if !sawBashEvent {
		t.Error("expected a piweb_bash broadcast event")
	}
}

func TestAbortEndpoint(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := postJSON(t, ts.URL+"/api/sessions", map[string]any{})
	var created struct {
		ID string `json:"id"`
	}
	decodeBody(t, resp, &created)

	abortResp := postJSON(t, ts.URL+"/api/sessions/"+created.ID+"/abort", map[string]any{})
	if abortResp.StatusCode != http.StatusOK {
		t.Fatalf("abort: status %d", abortResp.StatusCode)
	}
	abortResp.Body.Close()
}

func TestListSessionsEndpointMergesLive(t *testing.T) {
	ts, cfg := newTestServer(t)

	resp := postJSON(t, ts.URL+"/api/sessions", map[string]any{})
	var created struct {
		ID string `json:"id"`
	}
	decodeBody(t, resp, &created)

	// The stub does not write a real session file; place one on disk with the
	// live session's id plus one cold session.
	writeSessionFixture(t, cfg.SessionDir, created.ID, "live one")
	writeSessionFixture(t, cfg.SessionDir, "cccccccc-0000-0000-0000-000000000000", "cold one")

	listResp, err := http.Get(ts.URL + "/api/sessions")
	if err != nil {
		t.Fatal(err)
	}
	var list struct {
		Sessions []SessionInfo `json:"sessions"`
	}
	decodeBody(t, listResp, &list)
	if len(list.Sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(list.Sessions))
	}
	byID := map[string]SessionInfo{}
	for _, s := range list.Sessions {
		byID[s.ID] = s
	}
	if !byID[created.ID].Live {
		t.Error("created session should be marked live")
	}
	if byID["cccccccc-0000-0000-0000-000000000000"].Live {
		t.Error("cold session should not be marked live")
	}
}

func TestFileEndpoint(t *testing.T) {
	ts, cfg := newTestServer(t)
	if err := os.WriteFile(filepath.Join(cfg.Workspace, "hello.txt"), []byte("hi there\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(ts.URL + "/api/file?path=hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	var view fileView
	decodeBody(t, resp, &view)
	if view.Content != "hi there\n" || view.Binary {
		t.Fatalf("unexpected view: %+v", view)
	}

	missing, err := http.Get(ts.URL + "/api/file?path=nope.txt")
	if err != nil {
		t.Fatal(err)
	}
	missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("missing file: status %d", missing.StatusCode)
	}
}

func TestModelsEndpoint(t *testing.T) {
	ts, _ := newTestServer(t)
	resp, err := http.Get(ts.URL + "/api/models")
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Models []modelInfo `json:"models"`
	}
	decodeBody(t, resp, &out)
	if len(out.Models) != 2 {
		t.Fatalf("expected 2 models, got %d: %+v", len(out.Models), out.Models)
	}
	if out.Models[0].Model != "stub-model" || out.Models[0].Thinking {
		t.Errorf("unexpected first model: %+v", out.Models[0])
	}
	if out.Models[1].Model != "stub-think" || !out.Models[1].Thinking {
		t.Errorf("unexpected second model: %+v", out.Models[1])
	}
}

func TestSetModelAndThinking(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := postJSON(t, ts.URL+"/api/sessions", map[string]any{})
	var created struct {
		ID string `json:"id"`
	}
	decodeBody(t, resp, &created)

	modelResp := postJSON(t, ts.URL+"/api/sessions/"+created.ID+"/model", map[string]any{"provider": "stubco", "modelId": "stub-think"})
	if modelResp.StatusCode != http.StatusOK {
		t.Fatalf("set model: status %d", modelResp.StatusCode)
	}
	var mOut struct {
		Model struct {
			ID string `json:"id"`
		} `json:"model"`
	}
	decodeBody(t, modelResp, &mOut)
	if mOut.Model.ID != "stub-think" {
		t.Fatalf("model not echoed: %+v", mOut.Model)
	}

	thinkResp := postJSON(t, ts.URL+"/api/sessions/"+created.ID+"/thinking", map[string]any{"level": "high"})
	if thinkResp.StatusCode != http.StatusOK {
		t.Fatalf("set thinking: status %d", thinkResp.StatusCode)
	}
	thinkResp.Body.Close()

	bad := postJSON(t, ts.URL+"/api/sessions/"+created.ID+"/thinking", map[string]any{"level": "bogus"})
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid thinking level: status %d, want 400", bad.StatusCode)
	}
	bad.Body.Close()
}

func TestDirsEndpoint(t *testing.T) {
	ts, cfg := newTestServer(t)
	if err := os.MkdirAll(filepath.Join(cfg.Workspace, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.Workspace, "afile.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(ts.URL + "/api/dirs")
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Path string   `json:"path"`
		Dirs []string `json:"dirs"`
	}
	decodeBody(t, resp, &out)
	if out.Path != cfg.Workspace {
		t.Errorf("path = %q, want workspace %q", out.Path, cfg.Workspace)
	}
	if len(out.Dirs) != 1 || out.Dirs[0] != "alpha" {
		t.Errorf("dirs = %v, want [alpha] (files excluded)", out.Dirs)
	}
}

func TestUpdateStatusAndAuto(t *testing.T) {
	ts, cfg := newTestServer(t)

	resp, err := http.Get(ts.URL + "/api/update")
	if err != nil {
		t.Fatal(err)
	}
	var status struct {
		Current    string `json:"current"`
		AutoUpdate bool   `json:"autoUpdate"`
		CanUpdate  bool   `json:"canUpdate"`
	}
	decodeBody(t, resp, &status)
	if status.Current != "test" || status.CanUpdate {
		t.Fatalf("dev build should not be updatable: %+v", status)
	}
	if status.AutoUpdate {
		t.Fatal("auto-update should default off")
	}

	autoResp := postJSON(t, ts.URL+"/api/update/auto", map[string]any{"enabled": true})
	if autoResp.StatusCode != http.StatusOK {
		t.Fatalf("set auto: status %d", autoResp.StatusCode)
	}
	var after struct {
		AutoUpdate bool `json:"autoUpdate"`
	}
	decodeBody(t, autoResp, &after)
	if !after.AutoUpdate {
		t.Fatal("auto-update not reflected after enabling")
	}
	if s, ok := loadSettings(cfg.SettingsPath); !ok || !s.AutoUpdate {
		t.Fatalf("auto-update preference not persisted: %+v ok=%v", s, ok)
	}

	// Dev builds must refuse to apply.
	applyResp := postJSON(t, ts.URL+"/api/update/apply", map[string]any{})
	if applyResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("apply on dev build: status %d, want 400", applyResp.StatusCode)
	}
	applyResp.Body.Close()
}

func TestMessageWithImages(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := postJSON(t, ts.URL+"/api/sessions", map[string]any{})
	var created struct {
		ID string `json:"id"`
	}
	decodeBody(t, resp, &created)

	stream, err := http.Get(ts.URL + "/api/sessions/" + created.ID + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()

	msgResp := postJSON(t, ts.URL+"/api/sessions/"+created.ID+"/message", map[string]any{
		"message": "look",
		"images":  []map[string]any{{"data": "aGVsbG8=", "mimeType": "image/png"}},
	})
	if msgResp.StatusCode != http.StatusAccepted {
		t.Fatalf("message with images: status %d", msgResp.StatusCode)
	}
	msgResp.Body.Close()

	// The stub appends "[+images]" to its echo when images are present.
	var sawImageEcho bool
	for _, ev := range readSSE(t, stream, 4, 10*time.Second) {
		if ev.name == "pi" && strings.Contains(ev.data, "[+images]") {
			sawImageEcho = true
		}
	}
	if !sawImageEcho {
		t.Error("images were not forwarded to the prompt command")
	}
}

func TestForkMessagesAndFork(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := postJSON(t, ts.URL+"/api/sessions", map[string]any{})
	var created struct {
		ID string `json:"id"`
	}
	decodeBody(t, resp, &created)

	fmResp, err := http.Get(ts.URL + "/api/sessions/" + created.ID + "/fork-messages")
	if err != nil {
		t.Fatal(err)
	}
	var fm struct {
		Messages []struct {
			EntryID string `json:"entryId"`
			Text    string `json:"text"`
		} `json:"messages"`
	}
	decodeBody(t, fmResp, &fm)
	if len(fm.Messages) != 1 || fm.Messages[0].EntryID != "e1" {
		t.Fatalf("unexpected fork messages: %+v", fm.Messages)
	}

	// Subscribe, then fork, and confirm a piweb_fork broadcast.
	stream, err := http.Get(ts.URL + "/api/sessions/" + created.ID + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()

	forkResp := postJSON(t, ts.URL+"/api/sessions/"+created.ID+"/fork", map[string]any{"entryId": "e1"})
	if forkResp.StatusCode != http.StatusOK {
		t.Fatalf("fork: status %d", forkResp.StatusCode)
	}
	forkResp.Body.Close()

	var sawFork bool
	for _, ev := range readSSE(t, stream, 3, 10*time.Second) {
		if ev.name == "pi" && strings.Contains(ev.data, "piweb_fork") {
			sawFork = true
		}
	}
	if !sawFork {
		t.Error("expected a piweb_fork broadcast after forking")
	}

	// Missing entryId is a 400.
	bad := postJSON(t, ts.URL+"/api/sessions/"+created.ID+"/fork", map[string]any{})
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty fork: status %d, want 400", bad.StatusCode)
	}
	bad.Body.Close()
}

func TestCompactAndModeEndpoints(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := postJSON(t, ts.URL+"/api/sessions", map[string]any{})
	var created struct {
		ID string `json:"id"`
	}
	decodeBody(t, resp, &created)
	base := ts.URL + "/api/sessions/" + created.ID

	compactResp := postJSON(t, base+"/compact", map[string]any{})
	if compactResp.StatusCode != http.StatusOK {
		t.Fatalf("compact: status %d", compactResp.StatusCode)
	}
	compactResp.Body.Close()

	for _, ep := range []string{"/compaction-auto"} {
		r := postJSON(t, base+ep, map[string]any{"enabled": true})
		if r.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d", ep, r.StatusCode)
		}
		r.Body.Close()
	}

	retryResp := postJSON(t, base+"/retry-abort", map[string]any{})
	if retryResp.StatusCode != http.StatusOK {
		t.Fatalf("retry-abort: status %d", retryResp.StatusCode)
	}
	retryResp.Body.Close()

	for _, ep := range []string{"/steering", "/follow-up"} {
		ok := postJSON(t, base+ep, map[string]any{"mode": "all"})
		if ok.StatusCode != http.StatusOK {
			t.Fatalf("%s valid mode: status %d", ep, ok.StatusCode)
		}
		ok.Body.Close()
		bad := postJSON(t, base+ep, map[string]any{"mode": "bogus"})
		if bad.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s bad mode: status %d, want 400", ep, bad.StatusCode)
		}
		bad.Body.Close()
	}
}

func TestFilesEndpoint(t *testing.T) {
	ts, cfg := newTestServer(t)
	// A non-git workspace exercises the WalkDir fallback.
	if err := os.MkdirAll(filepath.Join(cfg.Workspace, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.Workspace, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.Workspace, "sub", "b.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cfg.Workspace, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.Workspace, "node_modules", "pkg", "c.txt"), []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(ts.URL + "/api/files")
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Files     []string `json:"files"`
		Truncated bool     `json:"truncated"`
	}
	decodeBody(t, resp, &out)
	got := map[string]bool{}
	for _, f := range out.Files {
		got[f] = true
	}
	if !got["a.txt"] || !got[filepath.Join("sub", "b.txt")] {
		t.Fatalf("expected a.txt and sub/b.txt, got %v", out.Files)
	}
	if got[filepath.Join("node_modules", "pkg", "c.txt")] {
		t.Errorf("node_modules should be skipped, got %v", out.Files)
	}
}

func TestTreeEndpoint(t *testing.T) {
	ts, cfg := newTestServer(t)
	if err := os.MkdirAll(filepath.Join(cfg.Workspace, "zdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.Workspace, "afile.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(ts.URL + "/api/tree")
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Path    string      `json:"path"`
		Entries []treeEntry `json:"entries"`
	}
	decodeBody(t, resp, &out)
	if len(out.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %+v", out.Entries)
	}
	// Directories are listed before files.
	if !out.Entries[0].Dir || out.Entries[0].Name != "zdir" {
		t.Errorf("dirs should come first: %+v", out.Entries)
	}
	if out.Entries[1].Dir || out.Entries[1].Name != "afile.txt" {
		t.Errorf("unexpected second entry: %+v", out.Entries[1])
	}
}

func TestRawEndpoint(t *testing.T) {
	ts, cfg := newTestServer(t)
	// A 1x1 PNG header is enough for content-type detection by extension.
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	if err := os.WriteFile(filepath.Join(cfg.Workspace, "pixel.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(ts.URL + "/api/raw?path=pixel.png")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content type = %q, want image/png", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(body, png) {
		t.Fatalf("raw bytes mismatch: %v", body)
	}

	missing, err := http.Get(ts.URL + "/api/raw?path=nope.png")
	if err != nil {
		t.Fatal(err)
	}
	missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("missing raw file: status %d", missing.StatusCode)
	}
}

func TestPiEndpoints(t *testing.T) {
	ts, cfg := newTestServer(t)

	resp, err := http.Get(ts.URL + "/api/pi")
	if err != nil {
		t.Fatal(err)
	}
	var status struct {
		Current          string `json:"current"`
		ApproveSupported bool   `json:"approveSupported"`
	}
	decodeBody(t, resp, &status)
	if status.Current != "0.80.1" || !status.ApproveSupported {
		t.Fatalf("unexpected pi status: %+v", status)
	}

	checkResp := postJSON(t, ts.URL+"/api/pi/check", map[string]any{})
	if checkResp.StatusCode != http.StatusOK {
		t.Fatalf("pi check: status %d", checkResp.StatusCode)
	}
	checkResp.Body.Close()

	autoResp := postJSON(t, ts.URL+"/api/pi/auto", map[string]any{"enabled": true})
	if autoResp.StatusCode != http.StatusOK {
		t.Fatalf("pi auto: status %d", autoResp.StatusCode)
	}
	autoResp.Body.Close()
	if s, ok := loadSettings(cfg.SettingsPath); !ok || !s.AutoUpdatePi {
		t.Fatalf("pi auto-update not persisted: %+v ok=%v", s, ok)
	}

	updResp := postJSON(t, ts.URL+"/api/pi/update", map[string]any{})
	if updResp.StatusCode != http.StatusOK {
		t.Fatalf("pi update: status %d", updResp.StatusCode)
	}
	updResp.Body.Close()
}

// TestBroadcastStreamingSettleSignals asserts the streaming flag flips on the
// documented settle events plus the skew-insurance aliases.
func TestBroadcastStreamingSettleSignals(t *testing.T) {
	for _, settle := range []string{"agent_settled", "agent_end", "turn_end"} {
		s := &session{subs: map[chan []byte]struct{}{}}
		s.broadcast([]byte(`{"type":"agent_start"}`))
		if !s.streaming {
			t.Fatalf("agent_start should set streaming for %q case", settle)
		}
		s.broadcast([]byte(`{"type":"` + settle + `"}`))
		if s.streaming {
			t.Fatalf("%q should clear streaming", settle)
		}
	}
	// turn_start also marks streaming.
	s := &session{subs: map[chan []byte]struct{}{}}
	s.broadcast([]byte(`{"type":"turn_start"}`))
	if !s.streaming {
		t.Fatal("turn_start should set streaming")
	}
}

type sseEvent struct {
	name string
	data string
}

// readSSE reads up to n events from an SSE response, stopping at the timeout.
func readSSE(t *testing.T, resp *http.Response, n int, timeout time.Duration) []sseEvent {
	t.Helper()
	type result struct {
		events []sseEvent
	}
	done := make(chan result, 1)
	go func() {
		var events []sseEvent
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 64<<10), 8<<20)
		current := sseEvent{}
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				current.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				current.data = strings.TrimPrefix(line, "data: ")
			case line == "" && current.name != "":
				events = append(events, current)
				current = sseEvent{}
				if len(events) >= n {
					done <- result{events}
					return
				}
			}
		}
		done <- result{events}
	}()
	select {
	case r := <-done:
		return r.events
	case <-time.After(timeout):
		// Return what we have; callers assert on contents.
		resp.Body.Close()
		r := <-done
		return r.events
	}
}

func writeSessionFixture(t *testing.T, dir, id, firstMessage string) {
	t.Helper()
	sub := filepath.Join(dir, "--tmp-workspace--")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	header := fmt.Sprintf(`{"type":"session","version":3,"id":%q,"timestamp":"2026-07-18T09:00:00.000Z","cwd":"/tmp/workspace"}`, id)
	msg := fmt.Sprintf(`{"type":"message","id":"e1","parentId":null,"timestamp":"2026-07-18T09:00:01.000Z","message":{"role":"user","content":%q,"timestamp":1752829201000}}`, firstMessage)
	path := filepath.Join(sub, "2026-07-18T09-00-00_"+id+".jsonl")
	if err := os.WriteFile(path, []byte(header+"\n"+msg+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
