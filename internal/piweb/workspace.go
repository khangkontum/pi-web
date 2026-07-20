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
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// gitInfo is the read-only repository summary shown in the session rail.
type gitInfo struct {
	Repo       bool   `json:"repo"`
	Branch     string `json:"branch"`
	DirtyCount int    `json:"dirtyCount"`
	Graph      string `json:"graph"`
}

// readGitInfo shells out to git on demand; there are no watchers. A workspace
// without a repository is a normal, non-error state.
func readGitInfo(ctx context.Context, workspace string) (gitInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	status, err := gitOutput(ctx, workspace, "status", "--porcelain=v2", "--branch")
	if err != nil {
		return gitInfo{Repo: false}, nil
	}

	info := gitInfo{Repo: true}
	for line := range strings.SplitSeq(status, "\n") {
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			info.Branch = strings.TrimPrefix(line, "# branch.head ")
		case line == "" || strings.HasPrefix(line, "#"):
		default:
			info.DirtyCount++
		}
	}

	graph, err := gitOutput(ctx, workspace, "log", "--graph", "--oneline", "--decorate=short", "-n", "20")
	if err == nil {
		info.Graph = graph
	}
	return info, nil
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
