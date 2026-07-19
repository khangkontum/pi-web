package piweb

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFileView(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	view, err := readFileView(ws, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if view.Content != "hello\n" || view.Truncated || view.Binary {
		t.Fatalf("unexpected view: %+v", view)
	}

	if _, err := readFileView(ws, ""); err == nil {
		t.Error("empty path should error")
	}
	if _, err := readFileView(ws, "."); err == nil {
		t.Error("directory should error")
	}

	big := bytes.Repeat([]byte("x"), fileViewLimit+100)
	if err := os.WriteFile(filepath.Join(ws, "big.txt"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	view, err = readFileView(ws, "big.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !view.Truncated || len(view.Content) != fileViewLimit {
		t.Fatalf("expected truncated view, got truncated=%v len=%d", view.Truncated, len(view.Content))
	}

	if err := os.WriteFile(filepath.Join(ws, "bin"), []byte{0x00, 0x01, 0xff}, 0o644); err != nil {
		t.Fatal(err)
	}
	view, err = readFileView(ws, "bin")
	if err != nil {
		t.Fatal(err)
	}
	if !view.Binary {
		t.Fatalf("expected binary detection, got %+v", view)
	}
}

func TestReadGitInfo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	ws := t.TempDir()

	info, err := readGitInfo(t.Context(), ws)
	if err != nil {
		t.Fatal(err)
	}
	if info.Repo {
		t.Fatal("bare directory should not report a repo")
	}

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", ws}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(ws, "f.txt"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "first commit")
	if err := os.WriteFile(filepath.Join(ws, "dirty.txt"), []byte("2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	info, err = readGitInfo(t.Context(), ws)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Repo || info.Branch != "main" {
		t.Fatalf("unexpected info: %+v", info)
	}
	if info.DirtyCount != 1 {
		t.Errorf("dirty count = %d, want 1", info.DirtyCount)
	}
	if !strings.Contains(info.Graph, "first commit") {
		t.Errorf("graph missing commit: %q", info.Graph)
	}
}
