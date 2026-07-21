package piweb

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func writeColdFixture(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// assertJSONEqual compares two JSON documents structurally, so key order and
// whitespace do not matter but every value does.
func assertJSONEqual(t *testing.T, got []byte, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("unmarshal got: %v\n%s", err, got)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("unmarshal want: %v", err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("snapshot mismatch\n got: %s\nwant: %s", got, want)
	}
}

// TestColdSnapshotWireShape pins the exact snapshot payload built from a v3
// session file: message entries verbatim, compaction as a compactionSummary
// message, and state/stats folded from the change entries and assistant
// usage. This is the cold twin of the live get_state/get_messages assembly.
func TestColdSnapshotWireShape(t *testing.T) {
	id := "cdcdcdcd-0000-0000-0000-000000000000"
	compactedAt := time.Date(2026, 7, 18, 9, 0, 7, 0, time.UTC)
	path := writeColdFixture(t,
		fmt.Sprintf(`{"type":"session","version":3,"id":%q,"timestamp":"2026-07-18T09:00:00.000Z","cwd":"/tmp/ws"}`, id),
		`{"type":"message","id":"e1","parentId":null,"timestamp":"2026-07-18T09:00:01.000Z","message":{"role":"user","content":"hi","timestamp":1000}}`,
		`{"type":"message","id":"e2","parentId":"e1","timestamp":"2026-07-18T09:00:02.000Z","message":{"role":"assistant","content":[{"type":"text","text":"hello"}],"provider":"stubco","model":"stub-model","usage":{"input":10,"output":5,"cacheRead":2,"cacheWrite":1,"totalTokens":18,"cost":{"total":0.5}},"stopReason":"stop","timestamp":2000}}`,
		`{"type":"message","id":"e3","parentId":"e2","timestamp":"2026-07-18T09:00:03.000Z","message":{"role":"toolResult","toolCallId":"c1","toolName":"bash","content":[{"type":"text","text":"ok"}],"isError":false,"timestamp":3000}}`,
		`{"type":"model_change","id":"e4","parentId":"e3","timestamp":"2026-07-18T09:00:04.000Z","provider":"acme","modelId":"fast-1"}`,
		`{"type":"thinking_level_change","id":"e5","parentId":"e4","timestamp":"2026-07-18T09:00:05.000Z","thinkingLevel":"high"}`,
		`{"type":"session_info","id":"e6","parentId":"e5","timestamp":"2026-07-18T09:00:06.000Z","name":"My Session"}`,
		`{"type":"compaction","id":"e7","parentId":"e6","timestamp":"2026-07-18T09:00:07.000Z","summary":"compacted stuff","firstKeptEntryId":"e2","tokensBefore":50000}`,
		`{"type":"message","id":"e8","parentId":"e7","timestamp":"2026-07-18T09:00:08.000Z","message":{"role":"user","content":"and then","timestamp":8000}}`,
	)

	got, err := readColdSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, got, fmt.Sprintf(`{
		"id": %q,
		"cwd": "/tmp/ws",
		"state": {
			"sessionId": %q,
			"sessionFile": %q,
			"isStreaming": false,
			"isCompacting": false,
			"model": {"provider": "acme", "id": "fast-1"},
			"thinkingLevel": "high",
			"sessionName": "My Session"
		},
		"messages": {"messages": [
			{"role":"user","content":"hi","timestamp":1000},
			{"role":"assistant","content":[{"type":"text","text":"hello"}],"provider":"stubco","model":"stub-model","usage":{"input":10,"output":5,"cacheRead":2,"cacheWrite":1,"totalTokens":18,"cost":{"total":0.5}},"stopReason":"stop","timestamp":2000},
			{"role":"toolResult","toolCallId":"c1","toolName":"bash","content":[{"type":"text","text":"ok"}],"isError":false,"timestamp":3000},
			{"role":"compactionSummary","summary":"compacted stuff","tokensBefore":50000,"timestamp":%d},
			{"role":"user","content":"and then","timestamp":8000}
		]},
		"stats": {
			"tokens": {"input": 10, "output": 5, "total": 18},
			"cost": 0.5,
			"contextUsage": {"tokens": 18}
		}
	}`, id, id, path, compactedAt.UnixMilli()))
}

// TestColdSnapshotFollowsActiveBranch checks the tree walk: the leaf is the
// last appended entry, so messages on an abandoned branch are excluded and
// the branch summary appears in their place.
func TestColdSnapshotFollowsActiveBranch(t *testing.T) {
	path := writeColdFixture(t,
		`{"type":"session","version":3,"id":"bbbb","timestamp":"2026-07-18T09:00:00.000Z","cwd":"/tmp/ws"}`,
		`{"type":"message","id":"e1","parentId":null,"timestamp":"2026-07-18T09:00:01.000Z","message":{"role":"user","content":"start","timestamp":1000}}`,
		`{"type":"message","id":"e2","parentId":"e1","timestamp":"2026-07-18T09:00:02.000Z","message":{"role":"assistant","content":[{"type":"text","text":"ok"}],"timestamp":2000}}`,
		`{"type":"message","id":"e3","parentId":"e2","timestamp":"2026-07-18T09:00:03.000Z","message":{"role":"user","content":"abandoned","timestamp":3000}}`,
		`{"type":"branch_summary","id":"e4","parentId":"e2","timestamp":"2026-07-18T09:00:04.000Z","fromId":"e3","summary":"explored X"}`,
		`{"type":"message","id":"e5","parentId":"e4","timestamp":"2026-07-18T09:00:05.000Z","message":{"role":"user","content":"new direction","timestamp":5000}}`,
	)

	got, err := readColdSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "abandoned") {
		t.Fatalf("abandoned branch leaked into snapshot: %s", got)
	}
	var snap struct {
		Messages struct {
			Messages []struct {
				Role    string `json:"role"`
				Summary string `json:"summary"`
			} `json:"messages"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(got, &snap); err != nil {
		t.Fatal(err)
	}
	roles := make([]string, 0, len(snap.Messages.Messages))
	for _, m := range snap.Messages.Messages {
		roles = append(roles, m.Role)
	}
	want := []string{"user", "assistant", "branchSummary", "user"}
	if !reflect.DeepEqual(roles, want) {
		t.Fatalf("roles = %v, want %v", roles, want)
	}
	if snap.Messages.Messages[2].Summary != "explored X" {
		t.Fatalf("branch summary not carried: %+v", snap.Messages.Messages[2])
	}
}

// TestColdSnapshotLinearV1 covers legacy v1 files: no id/parentId links, the
// file order is the path.
func TestColdSnapshotLinearV1(t *testing.T) {
	path := writeColdFixture(t,
		`{"type":"session","version":1,"id":"v1v1","timestamp":"2024-01-01T00:00:00.000Z","cwd":"/tmp/old"}`,
		`{"type":"message","message":{"role":"user","content":"one","timestamp":1000}}`,
		`{"type":"message","message":{"role":"assistant","content":[{"type":"text","text":"two"}],"timestamp":2000}}`,
	)

	got, err := readColdSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	var snap struct {
		Messages struct {
			Messages []json.RawMessage `json:"messages"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(got, &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Messages.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d: %s", len(snap.Messages.Messages), got)
	}
}

// TestColdSnapshotSkipsGarbage: torn or unknown lines are skipped, matching
// how every session-file consumer must behave while pi appends concurrently.
func TestColdSnapshotSkipsGarbage(t *testing.T) {
	path := writeColdFixture(t,
		`{"type":"session","version":3,"id":"gggg","timestamp":"2026-07-18T09:00:00.000Z","cwd":"/tmp/ws"}`,
		`{"type":"message","id":"e1","parentId":null,"timestamp":"2026-07-18T09:00:01.000Z","message":{"role":"user","content":"kept","timestamp":1000}}`,
		`this is not json`,
		`{"type":"some_future_entry","id":"e2","parentId":"e1","timestamp":"2026-07-18T09:00:02.000Z"}`,
	)

	got, err := readColdSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "kept") {
		t.Fatalf("message lost: %s", got)
	}
}

// TestColdSnapshotEmptySession: a header-only file renders an empty feed.
func TestColdSnapshotEmptySession(t *testing.T) {
	path := writeColdFixture(t,
		`{"type":"session","version":3,"id":"eeee","timestamp":"2026-07-18T09:00:00.000Z","cwd":"/tmp/ws"}`,
	)

	got, err := readColdSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, got, fmt.Sprintf(`{
		"id": "eeee",
		"cwd": "/tmp/ws",
		"state": {"sessionId": "eeee", "sessionFile": %q, "isStreaming": false, "isCompacting": false},
		"messages": {"messages": []},
		"stats": null
	}`, path))
}

// TestColdSnapshotRefusesUnknown: newer session versions and non-session
// files must fail so the server falls back to a live child.
func TestColdSnapshotRefusesUnknown(t *testing.T) {
	cases := map[string]string{
		"future version": `{"type":"session","version":99,"id":"ffff","timestamp":"2026-07-18T09:00:00.000Z","cwd":"/tmp"}`,
		"not a session":  `{"type":"something","id":"ffff"}`,
		"no id":          `{"type":"session","version":3}`,
		"not json":       `hello`,
	}
	for name, header := range cases {
		path := writeColdFixture(t, header)
		if _, err := readColdSnapshot(path); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	if _, err := readColdSnapshot(filepath.Join(t.TempDir(), "missing.jsonl")); err == nil {
		t.Error("missing file: expected error")
	}
}
