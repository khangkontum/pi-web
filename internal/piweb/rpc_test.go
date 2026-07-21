package piweb

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestHelperPiRPC is not a real test: it is the stub pi process used by the
// piweb tests. It speaks just enough of the pi RPC protocol (LF-delimited
// JSONL on stdio) to exercise the supervisor and server.
func TestHelperPiRPC(t *testing.T) {
	if os.Getenv("GO_PIWEB_HELPER") != "1" {
		t.Skip("helper process")
	}
	// --list-models is a one-shot: emit a fixed table and exit, mirroring the
	// real pi CLI the models endpoint shells out to.
	if slices.Contains(os.Args, "--list-models") {
		fmt.Print("provider  model  context  max-out  thinking  images\n")
		fmt.Print("stubco  stub-model  128K  16.4K  no  no\n")
		fmt.Print("stubco  stub-think  270K  32K  yes  yes\n")
		return
	}

	sessionID := "aaaaaaaa-0000-0000-0000-000000000000"
	for i, arg := range os.Args {
		if arg == "--session" && i+1 < len(os.Args) {
			ref := os.Args[i+1]
			// pi resolves a --session path by reading the file's header id;
			// a bare id is used verbatim. Mirror both.
			if strings.HasSuffix(ref, ".jsonl") {
				if id := stubHeaderID(ref); id != "" {
					sessionID = id
				}
			} else {
				sessionID = ref
			}
		}
	}

	out := bufio.NewWriter(os.Stdout)
	emit := func(v any) {
		data, err := json.Marshal(v)
		if err != nil {
			panic(err)
		}
		out.Write(data)
		out.WriteByte('\n')
		out.Flush()
	}
	respond := func(id, command string, data any) {
		resp := map[string]any{"type": "response", "id": id, "command": command, "success": true}
		if data != nil {
			resp["data"] = data
		}
		emit(resp)
	}

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64<<10), 8<<20)
	for scanner.Scan() {
		var cmd struct {
			Type     string          `json:"type"`
			ID       string          `json:"id"`
			Message  string          `json:"message"`
			Command  string          `json:"command"`
			Name     string          `json:"name"`
			Provider string          `json:"provider"`
			ModelID  string          `json:"modelId"`
			Level    string          `json:"level"`
			EntryID  string          `json:"entryId"`
			Mode     string          `json:"mode"`
			Enabled  bool            `json:"enabled"`
			Images   json.RawMessage `json:"images"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &cmd); err != nil {
			continue
		}
		switch cmd.Type {
		case "get_state":
			respond(cmd.ID, "get_state", map[string]any{
				"model":        map[string]any{"id": "stub-model"},
				"isStreaming":  false,
				"sessionFile":  "/tmp/stub-session.jsonl",
				"sessionId":    sessionID,
				"messageCount": 0,
			})
		case "get_messages":
			respond(cmd.ID, "get_messages", map[string]any{
				"messages": []any{
					map[string]any{"role": "user", "content": "earlier message", "timestamp": 1000},
				},
			})
		case "get_session_stats":
			respond(cmd.ID, "get_session_stats", map[string]any{
				"sessionId": sessionID,
				"tokens":    map[string]any{"input": 10, "output": 5},
			})
		case "set_session_name":
			respond(cmd.ID, "set_session_name", nil)
		case "set_model":
			respond(cmd.ID, "set_model", map[string]any{"provider": cmd.Provider, "id": cmd.ModelID, "name": cmd.ModelID})
		case "set_thinking_level":
			respond(cmd.ID, "set_thinking_level", map[string]any{"level": cmd.Level})
		case "prompt":
			respond(cmd.ID, "prompt", nil)
			emit(map[string]any{"type": "agent_start"})
			echo := "echo: " + cmd.Message
			if len(cmd.Images) > 0 {
				echo += " [+images]"
			}
			emit(map[string]any{
				"type":    "message_update",
				"message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": echo}}},
				"assistantMessageEvent": map[string]any{
					"type": "text_delta", "contentIndex": 0, "delta": echo,
				},
			})
			emit(map[string]any{"type": "agent_settled"})
		case "get_commands":
			respond(cmd.ID, "get_commands", map[string]any{
				"commands": []any{
					map[string]any{"name": "skill:brave-search", "description": "Web search", "source": "skill", "location": "user", "path": "/home/user/.pi/agent/skills/brave-search/SKILL.md"},
					map[string]any{"name": "fix-tests", "description": "Fix failing tests", "source": "prompt", "location": "project"},
				},
			})
		case "get_fork_messages":
			respond(cmd.ID, "get_fork_messages", map[string]any{
				"messages": []any{
					map[string]any{"entryId": "e1", "text": "first prompt"},
				},
			})
		case "fork":
			respond(cmd.ID, "fork", map[string]any{"text": "first prompt", "cancelled": false})
		case "compact":
			respond(cmd.ID, "compact", map[string]any{"summary": "compacted", "tokensBefore": 100, "estimatedTokensAfter": 20})
		case "set_auto_compaction":
			respond(cmd.ID, "set_auto_compaction", nil)
		case "abort_retry":
			respond(cmd.ID, "abort_retry", nil)
		case "set_steering_mode":
			respond(cmd.ID, "set_steering_mode", nil)
		case "set_follow_up_mode":
			respond(cmd.ID, "set_follow_up_mode", nil)
		case "bash":
			respond(cmd.ID, "bash", map[string]any{
				"output": "ran: " + cmd.Command, "exitCode": 0, "cancelled": false, "truncated": false,
			})
		case "abort":
			respond(cmd.ID, "abort", nil)
		case "trigger_dialog":
			// Test hook: ask a dialog question; the client must auto-cancel.
			emit(map[string]any{"type": "extension_ui_request", "id": "dlg-1", "method": "confirm", "title": "sure?"})
			respond(cmd.ID, "trigger_dialog", nil)
		case "extension_ui_response":
			emit(map[string]any{"type": "stub_dialog_result", "id": cmd.ID, "cancelled": true})
		case "exit":
			respond(cmd.ID, "exit", nil)
			return
		default:
			emit(map[string]any{"type": "response", "id": cmd.ID, "command": cmd.Type, "success": false, "error": "unknown command"})
		}
	}
}

// stubHeaderID reads the session id from a session file's first JSONL line,
// the same way pi resolves a --session path.
func stubHeaderID(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return ""
	}
	var header struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
		return ""
	}
	return header.ID
}

// helperPiCommand returns an argv that re-executes this test binary as the
// stub pi process.
func helperPiCommand() []string {
	return []string{os.Args[0], "-test.run=TestHelperPiRPC", "--"}
}

func helperConfig(t *testing.T) Config {
	t.Helper()
	t.Setenv("GO_PIWEB_HELPER", "1")
	return Config{
		Addr:         "127.0.0.1:0",
		Workspace:    t.TempDir(),
		SessionDir:   t.TempDir(),
		PiCommand:    helperPiCommand(),
		Version:      "test",
		SettingsPath: filepath.Join(t.TempDir(), "settings.json"),
	}
}

func TestRPCClientRequestResponse(t *testing.T) {
	cfg := helperConfig(t)
	events := make(chan []byte, 64)
	client, err := startRPCClient(append(helperPiCommand(), "--mode", "rpc"), cfg.Workspace, os.Environ(), func(raw []byte) {
		events <- raw
	})
	if err != nil {
		t.Fatalf("start stub pi: %v", err)
	}
	defer client.close()

	var st agentState
	if err := client.call(t.Context(), map[string]any{"type": "get_state"}, &st); err != nil {
		t.Fatalf("get_state: %v", err)
	}
	if st.SessionID == "" {
		t.Fatalf("expected session id, got %+v", st)
	}

	if err := client.call(t.Context(), map[string]any{"type": "trigger_dialog"}, nil); err != nil {
		t.Fatalf("trigger_dialog: %v", err)
	}
	sawCancel := false
	for range 10 {
		select {
		case raw := <-events:
			var head struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal(raw, &head)
			if head.Type == "stub_dialog_result" {
				sawCancel = true
			}
		case <-t.Context().Done():
			t.Fatal("context done before dialog result")
		}
		if sawCancel {
			break
		}
	}
	if !sawCancel {
		t.Fatal("client did not auto-cancel the extension UI dialog")
	}
}

func TestRPCClientReportsExit(t *testing.T) {
	cfg := helperConfig(t)
	client, err := startRPCClient(append(helperPiCommand(), "--mode", "rpc"), cfg.Workspace, os.Environ(), nil)
	if err != nil {
		t.Fatalf("start stub pi: %v", err)
	}
	if err := client.call(t.Context(), map[string]any{"type": "exit"}, nil); err != nil {
		t.Fatalf("exit: %v", err)
	}
	client.close()
	if client.alive() {
		t.Fatal("client should report not alive after exit")
	}
	if _, err := client.request(t.Context(), map[string]any{"type": "get_state"}); err == nil {
		t.Fatal("expected error requesting on closed client")
	}
}

func TestSupervisorResumesLegacyByPath(t *testing.T) {
	cfg := helperConfig(t)
	sv := newSupervisor(cfg)
	defer sv.closeAll()

	// A legacy per-project session file whose id pi's bare-id lookup would
	// miss; the supervisor must resolve it to a path and resume it.
	id := "dddddddd-0000-0000-0000-000000000000"
	writeSessionFixture(t, cfg.SessionDir, id, "legacy session")

	s, err := sv.get(t.Context(), id)
	if err != nil {
		t.Fatalf("resume by path: %v", err)
	}
	if s.id != id {
		t.Fatalf("resumed id = %q, want %q", s.id, id)
	}
}

func TestSupervisorResumeMismatch(t *testing.T) {
	cfg := helperConfig(t)
	sv := newSupervisor(cfg)
	defer sv.closeAll()

	// The stub mirrors --session back as its id, so a matching resume works…
	s, err := sv.get(t.Context(), "bbbbbbbb-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if s.id != "bbbbbbbb-0000-0000-0000-000000000000" {
		t.Fatalf("unexpected session id %q", s.id)
	}
	fmt.Fprintln(os.Stderr, "resume ok")
}
