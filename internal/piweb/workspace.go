package piweb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
