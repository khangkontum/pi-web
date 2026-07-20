package piweb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// piRegistryURL is the npm registry endpoint for the pi coding agent's latest
// published version. A side-effect-free GET compares it to the installed pi.
const piRegistryURL = "https://registry.npmjs.org/@earendil-works/pi-coding-agent/latest"

// piCheckInterval is how often pi-web checks the registry for a newer pi.
const piCheckInterval = 6 * time.Hour

// piCheckInitialDelay spaces the first pi check away from process start.
const piCheckInitialDelay = time.Minute

// piManager tracks the installed pi coding agent: which CLI flags it supports,
// whether a newer version is published, and (when asked) upgrades it via pi's
// own installer. It mirrors updater: every collaborator (flag probe, registry
// fetch, upgrade subprocess) is injectable so tests never spawn pi or touch
// the network.
//
// pi-web eliminates version skew rather than tolerating it: it keeps pi current
// and, when pi is too old for an optional flag, degrades by omitting the flag
// instead of refusing to start a session.
type piManager struct {
	piCommand    []string
	workspace    string
	interval     time.Duration
	settingsPath string
	client       *http.Client
	logw         io.Writer

	// probe returns pi's version and its supported flag set (long flags like
	// "--approve", without the leading dashes). Injectable for tests.
	probe func(ctx context.Context) (version string, flags map[string]bool, err error)
	// registry returns the latest published pi version. Injectable for tests.
	registry func(ctx context.Context) (string, error)
	// upgrade runs pi's own installer (`pi update pi`). Injectable for tests.
	upgrade func(ctx context.Context) error
	// recycle closes idle children so they respawn on the upgraded pi.
	recycle func()

	auto atomic.Bool

	mu    sync.Mutex
	cur   string
	flags map[string]bool
	last  piStatus
}

// piStatus is the cached pi version state surfaced to the UI banner.
type piStatus struct {
	Latest    string    `json:"latest"`
	Available bool      `json:"available"`
	CheckedAt time.Time `json:"checkedAt"`
	Error     string    `json:"error"`
}

func newPiManager(cfg Config, logw io.Writer) *piManager {
	m := &piManager{
		piCommand:    cfg.PiCommand,
		workspace:    cfg.Workspace,
		interval:     piCheckInterval,
		settingsPath: cfg.SettingsPath,
		client:       &http.Client{Timeout: 30 * time.Second},
		logw:         logw,
	}
	m.probe = m.probePi
	m.registry = m.fetchLatest
	m.upgrade = m.runUpgrade
	if s, ok := loadSettings(cfg.SettingsPath); ok {
		m.auto.Store(s.AutoUpdatePi)
	}
	return m
}

// bootProbe runs the flag probe once at startup and caches the result. A failed
// probe leaves the flag set nil; supportsFlag then reports false, so the
// supervisor spawns pi without optional flags rather than refusing to start.
func (m *piManager) bootProbe(ctx context.Context) {
	ver, flags, err := m.probe(ctx)
	if err != nil {
		fmt.Fprintf(m.logw, "pi-web: pi flag probe: %v\n", err)
		return
	}
	m.mu.Lock()
	m.cur = ver
	m.flags = flags
	m.mu.Unlock()
}

// supportsFlag reports whether the installed pi accepts the given long flag
// (e.g. "--approve"). An unprobed or failed pi reports false: the supervisor
// then omits the flag, degrading instead of dying.
func (m *piManager) supportsFlag(flag string) bool {
	flag = strings.TrimLeft(flag, "-")
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.flags[flag]
}

func (m *piManager) run(ctx context.Context) {
	timer := time.NewTimer(piCheckInitialDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		st, err := m.check(ctx)
		if err != nil {
			fmt.Fprintf(m.logw, "pi-web: pi update check: %v\n", err)
		} else if st.Available && m.auto.Load() {
			if err := m.applyUpgrade(ctx); err != nil {
				fmt.Fprintf(m.logw, "pi-web: pi auto-update: %v\n", err)
			}
		}
		timer.Reset(m.interval)
	}
}

// check queries the registry, compares to the installed pi, and caches the
// result. It has no side effects on the installed pi.
func (m *piManager) check(ctx context.Context) (piStatus, error) {
	st := piStatus{CheckedAt: time.Now()}
	latest, err := m.registry(ctx)
	if err != nil {
		st.Error = err.Error()
		m.setStatus(st)
		return st, err
	}
	st.Latest = latest
	st.Available = newerVersion(m.current(), latest)
	m.setStatus(st)
	return st, nil
}

// applyUpgrade upgrades pi via its own installer, then re-probes the version
// and flag set and recycles idle children onto the new binary. Any failure
// keeps the current pi running: version skew degrades, it does not crash.
func (m *piManager) applyUpgrade(ctx context.Context) error {
	before := m.current()
	if err := m.upgrade(ctx); err != nil {
		return err
	}
	ver, flags, err := m.probe(ctx)
	if err != nil {
		return fmt.Errorf("re-probe pi after upgrade: %w", err)
	}
	m.mu.Lock()
	m.cur = ver
	m.flags = flags
	m.last.Available = newerVersion(ver, m.last.Latest)
	m.mu.Unlock()
	fmt.Fprintf(m.logw, "pi-web: upgraded pi %s -> %s\n", before, ver)
	if m.recycle != nil {
		m.recycle()
	}
	return nil
}

func (m *piManager) current() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cur
}

func (m *piManager) setStatus(st piStatus) {
	m.mu.Lock()
	m.last = st
	m.mu.Unlock()
}

// status returns the cached pi state for the UI. approveSupported drives the
// degraded-mode banner: when false, sessions run without --approve.
func (m *piManager) status() map[string]any {
	m.mu.Lock()
	st := m.last
	cur := m.cur
	approve := m.flags["approve"]
	m.mu.Unlock()
	out := map[string]any{
		"current":          cur,
		"latest":           st.Latest,
		"available":        st.Available,
		"error":            st.Error,
		"autoUpdate":       m.auto.Load(),
		"approveSupported": approve,
	}
	if !st.CheckedAt.IsZero() {
		out["checkedAt"] = st.CheckedAt
	}
	return out
}

// setAuto flips pi auto-upgrade and persists it, preserving the pi-web
// auto-update toggle in the same file.
func (m *piManager) setAuto(enabled bool) error {
	m.auto.Store(enabled)
	s, _ := loadSettings(m.settingsPath)
	s.AutoUpdatePi = enabled
	return saveSettings(m.settingsPath, s)
}

// probePi runs `pi --help` and `pi --version` to learn pi's supported flags
// and installed version. It is the default probe collaborator.
func (m *piManager) probePi(ctx context.Context) (string, map[string]bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	help, err := m.piOutput(ctx, "--help")
	if err != nil {
		return "", nil, fmt.Errorf("pi --help: %w", err)
	}
	ver, err := m.piOutput(ctx, "--version")
	if err != nil {
		return "", nil, fmt.Errorf("pi --version: %w", err)
	}
	return strings.TrimSpace(ver), parseHelpFlags(help), nil
}

// piOutput runs pi with the given extra args (mirroring how the supervisor
// spawns pi, so provider configuration resolves identically) and returns its
// combined stdout+stderr; pi prints --help/--version to either stream.
func (m *piManager) piOutput(ctx context.Context, args ...string) (string, error) {
	if len(m.piCommand) == 0 {
		return "", fmt.Errorf("empty pi command")
	}
	full := append(append([]string{}, m.piCommand[1:]...), args...)
	cmd := exec.CommandContext(ctx, m.piCommand[0], full...)
	cmd.Dir = m.workspace
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// parseHelpFlags extracts the long-flag names (without leading dashes) from
// pi's --help output. Lines list flags as "  --flag, -f <arg>  description";
// only the long forms are recorded.
func parseHelpFlags(help string) map[string]bool {
	flags := make(map[string]bool)
	for line := range strings.Lines(help) {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "--") {
			continue
		}
		// Take the token up to the first space/comma; strip a trailing "=".
		field := strings.FieldsFunc(trimmed, func(r rune) bool {
			return r == ' ' || r == ',' || r == '\t' || r == '='
		})
		if len(field) == 0 {
			continue
		}
		name := strings.TrimLeft(field[0], "-")
		if name != "" {
			flags[name] = true
		}
	}
	return flags
}

// fetchLatest reads the pi package's latest published version from the npm
// registry. It is the default registry collaborator.
func (m *piManager) fetchLatest(ctx context.Context) (string, error) {
	return fetchLatestFrom(m.client, piRegistryURL, ctx)
}

// fetchLatestFrom GETs an npm-registry "latest" document and returns its
// version. Split out from fetchLatest so tests can point it at a fake registry.
func fetchLatestFrom(client *http.Client, url string, ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &pkg); err != nil {
		return "", fmt.Errorf("parse registry metadata: %w", err)
	}
	if pkg.Version == "" {
		return "", fmt.Errorf("registry metadata has no version")
	}
	return pkg.Version, nil
}

// runUpgrade shells out to `pi update pi` — pi owns its install mechanism, so
// pi-web never reimplements npm. It is the default upgrade collaborator.
func (m *piManager) runUpgrade(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if len(m.piCommand) == 0 {
		return fmt.Errorf("empty pi command")
	}
	full := append(append([]string{}, m.piCommand[1:]...), "update", "pi")
	cmd := exec.CommandContext(ctx, m.piCommand[0], full...)
	cmd.Dir = m.workspace
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pi update pi: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
