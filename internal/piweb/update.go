package piweb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// DefaultUpdateURL is the stable release-metadata URL: it always points at
// the latest release's release.json asset, so update checks never touch the
// GitHub API.
const DefaultUpdateURL = "https://github.com/khangkontum/pi-web/releases/latest/download/release.json"

// DefaultUpdateInterval is how often pi-web checks for a new release.
const DefaultUpdateInterval = 6 * time.Hour

// updateInitialDelay spaces the first check away from process start so a
// crash-looping binary cannot hammer the release endpoint.
const updateInitialDelay = time.Minute

// maxUpdateDownload bounds how much of a release download is read into
// memory before checksum verification.
const maxUpdateDownload = 512 << 20

// releaseInfo mirrors the release.json contract published with every
// release: everything a client needs to check for and verify an update.
type releaseInfo struct {
	Version      string            `json:"version"`
	Commit       string            `json:"commit"`
	PublishedAt  time.Time         `json:"published_at"`
	ChecksumsURL string            `json:"checksums_url"`
	DownloadURLs map[string]string `json:"download_urls"`
}

// updater checks for new releases and, when asked, replaces the running
// binary. Auto-apply is opt-in (see auto); the background loop always
// refreshes status so the UI can offer a manual update. All collaborators
// are injectable so tests never touch the network, the real executable, or
// sudo.
type updater struct {
	url          string
	interval     time.Duration
	version      string
	settingsPath string
	client       *http.Client
	logw         io.Writer
	exePath      func() (string, error)
	apply        func(exePath string, data []byte) error
	restart      func()

	auto atomic.Bool

	mu   sync.Mutex
	last updateStatus
}

// updateStatus is the cached result of the most recent check, surfaced to the
// UI. CheckedAt is the zero time until the first check completes.
type updateStatus struct {
	Latest    string    `json:"latest"`
	Available bool      `json:"available"`
	CheckedAt time.Time `json:"checkedAt"`
	Error     string    `json:"error"`
}

func newUpdater(cfg Config, logw io.Writer) *updater {
	u := &updater{
		url:          cfg.UpdateURL,
		interval:     cfg.UpdateInterval,
		version:      cfg.Version,
		settingsPath: cfg.SettingsPath,
		client:       &http.Client{Timeout: 2 * time.Minute},
		logw:         logw,
		exePath:      os.Executable,
		apply:        applyBinary,
		restart:      restartSelf,
	}
	auto := cfg.AutoUpdate
	if s, ok := loadSettings(cfg.SettingsPath); ok {
		auto = s.AutoUpdate
	}
	u.auto.Store(auto)
	return u
}

// canUpdate reports whether this build can self-update at all. Dev builds
// (unparsable version) never do.
func (u *updater) canUpdate() bool {
	_, ok := parseVersion(u.version)
	return ok
}

func (u *updater) run(ctx context.Context) {
	timer := time.NewTimer(updateInitialDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		st, err := u.check(ctx)
		if err != nil {
			fmt.Fprintf(u.logw, "pi-web: update check: %v\n", err)
		} else if st.Available && u.auto.Load() {
			if v, err := u.installLatest(ctx); err != nil {
				fmt.Fprintf(u.logw, "pi-web: auto-update: %v\n", err)
			} else if v != "" {
				fmt.Fprintf(u.logw, "pi-web: auto-updated %s -> %s, restarting\n", u.version, v)
				u.restart()
			}
		}
		timer.Reset(u.interval)
	}
}

// check fetches release metadata, compares versions, and caches the result.
// It has no side effects on disk.
func (u *updater) check(ctx context.Context) (updateStatus, error) {
	st := updateStatus{CheckedAt: time.Now()}
	rel, err := u.fetchRelease(ctx)
	if err != nil {
		st.Error = err.Error()
		u.setStatus(st)
		return st, err
	}
	st.Latest = rel.Version
	st.Available = newerVersion(u.version, rel.Version)
	u.setStatus(st)
	return st, nil
}

// installLatest applies a strictly-newer release: it downloads it, verifies
// its checksum in memory, and renames it over the running executable. It does
// not restart. It returns the installed version, or "" when already current.
// Any failure leaves the installed binary untouched.
func (u *updater) installLatest(ctx context.Context) (string, error) {
	rel, err := u.fetchRelease(ctx)
	if err != nil {
		return "", err
	}
	if !newerVersion(u.version, rel.Version) {
		return "", nil
	}
	data, err := u.downloadVerified(ctx, rel)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", rel.Version, err)
	}
	exe, err := u.exePath()
	if err != nil {
		return "", err
	}
	if err := u.apply(exe, data); err != nil {
		return "", fmt.Errorf("apply %s: %w", rel.Version, err)
	}
	return rel.Version, nil
}

// checkAndApply installs a newer release and restarts. It is the auto-apply
// primitive exercised directly by tests.
func (u *updater) checkAndApply(ctx context.Context) error {
	v, err := u.installLatest(ctx)
	if err != nil {
		return err
	}
	if v != "" {
		fmt.Fprintf(u.logw, "pi-web: updated %s -> %s, restarting\n", u.version, v)
		u.restart()
	}
	return nil
}

func (u *updater) setStatus(st updateStatus) {
	u.mu.Lock()
	u.last = st
	u.mu.Unlock()
}

// status returns the cached check result plus the static build facts the UI
// needs.
func (u *updater) status() map[string]any {
	u.mu.Lock()
	st := u.last
	u.mu.Unlock()
	out := map[string]any{
		"current":    u.version,
		"latest":     st.Latest,
		"available":  st.Available,
		"error":      st.Error,
		"autoUpdate": u.auto.Load(),
		"canUpdate":  u.canUpdate(),
	}
	if !st.CheckedAt.IsZero() {
		out["checkedAt"] = st.CheckedAt
	}
	return out
}

// setAuto flips auto-apply and persists the choice so it survives restarts.
func (u *updater) setAuto(enabled bool) error {
	u.auto.Store(enabled)
	return saveSettings(u.settingsPath, settings{AutoUpdate: enabled})
}

func (u *updater) fetchRelease(ctx context.Context) (releaseInfo, error) {
	var rel releaseInfo
	body, err := u.get(ctx, u.url, 1<<20)
	if err != nil {
		return rel, err
	}
	if err := json.Unmarshal(body, &rel); err != nil {
		return rel, fmt.Errorf("parse release metadata: %w", err)
	}
	if rel.Version == "" {
		return rel, errors.New("release metadata has no version")
	}
	return rel, nil
}

func (u *updater) downloadVerified(ctx context.Context, rel releaseInfo) ([]byte, error) {
	key := runtime.GOOS + "_" + runtime.GOARCH
	url := rel.DownloadURLs[key]
	if url == "" {
		return nil, fmt.Errorf("no download for %s", key)
	}
	data, err := u.get(ctx, url, maxUpdateDownload)
	if err != nil {
		return nil, err
	}
	if rel.ChecksumsURL == "" {
		return nil, errors.New("release metadata has no checksums_url")
	}
	sums, err := u.get(ctx, rel.ChecksumsURL, 1<<20)
	if err != nil {
		return nil, err
	}
	want, err := checksumFor(string(sums), "pi-web_"+key)
	if err != nil {
		return nil, err
	}
	got := sha256.Sum256(data)
	if hex.EncodeToString(got[:]) != want {
		return nil, fmt.Errorf("checksum mismatch for pi-web_%s", key)
	}
	return data, nil
}

func (u *updater) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// checksumFor extracts the sha256 for name from checksums.txt content
// ("<hex>  <name>" per line).
func checksumFor(sums, name string) (string, error) {
	for line := range strings.Lines(sums) {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == name {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no checksum entry for %s", name)
}

// newerVersion reports whether candidate is a strictly newer vX.Y.Z than
// current. Anything unparsable on either side (dev builds, malformed
// metadata) means no update.
func newerVersion(current, candidate string) bool {
	cur, ok := parseVersion(current)
	if !ok {
		return false
	}
	cand, ok := parseVersion(candidate)
	if !ok {
		return false
	}
	for i := range cur {
		if cand[i] != cur[i] {
			return cand[i] > cur[i]
		}
	}
	return false
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.Split(v, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// applyBinary replaces the executable at exePath with data. It always goes
// through a sibling ".new" file and a rename: a rename swaps the directory
// entry without opening the file, so it succeeds while the old binary is
// running (an in-place write would fail with ETXTBSY). When the directory is
// not writable it falls back to non-interactive sudo.
func applyBinary(exePath string, data []byte) error {
	err := applyDirect(exePath, data)
	if err == nil || !errors.Is(err, fs.ErrPermission) {
		return err
	}
	if sudoErr := applySudo(exePath, data); sudoErr != nil {
		return fmt.Errorf("%w (sudo fallback: %v)", err, sudoErr)
	}
	return nil
}

func applyDirect(exePath string, data []byte) error {
	newPath := exePath + ".new"
	if err := os.WriteFile(newPath, data, 0o755); err != nil {
		return err
	}
	if err := os.Chmod(newPath, 0o755); err != nil {
		os.Remove(newPath)
		return err
	}
	if err := os.Rename(newPath, exePath); err != nil {
		os.Remove(newPath)
		return err
	}
	return nil
}

// applySudo installs data over exePath with `sudo -n` (never prompts). The
// original owner and mode are preserved, and the old binary is kept as .old
// until the swap succeeds.
func applySudo(exePath string, data []byte) error {
	info, err := os.Stat(exePath)
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("cannot determine binary ownership")
	}

	tmp, err := os.CreateTemp("", "pi-web-update-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	newPath := exePath + ".new"
	oldPath := exePath + ".old"
	steps := [][]string{
		{"cp", tmpPath, newPath},
		{"chown", fmt.Sprintf("%d:%d", st.Uid, st.Gid), newPath},
		{"chmod", fmt.Sprintf("%o", info.Mode().Perm()), newPath},
		{"mv", exePath, oldPath},
	}
	for _, step := range steps {
		if err := runSudo(step...); err != nil {
			runSudo("rm", "-f", newPath)
			return err
		}
	}
	if err := runSudo("mv", newPath, exePath); err != nil {
		runSudo("mv", oldPath, exePath)
		return err
	}
	runSudo("rm", "-f", oldPath)
	return nil
}

func runSudo(args ...string) error {
	cmd := exec.Command("sudo", append([]string{"-n"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sudo -n %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// restartSelf hands control to the freshly installed binary. Under systemd
// (INVOCATION_ID set) it exits non-zero so Restart=on-failure starts the new
// file; elsewhere it re-execs in place so standalone installs keep running.
func restartSelf() {
	exe, err := os.Executable()
	if err != nil || os.Getenv("INVOCATION_ID") != "" {
		os.Exit(1)
	}
	argv0, err := filepath.Abs(exe)
	if err == nil {
		syscall.Exec(argv0, os.Args, os.Environ())
	}
	// Exec only returns on error; a restart is still owed.
	os.Exit(1)
}
