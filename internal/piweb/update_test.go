package piweb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		current, candidate string
		want               bool
	}{
		{"v0.1.0", "v0.2.0", true},
		{"v0.1.0", "v0.1.1", true},
		{"v0.9.0", "v1.0.0", true},
		{"v1.2.3", "v1.2.3", false},
		{"v0.2.0", "v0.1.9", false},
		{"v1.0.0", "v0.9.9", false},
		{"dev", "v9.9.9", false},
		{"v0.1.0", "dev", false},
		{"v0.1.0", "", false},
		{"v0.1.0", "v0.10.0", true},
		{"0.1.0", "0.2", true},
	}
	for _, c := range cases {
		if got := newerVersion(c.current, c.candidate); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", c.current, c.candidate, got, c.want)
		}
	}
}

func TestChecksumFor(t *testing.T) {
	sums := "aaaa  pi-web_linux_amd64\nbbbb  pi-web_darwin_arm64\n"
	got, err := checksumFor(sums, "pi-web_darwin_arm64")
	if err != nil || got != "bbbb" {
		t.Fatalf("checksumFor = %q, %v", got, err)
	}
	if _, err := checksumFor(sums, "pi-web_windows_amd64"); err == nil {
		t.Fatal("expected error for missing entry")
	}
}

func TestApplyBinaryDirect(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "pi-web")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := applyBinary(exe, []byte("new")); err != nil {
		t.Fatalf("applyBinary: %v", err)
	}
	data, err := os.ReadFile(exe)
	if err != nil || string(data) != "new" {
		t.Fatalf("binary content = %q, %v", data, err)
	}
	info, err := os.Stat(exe)
	if err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("binary not executable: %v %v", info.Mode(), err)
	}
	if _, err := os.Stat(exe + ".new"); !os.IsNotExist(err) {
		t.Fatalf(".new left behind: %v", err)
	}
}

// updateFixture serves a complete fake release: metadata, checksums, and the
// binary itself.
func updateFixture(t *testing.T, version string, binary []byte, corruptSum bool) *httptest.Server {
	t.Helper()
	key := runtime.GOOS + "_" + runtime.GOARCH
	sum := sha256.Sum256(binary)
	sumHex := hex.EncodeToString(sum[:])
	if corruptSum {
		sumHex = "deadbeef" + sumHex[8:]
	}

	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/release.json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(releaseInfo{
			Version:      version,
			PublishedAt:  time.Now(),
			ChecksumsURL: srv.URL + "/checksums.txt",
			DownloadURLs: map[string]string{key: srv.URL + "/bin"},
		})
	})
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  pi-web_%s\n", sumHex, key)
	})
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) {
		w.Write(binary)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func testUpdater(t *testing.T, srv *httptest.Server, current string) (*updater, string, *atomic.Int32) {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "pi-web")
	if err := os.WriteFile(exe, []byte("current"), 0o755); err != nil {
		t.Fatal(err)
	}
	restarts := &atomic.Int32{}
	u := &updater{
		url:      srv.URL + "/release.json",
		interval: time.Hour,
		version:  current,
		client:   srv.Client(),
		logw:     testWriter{t},
		exePath:  func() (string, error) { return exe, nil },
		apply:    applyBinary,
		restart:  func() { restarts.Add(1) },
	}
	return u, exe, restarts
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(string(p))
	return len(p), nil
}

func TestUpdaterAppliesNewerRelease(t *testing.T) {
	srv := updateFixture(t, "v9.9.9", []byte("shiny new binary"), false)
	u, exe, restarts := testUpdater(t, srv, "v0.1.0")

	if err := u.checkAndApply(context.Background()); err != nil {
		t.Fatalf("checkAndApply: %v", err)
	}
	data, _ := os.ReadFile(exe)
	if string(data) != "shiny new binary" {
		t.Fatalf("binary not replaced: %q", data)
	}
	if restarts.Load() != 1 {
		t.Fatalf("restarts = %d, want 1", restarts.Load())
	}
}

func TestUpdaterSkipsSameAndOlder(t *testing.T) {
	for _, version := range []string{"v0.1.0", "v0.0.9"} {
		srv := updateFixture(t, version, []byte("should never land"), false)
		u, exe, restarts := testUpdater(t, srv, "v0.1.0")

		if err := u.checkAndApply(context.Background()); err != nil {
			t.Fatalf("checkAndApply(%s): %v", version, err)
		}
		data, _ := os.ReadFile(exe)
		if string(data) != "current" {
			t.Fatalf("binary replaced by %s: %q", version, data)
		}
		if restarts.Load() != 0 {
			t.Fatalf("restarted for %s", version)
		}
	}
}

func TestUpdaterRejectsChecksumMismatch(t *testing.T) {
	srv := updateFixture(t, "v9.9.9", []byte("tampered"), true)
	u, exe, restarts := testUpdater(t, srv, "v0.1.0")

	err := u.checkAndApply(context.Background())
	if err == nil {
		t.Fatal("expected checksum error")
	}
	data, _ := os.ReadFile(exe)
	if string(data) != "current" {
		t.Fatalf("binary replaced despite bad checksum: %q", data)
	}
	if restarts.Load() != 0 {
		t.Fatal("restarted despite bad checksum")
	}
}
