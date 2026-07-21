package piweb

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/khangkontum/pi-web/internal/dtach"
)

func TestTerminalManagerLifecycle(t *testing.T) {
	tm := newTestTerminalManager(t)

	if got := tm.list(); len(got) != 0 {
		t.Fatalf("fresh manager lists %d terminals", len(got))
	}

	sess, err := tm.create("", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if sess.ID == "" || sess.Cwd == "" {
		t.Fatalf("incomplete session: %+v", sess)
	}
	if _, err := os.Stat(filepath.Join(tm.dir, sess.ID+".json")); err != nil {
		t.Fatalf("record not written: %v", err)
	}
	if got := tm.list(); len(got) != 1 || got[0].ID != sess.ID {
		t.Fatalf("list = %+v", got)
	}

	// Input goes through the shared writer; output is observable from a
	// separate read attachment (like an SSE stream would).
	dc, err := tm.attach(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer dc.Close()
	if mt, _, err := dc.Recv(); err != nil || mt != dtach.MsgSnapshot {
		t.Fatalf("first frame = %v, %v; want snapshot", mt, err)
	}
	if err := tm.input(sess.ID, []byte("echo terminal-works\n")); err != nil {
		t.Fatal(err)
	}
	var collected bytes.Buffer
	deadline := time.Now().Add(5 * time.Second)
	for !bytes.Contains(collected.Bytes(), []byte("terminal-works")) {
		if time.Now().After(deadline) {
			t.Fatalf("no echo, got %q", collected.String())
		}
		mt, p, err := dc.Recv()
		if err != nil {
			t.Fatalf("recv: %v (got %q)", err, collected.String())
		}
		if mt == dtach.MsgOutput || mt == dtach.MsgSnapshot {
			collected.Write(p)
		}
	}

	if err := tm.resize(sess.ID, 132, 43); err != nil {
		t.Fatal(err)
	}

	// kill with pid 0 (in-process) must not signal, but must drop the record.
	tm.kill(sess.ID)
	if got := tm.list(); len(got) != 0 {
		t.Fatalf("after kill, list = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(tm.dir, sess.ID+".json")); !os.IsNotExist(err) {
		t.Fatalf("record not removed: %v", err)
	}
}

func TestTerminalManagerScanDropsDeadRecords(t *testing.T) {
	dir, err := os.MkdirTemp("", "pwt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	// A record pointing at a socket that doesn't exist is stale.
	stale := terminalSession{ID: "tdeaddead1", Cwd: "/", Socket: filepath.Join(dir, "tdeaddead1.sock")}
	data, _ := json.MarshalIndent(stale, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "tdeaddead1.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	tm, err := newTerminalManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := tm.list(); len(got) != 0 {
		t.Fatalf("stale record survived scan: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "tdeaddead1.json")); !os.IsNotExist(err) {
		t.Fatal("stale record file not removed")
	}
}

func TestTerminalManagerRediscoversLiveTerminal(t *testing.T) {
	tm := newTestTerminalManager(t)
	sess, err := tm.create("", 80, 24)
	if err != nil {
		t.Fatal(err)
	}

	// A second manager over the same dir (a pi-web restart) must rediscover
	// the still-live terminal and route input to it.
	tm2, err := newTerminalManager(tm.dir)
	if err != nil {
		t.Fatal(err)
	}
	tm2.spawner = inProcessSpawner
	if got := tm2.list(); len(got) != 1 || got[0].ID != sess.ID {
		t.Fatalf("rediscovered list = %+v", got)
	}
	if err := tm2.input(sess.ID, []byte(":\n")); err != nil {
		t.Fatalf("input after rediscovery: %v", err)
	}
	tm.kill(sess.ID)
}
