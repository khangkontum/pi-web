package piweb

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SessionInfo describes one stored pi session, derived from its JSONL file.
// The same files back the pi CLI, so everything listed here is resumable from
// a terminal too.
type SessionInfo struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	Cwd       string    `json:"cwd"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updatedAt"`
	Live      bool      `json:"live"`
}

// sessionHeadScanLimit bounds how much of a session file is read while
// deriving its title from the first user message.
const sessionHeadScanLimit = 256 << 10

// listSessions scans dir recursively for pi session files, newest first.
func listSessions(dir string) ([]SessionInfo, error) {
	var out []SessionInfo
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		info, ok := readSessionInfo(path)
		if !ok {
			return nil
		}
		out = append(out, info)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

// sessionFileByID walks dir for the stored session whose header id equals id
// and returns its file path plus the cwd recorded in its header. pi's
// --session flag accepts a file path, and the path form resolves legacy
// per-project session layouts that pi's bare-id lookup can no longer find.
func sessionFileByID(dir, id string) (path, cwd string, ok bool) {
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		hid, hcwd, hok := readSessionHeader(p)
		if hok && hid == id {
			path, cwd, ok = p, hcwd, true
			return fs.SkipAll
		}
		return nil
	})
	return path, cwd, ok
}

// readSessionHeader reads only the first JSONL line of a session file and
// returns its id and cwd.
func readSessionHeader(path string) (id, cwd string, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), sessionHeadScanLimit)
	if !scanner.Scan() {
		return "", "", false
	}
	var header struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Cwd  string `json:"cwd"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
		return "", "", false
	}
	if header.Type != "session" || header.ID == "" {
		return "", "", false
	}
	return header.ID, header.Cwd, true
}

// readSessionInfo parses the session header line and derives a display title
// from the first user message within a bounded prefix of the file.
func readSessionInfo(path string) (SessionInfo, bool) {
	f, err := os.Open(path)
	if err != nil {
		return SessionInfo{}, false
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return SessionInfo{}, false
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), sessionHeadScanLimit)
	if !scanner.Scan() {
		return SessionInfo{}, false
	}
	var header struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Cwd  string `json:"cwd"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
		return SessionInfo{}, false
	}
	if header.Type != "session" || header.ID == "" {
		return SessionInfo{}, false
	}

	info := SessionInfo{
		ID:        header.ID,
		Path:      path,
		Cwd:       header.Cwd,
		UpdatedAt: st.ModTime(),
	}

	read := 0
	for scanner.Scan() {
		read += len(scanner.Bytes())
		if read > sessionHeadScanLimit {
			break
		}
		var entry struct {
			Type    string `json:"type"`
			Label   string `json:"label"`
			Message struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type == "label" && entry.Label != "" {
			info.Title = entry.Label
			break
		}
		if entry.Type == "message" && entry.Message.Role == "user" {
			if text := userMessageText(entry.Message.Content); text != "" {
				info.Title = summarizeTitle(text)
				break
			}
		}
	}
	if info.Title == "" {
		info.Title = "(empty session)"
	}
	return info, true
}

// userMessageText extracts plain text from a user message content field,
// which is either a string or an array of typed content blocks.
func userMessageText(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	for _, b := range blocks {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			return b.Text
		}
	}
	return ""
}

func summarizeTitle(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	const max = 80
	if len(text) > max {
		text = text[:max] + "…"
	}
	return text
}
