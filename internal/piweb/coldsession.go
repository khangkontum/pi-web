package piweb

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Cold session rendering: build the SSE snapshot for a session with no
// running pi child by reading its JSONL file directly, so browsing old
// sessions never spawns a pi process. The format is pi's documented
// session-format.md (v1 linear entries, v2/v3 entry tree). Anything this
// parser does not understand makes readColdSnapshot fail, and the caller
// falls back to resuming a child — degrade to spawning, never to an error.

// coldMaxSessionVersion is the newest session file version this parser
// understands. Newer files are rendered by a live child instead.
const coldMaxSessionVersion = 3

// sessionEntry is one JSONL line of a session file: the union of the fields
// pi-web reads across all entry types.
type sessionEntry struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	ParentID  string          `json:"parentId"`
	Timestamp string          `json:"timestamp"`
	Message   json.RawMessage `json:"message"`       // message
	Provider  string          `json:"provider"`      // model_change
	ModelID   string          `json:"modelId"`       // model_change
	Level     string          `json:"thinkingLevel"` // thinking_level_change
	Name      string          `json:"name"`          // session_info
	Summary   string          `json:"summary"`       // compaction, branch_summary
	FromID    string          `json:"fromId"`        // branch_summary
	Tokens    int64           `json:"tokensBefore"`  // compaction
	Custom    string          `json:"customType"`    // custom_message
	Content   json.RawMessage `json:"content"`       // custom_message
	Display   bool            `json:"display"`       // custom_message
}

// assistantMeta is the slice of an assistant message folded into cold state
// (current model) and stats (token/cost totals).
type assistantMeta struct {
	Role     string `json:"role"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Usage    struct {
		Input       int64 `json:"input"`
		Output      int64 `json:"output"`
		CacheRead   int64 `json:"cacheRead"`
		CacheWrite  int64 `json:"cacheWrite"`
		TotalTokens int64 `json:"totalTokens"`
		Cost        struct {
			Total float64 `json:"total"`
		} `json:"cost"`
	} `json:"usage"`
}

// readColdSnapshot renders a stored session into the same snapshot payload
// the live path assembles from get_state/get_messages/get_session_stats, so
// the browser cannot tell a cold session from an idle live one.
func readColdSnapshot(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), rpcScannerBuffer)
	if !scanner.Scan() {
		return nil, fmt.Errorf("empty session file")
	}
	var header struct {
		Type    string `json:"type"`
		Version int    `json:"version"`
		ID      string `json:"id"`
		Cwd     string `json:"cwd"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
		return nil, fmt.Errorf("parse session header: %w", err)
	}
	if header.Type != "session" || header.ID == "" {
		return nil, fmt.Errorf("not a session file")
	}
	// A missing version field is a v1 file.
	if header.Version > coldMaxSessionVersion {
		return nil, fmt.Errorf("session version %d is newer than this pi-web understands", header.Version)
	}

	// Tolerant per-line parse: pi appends lines while sessions run, so a
	// torn or unknown line is skipped, not fatal.
	var entries []sessionEntry
	for scanner.Scan() {
		var e sessionEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue
		}
		if e.Type == "" {
			continue
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return marshalColdSnapshot(header.ID, header.Cwd, path, activePath(entries))
}

// activePath resolves the entries the session currently stands on: the walk
// from the leaf (the last entry appended — branch switches append onto the
// new branch, so append order tracks the live position) back to the root.
// v1 files have no id/parentId links and are already linear.
func activePath(entries []sessionEntry) []sessionEntry {
	leaf := -1
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].ID != "" {
			leaf = i
			break
		}
	}
	if leaf == -1 {
		return entries
	}

	byID := make(map[string]int, len(entries))
	for i, e := range entries {
		if e.ID != "" {
			byID[e.ID] = i
		}
	}
	// Walk leaf → root, capped at len(entries) as cycle insurance, then
	// reverse into file order.
	var reversed []sessionEntry
	for i := leaf; len(reversed) <= len(entries); {
		reversed = append(reversed, entries[i])
		if entries[i].ParentID == "" {
			break
		}
		next, ok := byID[entries[i].ParentID]
		if !ok {
			break
		}
		i = next
	}
	path := make([]sessionEntry, 0, len(reversed))
	for i := len(reversed) - 1; i >= 0; i-- {
		path = append(path, reversed[i])
	}
	return path
}

// marshalColdSnapshot folds the active path into the snapshot wire shape:
// message entries pass through verbatim; compaction, branch-summary, and
// extension messages become their AgentMessage forms; model/thinking/name
// changes update the state the same way pi replays them on resume.
func marshalColdSnapshot(id, cwd, path string, entries []sessionEntry) ([]byte, error) {
	messages := []json.RawMessage{}
	var model map[string]any
	thinking := ""
	name := ""
	var stats map[string]any
	var tokIn, tokOut, tokTotal int64
	var cost float64

	for _, e := range entries {
		switch e.Type {
		case "message":
			messages = append(messages, e.Message)
			var meta assistantMeta
			if err := json.Unmarshal(e.Message, &meta); err != nil || meta.Role != "assistant" {
				continue
			}
			if meta.Model != "" {
				model = map[string]any{"provider": meta.Provider, "id": meta.Model}
			}
			u := meta.Usage
			tokIn += u.Input
			tokOut += u.Output
			tokTotal += u.TotalTokens
			cost += u.Cost.Total
			// The last response's full usage approximates current context
			// size; the window itself is unknown without a live child.
			if ctx := u.Input + u.Output + u.CacheRead + u.CacheWrite; ctx > 0 {
				stats = map[string]any{
					"tokens":       map[string]any{"input": tokIn, "output": tokOut, "total": tokTotal},
					"cost":         cost,
					"contextUsage": map[string]any{"tokens": ctx},
				}
			}
		case "compaction":
			messages = appendSynthesized(messages, map[string]any{
				"role":         "compactionSummary",
				"summary":      e.Summary,
				"tokensBefore": e.Tokens,
			}, e.Timestamp)
		case "branch_summary":
			messages = appendSynthesized(messages, map[string]any{
				"role":    "branchSummary",
				"summary": e.Summary,
				"fromId":  e.FromID,
			}, e.Timestamp)
		case "custom_message":
			messages = appendSynthesized(messages, map[string]any{
				"role":       "custom",
				"customType": e.Custom,
				"content":    e.Content,
				"display":    e.Display,
			}, e.Timestamp)
		case "model_change":
			model = map[string]any{"provider": e.Provider, "id": e.ModelID}
		case "thinking_level_change":
			thinking = e.Level
		case "session_info":
			name = e.Name
		}
	}

	state := map[string]any{
		"sessionId":    id,
		"sessionFile":  path,
		"isStreaming":  false,
		"isCompacting": false,
	}
	if model != nil {
		state["model"] = model
	}
	if thinking != "" {
		state["thinkingLevel"] = thinking
	}
	if name != "" {
		state["sessionName"] = name
	}
	if stats == nil {
		return json.Marshal(map[string]any{
			"id": id, "cwd": cwd, "state": state,
			"messages": map[string]any{"messages": messages},
			"stats":    nil,
		})
	}
	return json.Marshal(map[string]any{
		"id": id, "cwd": cwd, "state": state,
		"messages": map[string]any{"messages": messages},
		"stats":    stats,
	})
}

// appendSynthesized marshals a non-message entry's AgentMessage form, adding
// the entry's ISO timestamp as the Unix-ms timestamp messages carry.
func appendSynthesized(messages []json.RawMessage, msg map[string]any, isoTime string) []json.RawMessage {
	if ts, err := time.Parse(time.RFC3339Nano, isoTime); err == nil {
		msg["timestamp"] = ts.UnixMilli()
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		return messages
	}
	return append(messages, raw)
}
