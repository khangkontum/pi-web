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
	if got := info.Changes["dirty.txt"]; got != "?" {
		t.Errorf("changes[dirty.txt] = %q, want ?", got)
	}

	if err := os.WriteFile(filepath.Join(ws, "f.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(ws, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "sub", "new.txt"), []byte("3\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	info, err = readGitInfo(t.Context(), ws)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Changes["f.txt"]; got != "M" {
		t.Errorf("changes[f.txt] = %q, want M", got)
	}
	if got := info.Changes["sub/new.txt"]; got != "?" {
		t.Errorf("changes[sub/new.txt] = %q, want ?", got)
	}

	// a workspace rooted in a subdirectory sees subdir-relative paths and
	// none of the repo's other changes
	info, err = readGitInfo(t.Context(), filepath.Join(ws, "sub"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Changes["new.txt"]; got != "?" {
		t.Errorf("subdir changes[new.txt] = %q, want ?", got)
	}
	if _, found := info.Changes["f.txt"]; found {
		t.Errorf("subdir changes should not include ../f.txt: %v", info.Changes)
	}
}

// gitTestRepo initialises a repo in ws with a deterministic identity and
// returns a runner for further git commands.
func gitTestRepo(t *testing.T, ws string) func(args ...string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
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
	return run
}

func TestReadGitLog(t *testing.T) {
	ws := t.TempDir()

	commits, err := readGitLog(t.Context(), ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 0 {
		t.Fatalf("non-repo should list no commits, got %d", len(commits))
	}

	run := gitTestRepo(t, ws)
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "first")
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("commit", "-am", "second")

	commits, err = readGitLog(t.Context(), ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("commits = %d, want 2", len(commits))
	}
	head, root := commits[0], commits[1]
	if head.Subject != "second" || root.Subject != "first" {
		t.Fatalf("subjects = %q, %q", head.Subject, root.Subject)
	}
	if len(head.Parents) != 1 || head.Parents[0] != root.Hash {
		t.Fatalf("head parents = %v, want [%s]", head.Parents, root.Hash)
	}
	if len(root.Parents) != 0 {
		t.Fatalf("root parents = %v, want none", root.Parents)
	}
	if head.Author != "t" || head.Date == "" || len(head.Hash) != 40 {
		t.Fatalf("unexpected head commit: %+v", head)
	}
	if !strings.Contains(head.Refs, "main") {
		t.Errorf("head refs = %q, want branch decoration", head.Refs)
	}
}

func TestReadGitDiff(t *testing.T) {
	ws := t.TempDir()
	run := gitTestRepo(t, ws)
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "first")

	// Clean tree: empty patch.
	diff, err := readGitDiff(t.Context(), ws, "")
	if err != nil {
		t.Fatal(err)
	}
	if diff.Patch != "" || diff.Truncated {
		t.Fatalf("clean tree diff = %+v", diff)
	}

	// A tracked edit and an untracked file both appear in the working-tree patch.
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "new.txt"), []byte("fresh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diff, err = readGitDiff(t.Context(), ws, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff.Patch, "+2") || !strings.Contains(diff.Patch, "a.txt") {
		t.Errorf("patch missing tracked edit:\n%s", diff.Patch)
	}
	if !strings.Contains(diff.Patch, "+fresh") || !strings.Contains(diff.Patch, "new.txt") {
		t.Errorf("patch missing untracked file:\n%s", diff.Patch)
	}

	// A commit ref shows that commit's patch.
	commits, err := readGitLog(t.Context(), ws)
	if err != nil {
		t.Fatal(err)
	}
	diff, err = readGitDiff(t.Context(), ws, commits[0].Hash)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff.Patch, "+1") {
		t.Errorf("commit patch missing content:\n%s", diff.Patch)
	}

	// Option-shaped and malformed refs are rejected before reaching git.
	if _, err := readGitDiff(t.Context(), ws, "--output=/tmp/x"); err == nil {
		t.Error("option-shaped ref should be rejected")
	}
	if _, err := readGitDiff(t.Context(), ws, "main"); err == nil {
		t.Error("branch-name ref should be rejected (hex/HEAD only)")
	}
}

func TestCapPatch(t *testing.T) {
	small := capPatch("line\n", false)
	if small.Patch != "line\n" || small.Truncated {
		t.Fatalf("small patch altered: %+v", small)
	}
	big := strings.Repeat("x", 100) + "\n"
	huge := strings.Repeat(big, gitDiffLimit/len(big)+2)
	capped := capPatch(huge, false)
	if !capped.Truncated || len(capped.Patch) > gitDiffLimit {
		t.Fatalf("cap failed: truncated=%v len=%d", capped.Truncated, len(capped.Patch))
	}
	if !strings.HasSuffix(capped.Patch, strings.Repeat("x", 100)) {
		t.Error("cap should cut at a line boundary")
	}
}
