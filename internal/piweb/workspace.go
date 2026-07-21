package piweb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// gitInfo is the read-only repository summary shown in the session rail.
// Changes maps workspace-relative paths to a one-letter status (M, A, D, R,
// U, ?) so the file explorer can tint changed entries.
type gitInfo struct {
	Repo       bool              `json:"repo"`
	Branch     string            `json:"branch"`
	DirtyCount int               `json:"dirtyCount"`
	Graph      string            `json:"graph"`
	Changes    map[string]string `json:"changes,omitempty"`
}

// readGitInfo shells out to git on demand; there are no watchers. A workspace
// without a repository is a normal, non-error state.
func readGitInfo(ctx context.Context, workspace string) (gitInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// --untracked-files=all lists untracked files individually rather than
	// collapsing new directories, so every explorer row can be tinted.
	// Forcing status.relativePaths makes porcelain v2 paths relative to the
	// workspace (not the repo root) regardless of user config; changes
	// outside a subdirectory workspace then show up as ../ and are dropped.
	status, err := gitOutput(ctx, workspace, "-c", "status.relativePaths=true",
		"status", "--porcelain=v2", "--branch", "--untracked-files=all")
	if err != nil {
		return gitInfo{Repo: false}, nil
	}

	info := gitInfo{Repo: true, Changes: map[string]string{}}
	for line := range strings.SplitSeq(status, "\n") {
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			info.Branch = strings.TrimPrefix(line, "# branch.head ")
		case line == "" || strings.HasPrefix(line, "#"):
		default:
			info.DirtyCount++
			path, st, ok := parseStatusLine(line)
			if ok && path != ".." && !strings.HasPrefix(path, "../") {
				info.Changes[path] = st
			}
		}
	}

	graph, err := gitOutput(ctx, workspace, "log", "--graph", "--oneline", "--decorate=short", "-n", "20")
	if err == nil {
		info.Graph = graph
	}
	return info, nil
}

// parseStatusLine extracts the path and a one-letter status from one
// porcelain v2 entry. Paths in v2 records are relative to the repo root.
func parseStatusLine(line string) (path, status string, ok bool) {
	switch {
	case strings.HasPrefix(line, "1 "):
		parts := strings.SplitN(line, " ", 9)
		if len(parts) < 9 {
			return "", "", false
		}
		return unquoteGitPath(parts[8]), xyStatus(parts[1]), true
	case strings.HasPrefix(line, "2 "):
		parts := strings.SplitN(line, " ", 10)
		if len(parts) < 10 {
			return "", "", false
		}
		// rename records carry "<newPath>\t<origPath>"
		newPath, _, _ := strings.Cut(parts[9], "\t")
		return unquoteGitPath(newPath), "R", true
	case strings.HasPrefix(line, "u "):
		parts := strings.SplitN(line, " ", 11)
		if len(parts) < 11 {
			return "", "", false
		}
		return unquoteGitPath(parts[10]), "U", true
	case strings.HasPrefix(line, "? "):
		return unquoteGitPath(line[2:]), "?", true
	}
	return "", "", false
}

// xyStatus reduces a porcelain XY pair to one letter, preferring the
// worktree side over the index side.
func xyStatus(xy string) string {
	if len(xy) != 2 {
		return "M"
	}
	c := xy[1]
	if c == '.' {
		c = xy[0]
	}
	switch c {
	case 'A', 'D', 'R':
		return string(c)
	default:
		return "M"
	}
}

// unquoteGitPath undoes git's C-style quoting of paths with special
// characters; plain paths pass through unchanged.
func unquoteGitPath(p string) string {
	if len(p) >= 2 && strings.HasPrefix(p, `"`) && strings.HasSuffix(p, `"`) {
		if u, err := strconv.Unquote(p); err == nil {
			return u
		}
	}
	return p
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &bytes.Buffer{}
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

// gitPatchOutput runs a git diff-style command where exit status 1 means
// "differences found", not failure.
func gitPatchOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &bytes.Buffer{}
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		err = nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

// gitCommit is one commit in the structured history feed behind the git
// overlay; Parents is what lets the client draw graph lanes.
type gitCommit struct {
	Hash    string   `json:"hash"`
	Parents []string `json:"parents"`
	Refs    string   `json:"refs"`
	Author  string   `json:"author"`
	Date    string   `json:"date"`
	Subject string   `json:"subject"`
}

// gitLogLimit caps the history feed; the overlay is an audit view, not a full
// repository browser.
const gitLogLimit = 200

// readGitLog returns newest-first commit history for the graph view. A
// directory that is not a repository returns an empty list, mirroring
// readGitInfo's non-error treatment.
func readGitLog(ctx context.Context, base string) ([]gitCommit, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// \x1f separates fields; %s is the subject line so records stay one-per-line.
	out, err := gitOutput(ctx, base, "log", "--all",
		"--pretty=format:%H%x1f%P%x1f%D%x1f%an%x1f%aI%x1f%s", "-n", strconv.Itoa(gitLogLimit))
	if err != nil {
		return []gitCommit{}, nil
	}
	commits := []gitCommit{}
	for line := range strings.SplitSeq(out, "\n") {
		parts := strings.Split(line, "\x1f")
		if len(parts) != 6 {
			continue
		}
		commits = append(commits, gitCommit{
			Hash:    parts[0],
			Parents: strings.Fields(parts[1]),
			Refs:    parts[2],
			Author:  parts[3],
			Date:    parts[4],
			Subject: parts[5],
		})
	}
	return commits, nil
}

// gitDiff is a unified patch plus whether the server truncated it.
type gitDiff struct {
	Patch     string `json:"patch"`
	Truncated bool   `json:"truncated"`
}

// gitDiffLimit caps a served patch; a vendored-dependency commit can be tens
// of megabytes and the viewer marks truncation instead.
const gitDiffLimit = 1 << 20

// gitUntrackedLimit bounds how many untracked files are rendered into the
// working-tree patch.
const gitUntrackedLimit = 50

// gitRefPattern is the only ref shape the diff endpoint accepts: HEAD or a
// hex hash. Nothing that could be parsed as a git option gets through.
var gitRefPattern = regexp.MustCompile(`^(HEAD|[0-9a-fA-F]{4,40})$`)

// readGitDiff returns the patch for one commit (ref), or with an empty ref the
// full working-tree change: staged and unstaged edits vs HEAD, plus untracked
// files rendered via --no-index so new files the agent wrote are auditable.
func readGitDiff(ctx context.Context, base, ref string) (gitDiff, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	if ref != "" {
		if !gitRefPattern.MatchString(ref) {
			return gitDiff{}, fmt.Errorf("invalid ref %q", ref)
		}
		out, err := gitOutput(ctx, base, "show", "--format=", "--patch", ref)
		if err != nil {
			return gitDiff{}, fmt.Errorf("git show %s: %w", ref, err)
		}
		return capPatch(out, false), nil
	}

	// Working tree vs HEAD; a repository with no commits yet has no HEAD, so
	// fall back to the index diff. Any git failure (not a repo) is the normal
	// empty state.
	parts := []string{}
	patch, err := gitPatchOutput(ctx, base, "diff", "HEAD")
	if err != nil {
		patch, _ = gitPatchOutput(ctx, base, "diff")
	}
	if patch != "" {
		parts = append(parts, patch)
	}

	skipped := false
	if untracked, err := gitOutput(ctx, base, "ls-files", "--others", "--exclude-standard"); err == nil && untracked != "" {
		files := strings.Split(untracked, "\n")
		if len(files) > gitUntrackedLimit {
			files = files[:gitUntrackedLimit]
			skipped = true
		}
		for _, f := range files {
			p, err := gitPatchOutput(ctx, base, "diff", "--no-index", "--", os.DevNull, f)
			if err == nil && p != "" {
				parts = append(parts, p)
			}
		}
	}
	return capPatch(strings.Join(parts, "\n"), skipped), nil
}

// readGitFileDiff returns the working-tree patch scoped to a single file:
// staged and unstaged edits vs HEAD for a tracked file, or the whole file via
// --no-index when it is untracked. Tracked-ness is decided with `git ls-files`,
// which only reads the index — unlike `git diff`, which refreshes the whole
// index (an lstat over every tracked file) before honouring the pathspec, so an
// untracked file must never reach it on a large repo. The "--" guard means the
// caller-supplied path can never be read as a git option. Not a repository, or a
// clean/ignored file, is the normal empty state.
func readGitFileDiff(ctx context.Context, base, path string) (gitDiff, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	if tracked, err := gitOutput(ctx, base, "ls-files", "--", path); err == nil && tracked != "" {
		patch, err := gitPatchOutput(ctx, base, "diff", "HEAD", "--", path)
		if err != nil {
			patch, _ = gitPatchOutput(ctx, base, "diff", "--", path)
		}
		return capPatch(patch, false), nil
	}

	// Untracked (and not ignored): render the whole file as an addition without
	// ever touching the index.
	if others, err := gitOutput(ctx, base, "ls-files", "--others", "--exclude-standard", "--", path); err == nil && others != "" {
		patch, _ := gitPatchOutput(ctx, base, "diff", "--no-index", "--", os.DevNull, path)
		return capPatch(patch, false), nil
	}
	return gitDiff{}, nil
}

// capPatch enforces gitDiffLimit, cutting at a line boundary.
func capPatch(patch string, truncated bool) gitDiff {
	if len(patch) <= gitDiffLimit {
		return gitDiff{Patch: patch, Truncated: truncated}
	}
	cut := patch[:gitDiffLimit]
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		cut = cut[:i]
	}
	return gitDiff{Patch: cut, Truncated: true}
}

// fileIndexLimit caps how many paths the file index returns; a client-side
// fuzzy finder does not need more, and it bounds the response for very large
// repositories.
const fileIndexLimit = 20000

// fileIndexMaxDepth bounds the WalkDir fallback used when the base is not a git
// repository, so an accidental scan of a huge tree stays cheap.
const fileIndexMaxDepth = 12

// listFiles returns a flat, workspace-relative file index for a client-side
// fuzzy finder. It prefers `git ls-files` (respecting .gitignore); when base is
// not a git repository it falls back to a bounded WalkDir that skips dot-dirs
// and common heavy directories. The returned bool reports whether the cap was
// hit.
func listFiles(ctx context.Context, base string) ([]string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	if out, err := gitOutput(ctx, base, "ls-files", "--cached", "--others", "--exclude-standard"); err == nil {
		files := []string{}
		truncated := false
		for line := range strings.SplitSeq(out, "\n") {
			if line == "" {
				continue
			}
			if len(files) >= fileIndexLimit {
				truncated = true
				break
			}
			files = append(files, line)
		}
		sort.Strings(files)
		return files, truncated, nil
	}
	return walkFiles(base)
}

// skipDirNames are directories the WalkDir fallback never descends into: they
// are large and rarely useful to a file finder.
var skipDirNames = map[string]bool{
	"node_modules": true,
	".git":         true,
	"dist":         true,
	"build":        true,
	"target":       true,
	"vendor":       true,
	".venv":        true,
	"__pycache__":  true,
}

func walkFiles(base string) ([]string, bool, error) {
	files := []string{}
	truncated := false
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, relErr := filepath.Rel(base, path)
		if relErr != nil {
			return nil
		}
		if d.IsDir() {
			if rel == "." {
				return nil
			}
			name := d.Name()
			if strings.HasPrefix(name, ".") || skipDirNames[name] {
				return fs.SkipDir
			}
			if strings.Count(rel, string(filepath.Separator))+1 > fileIndexMaxDepth {
				return fs.SkipDir
			}
			return nil
		}
		if len(files) >= fileIndexLimit {
			truncated = true
			return fs.SkipAll
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	sort.Strings(files)
	return files, truncated, nil
}

// treeEntry is one node in the explorer's single-level directory listing:
// immediate children of a directory, dirs first then files.
type treeEntry struct {
	Name string `json:"name"`
	Dir  bool   `json:"dir"`
}

// readTree lists the immediate children (directories and files) of dir for the
// file explorer. Under the loopback trust model any readable path is allowed.
func readTree(dir string) ([]treeEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]treeEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, treeEntry{Name: e.Name(), Dir: e.IsDir()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// rawFile is a file's bytes plus the content type to serve it under, resolved
// with the same base+path rules as readFileView.
type rawFile struct {
	Path        string
	Data        []byte
	ContentType string
}

// readRawFile reads a file (image/PDF/audio and anything else) with its
// content type detected from extension, falling back to sniffing the first
// bytes. It reuses readFileView's base+path resolution: relative paths resolve
// against base, absolute paths are used as-is.
func readRawFile(base, path string) (rawFile, error) {
	if strings.TrimSpace(path) == "" {
		return rawFile{}, fmt.Errorf("missing path")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	path = filepath.Clean(path)

	st, err := os.Stat(path)
	if err != nil {
		return rawFile{}, err
	}
	if st.IsDir() {
		return rawFile{}, fmt.Errorf("%s is a directory", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return rawFile{}, err
	}
	ct := contentTypeByExt[strings.ToLower(filepath.Ext(path))]
	if ct == "" {
		ct = http.DetectContentType(data)
	}
	return rawFile{Path: path, Data: data, ContentType: ct}, nil
}

// contentTypeByExt maps file extensions to content types http.DetectContentType
// does not (or unreliably) recognise, so raw responses set a precise type.
var contentTypeByExt = map[string]string{
	".svg":  "image/svg+xml",
	".webp": "image/webp",
	".pdf":  "application/pdf",
	".mp3":  "audio/mpeg",
	".wav":  "audio/wav",
	".ogg":  "audio/ogg",
	".m4a":  "audio/mp4",
	".flac": "audio/flac",
}

// fileView is the read-only file viewer payload.
type fileView struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
	Binary    bool   `json:"binary"`
	Size      int64  `json:"size"`
}

const fileViewLimit = 512 << 10

// readFileView reads a file for display. Relative paths resolve against the
// workspace; pi-web runs as the VM user, so filesystem permissions are the
// access boundary, same as the agent's own tools.
func readFileView(workspace, path string) (fileView, error) {
	if strings.TrimSpace(path) == "" {
		return fileView{}, fmt.Errorf("missing path")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspace, path)
	}
	path = filepath.Clean(path)

	st, err := os.Stat(path)
	if err != nil {
		return fileView{}, err
	}
	if st.IsDir() {
		return fileView{}, fmt.Errorf("%s is a directory", path)
	}

	f, err := os.Open(path)
	if err != nil {
		return fileView{}, err
	}
	defer f.Close()

	buf := make([]byte, fileViewLimit+1)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return fileView{}, err
	}
	view := fileView{Path: path, Size: st.Size()}
	if n > fileViewLimit {
		n = fileViewLimit
		view.Truncated = true
	}
	data := buf[:n]
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		view.Binary = true
		return view, nil
	}
	view.Content = string(data)
	return view, nil
}
