package piweb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListSessionsParsing(t *testing.T) {
	dir := t.TempDir()
	writeSessionFixture(t, dir, "11111111-0000-0000-0000-000000000000", "set up systemd for the api server")
	writeSessionFixture(t, dir, "22222222-0000-0000-0000-000000000000", "debug postgres oom")

	// Non-session files are ignored.
	if err := os.WriteFile(filepath.Join(dir, "junk.jsonl"), []byte("not json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	sessions, err := listSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d: %+v", len(sessions), sessions)
	}
	byID := map[string]SessionInfo{}
	for _, s := range sessions {
		byID[s.ID] = s
	}
	got := byID["11111111-0000-0000-0000-000000000000"]
	if got.Title != "set up systemd for the api server" {
		t.Errorf("title = %q", got.Title)
	}
	if got.Cwd != "/tmp/workspace" {
		t.Errorf("cwd = %q", got.Cwd)
	}
}

func TestListSessionsMissingDir(t *testing.T) {
	sessions, err := listSessions(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("missing dir should not error: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected no sessions, got %d", len(sessions))
	}
}

func TestSummarizeTitle(t *testing.T) {
	long := strings.Repeat("workspace ", 30)
	title := summarizeTitle(long)
	if len(title) > 90 {
		t.Errorf("title not truncated: %d chars", len(title))
	}
	if summarizeTitle("  hello\n  world ") != "hello world" {
		t.Errorf("whitespace not collapsed: %q", summarizeTitle("  hello\n  world "))
	}
}
