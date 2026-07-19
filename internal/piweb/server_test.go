package piweb

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
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
	ts := httptest.NewServer(newServer(cfg, sv))
	t.Cleanup(ts.Close)
	return ts, cfg
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
		Service  string `json:"service"`
		Protocol int    `json:"protocol"`
	}
	decodeBody(t, resp, &v)
	if v.Service != "pi-web" || v.Protocol != Protocol {
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
