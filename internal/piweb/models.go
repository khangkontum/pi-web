package piweb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// modelInfo is one row of `pi --list-models`, the source of truth for the
// model picker. pi has no list-models RPC command, so the CLI is spawned.
type modelInfo struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Context  string `json:"context"`
	Thinking bool   `json:"thinking"`
	Images   bool   `json:"images"`
}

// listModels runs `pi --list-models` and parses its table. dir and the
// process environment mirror how the supervisor spawns pi, so provider
// configuration resolves identically.
func listModels(ctx context.Context, piCommand []string, dir string) ([]modelInfo, error) {
	if len(piCommand) == 0 {
		return nil, errors.New("empty pi command")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	args := append(append([]string{}, piCommand[1:]...), "--list-models")
	cmd := exec.CommandContext(ctx, piCommand[0], args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &bytes.Buffer{}
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("pi --list-models: %w", err)
	}
	return parseModels(out.String()), nil
}

// parseModels reads the whitespace-columned table: provider, model, context,
// max-out, thinking, images. The header row and malformed lines are skipped.
func parseModels(table string) []modelInfo {
	var out []modelInfo
	for line := range strings.SplitSeq(strings.TrimRight(table, "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		if f[0] == "provider" && f[1] == "model" {
			continue
		}
		m := modelInfo{Provider: f[0], Model: f[1]}
		if len(f) >= 3 {
			m.Context = f[2]
		}
		if len(f) >= 5 {
			m.Thinking = f[4] == "yes"
		}
		if len(f) >= 6 {
			m.Images = f[5] == "yes"
		}
		out = append(out, m)
	}
	return out
}
