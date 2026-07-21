// Derived from shelley (github.com/boldsoftware/shelley), Copyright 2026 Bold
// Software, Inc., Apache License 2.0. See NOTICE at the repository root.

package dtach

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRingDropsOldest(t *testing.T) {
	r := newRing(8)
	r.write([]byte("abcd"))
	r.write([]byte("efghij"))
	got := r.snapshot()
	want := []byte("cdefghij")
	if !bytes.Equal(got, want) {
		t.Fatalf("ring: got %q want %q", got, want)
	}
}

// testSock returns a socket path short enough for darwin's 104-byte
// sun_path limit; t.TempDir() paths overflow it under long test names.
func testSock(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "dtach")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "s")
}

func serveInBackground(t *testing.T, opts ServerOptions) <-chan error {
	t.Helper()
	ready := make(chan struct{})
	opts.Ready = ready
	done := make(chan error, 1)
	go func() { done <- Serve(opts) }()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("serve exited before ready: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not become ready")
	}
	return done
}

func TestServeAttachSurvivesDetach(t *testing.T) {
	sock := testSock(t)

	// Use `cat` so the session stays alive until we explicitly signal EOF.
	// A fast-exiting command would race the test: the session can tear down
	// (closing and unlinking the socket) before the test gets a chance to
	// attach.
	serverDone := serveInBackground(t, ServerOptions{
		SocketPath: sock,
		Command:    "cat",
	})

	c, err := Attach(sock)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}

	if err := c.SendInput([]byte("hello\nworld\n")); err != nil {
		t.Fatalf("send: %v", err)
	}
	// Tell cat to exit (EOF) and let the session shut down naturally.
	if err := c.SendInput([]byte{0x04}); err != nil {
		t.Fatalf("send eof: %v", err)
	}

	var collected bytes.Buffer
	var exitCode int32 = -2
	for {
		mt, p, err := c.Recv()
		if err != nil {
			break
		}
		switch mt {
		case MsgSnapshot, MsgOutput:
			collected.Write(p)
		case MsgExit:
			exitCode, _ = DecodeExit(p)
		}
	}
	c.Close()

	if exitCode != 0 {
		t.Fatalf("exit code = %d want 0", exitCode)
	}
	if !bytes.Contains(collected.Bytes(), []byte("hello")) || !bytes.Contains(collected.Bytes(), []byte("world")) {
		t.Fatalf("missing output, got %q", collected.String())
	}

	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not exit")
	}
}

func TestAttachReplaysScrollbackToLateClient(t *testing.T) {
	sock := testSock(t)

	// Use `cat` so the session stays alive until we send EOF. While running,
	// any output we feed it should appear in the scrollback for late
	// attachers.
	done := serveInBackground(t, ServerOptions{
		SocketPath: sock,
		Command:    "cat",
	})

	c1, err := Attach(sock)
	if err != nil {
		t.Fatalf("attach c1: %v", err)
	}
	if _, _, err := c1.Recv(); err != nil { // initial empty snapshot
		t.Fatalf("recv snap c1: %v", err)
	}
	if err := c1.SendInput([]byte("hello\n")); err != nil {
		t.Fatalf("send: %v", err)
	}
	// Wait until cat has echoed it back — the data is now in the scrollback.
	for {
		mt, p, err := c1.Recv()
		if err != nil {
			t.Fatalf("recv c1: %v", err)
		}
		if mt == MsgOutput && bytes.Contains(p, []byte("hello")) {
			break
		}
	}
	c1.Close()

	c2, err := Attach(sock)
	if err != nil {
		t.Fatalf("attach c2: %v", err)
	}
	mt, snap, err := c2.Recv()
	if err != nil || mt != MsgSnapshot {
		t.Fatalf("recv c2 snapshot: mt=%v err=%v", mt, err)
	}
	if !bytes.Contains(snap, []byte("hello")) {
		t.Fatalf("snapshot missing expected output, got %q", snap)
	}
	// Kill the session by closing cat's stdin. Drain until we see exit.
	if err := c2.SendInput([]byte{0x04}); err != nil {
		t.Fatalf("send eof: %v", err)
	}
	for {
		mt, _, err := c2.Recv()
		if err != nil || mt == MsgExit {
			break
		}
	}
	c2.Close()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not exit")
	}
}

func TestResizePropagatesToCommand(t *testing.T) {
	sock := testSock(t)

	// The shell reports its size on demand; resize first, then ask.
	done := serveInBackground(t, ServerOptions{
		SocketPath: sock,
		Command:    "sh",
		Args:       []string{"-c", "read line; stty size"},
		Cols:       80,
		Rows:       24,
	})

	c, err := Attach(sock)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	defer c.Close()
	if err := c.SendResize(132, 43); err != nil {
		t.Fatalf("resize: %v", err)
	}
	if err := c.SendInput([]byte("go\n")); err != nil {
		t.Fatalf("send: %v", err)
	}

	var collected bytes.Buffer
	deadline := time.After(5 * time.Second)
	for !bytes.Contains(collected.Bytes(), []byte("43 132")) {
		select {
		case <-deadline:
			t.Fatalf("no resized stty output, got %q", collected.String())
		default:
		}
		mt, p, err := c.Recv()
		if err != nil {
			break
		}
		if mt == MsgSnapshot || mt == MsgOutput {
			collected.Write(p)
		}
		if mt == MsgExit {
			break
		}
	}
	if !bytes.Contains(collected.Bytes(), []byte("43 132")) {
		t.Fatalf("stty size = %q, want it to report 43 132", collected.String())
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not exit")
	}
}
