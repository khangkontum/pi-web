package piweb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseHelpFlags(t *testing.T) {
	help := `Options:
  --mode <mode>                  Output mode
  --approve, -a                  Trust project-local files for this run
  --no-approve, -na              Ignore project-local files
  --session-dir <dir>            Directory for session storage
  --version, -v                  Show version number
  not a flag line
`
	flags := parseHelpFlags(help)
	for _, want := range []string{"mode", "approve", "no-approve", "session-dir", "version"} {
		if !flags[want] {
			t.Errorf("expected flag %q parsed, got %v", want, flags)
		}
	}
	if flags["not"] {
		t.Error("non-flag line should not be parsed as a flag")
	}
}

// fakePiManager builds a piManager with injected collaborators and no network
// or real pi process.
func fakePiManager(t *testing.T) *piManager {
	t.Helper()
	cfg := helperConfig(t)
	return newPiManager(cfg, testWriter{t})
}

func TestPiManagerCheckReportsAvailable(t *testing.T) {
	m := fakePiManager(t)
	m.probe = func(context.Context) (string, map[string]bool, error) {
		return "0.80.1", map[string]bool{"approve": true}, nil
	}
	m.registry = func(context.Context) (string, error) { return "0.81.0", nil }
	m.bootProbe(context.Background())

	st, err := m.check(context.Background())
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if st.Latest != "0.81.0" || !st.Available {
		t.Fatalf("expected available upgrade, got %+v", st)
	}
	status := m.status()
	if status["approveSupported"] != true {
		t.Fatalf("expected approveSupported true, got %v", status["approveSupported"])
	}
}

func TestPiManagerCheckNoUpgrade(t *testing.T) {
	m := fakePiManager(t)
	m.probe = func(context.Context) (string, map[string]bool, error) {
		return "0.81.0", map[string]bool{"approve": true}, nil
	}
	m.registry = func(context.Context) (string, error) { return "0.81.0", nil }
	m.bootProbe(context.Background())

	st, err := m.check(context.Background())
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if st.Available {
		t.Fatalf("same version should not be available: %+v", st)
	}
}

func TestPiManagerUpgradeReprobesAndRecycles(t *testing.T) {
	m := fakePiManager(t)
	probeVer := "0.80.0"
	m.probe = func(context.Context) (string, map[string]bool, error) {
		return probeVer, map[string]bool{"approve": true}, nil
	}
	m.registry = func(context.Context) (string, error) { return "0.81.0", nil }
	upgraded := false
	m.upgrade = func(context.Context) error {
		upgraded = true
		probeVer = "0.81.0" // the re-probe now sees the new version
		return nil
	}
	recycled := false
	m.recycle = func() { recycled = true }
	m.bootProbe(context.Background())

	if _, err := m.check(context.Background()); err != nil {
		t.Fatalf("check: %v", err)
	}
	if err := m.applyUpgrade(context.Background()); err != nil {
		t.Fatalf("applyUpgrade: %v", err)
	}
	if !upgraded || !recycled {
		t.Fatalf("upgrade=%v recycle=%v, want both true", upgraded, recycled)
	}
	if m.current() != "0.81.0" {
		t.Fatalf("current pi not re-probed: %q", m.current())
	}
	if st := m.status(); st["available"] != false {
		t.Fatalf("available should clear after upgrade, got %v", st["available"])
	}
}

// TestPiManagerDegradesWithoutApprove asserts that a probe reporting no
// --approve support makes the supervisor omit the flag rather than refuse.
func TestPiManagerDegradesWithoutApprove(t *testing.T) {
	cfg := helperConfig(t)
	sv := newSupervisor(cfg)
	t.Cleanup(sv.closeAll)

	m := newPiManager(cfg, testWriter{t})
	m.probe = func(context.Context) (string, map[string]bool, error) {
		return "0.50.0", map[string]bool{"mode": true}, nil // no "approve"
	}
	sv.pi = m
	m.bootProbe(context.Background())

	argv := sv.piCommand("")
	for _, a := range argv {
		if a == "--approve" {
			t.Fatalf("--approve should be omitted for a pi that lacks it: %v", argv)
		}
	}

	// And when supported, it is present.
	m2 := newPiManager(cfg, testWriter{t})
	m2.probe = func(context.Context) (string, map[string]bool, error) {
		return "0.80.1", map[string]bool{"approve": true, "mode": true}, nil
	}
	sv.pi = m2
	m2.bootProbe(context.Background())
	if !containsArg(sv.piCommand(""), "--approve") {
		t.Fatalf("--approve should be present when supported: %v", sv.piCommand(""))
	}
}

func containsArg(argv []string, want string) bool {
	for _, a := range argv {
		if a == want {
			return true
		}
	}
	return false
}

func TestPiManagerAutoPersists(t *testing.T) {
	cfg := helperConfig(t)
	m := newPiManager(cfg, testWriter{t})
	if err := m.setAuto(true); err != nil {
		t.Fatalf("setAuto: %v", err)
	}
	s, ok := loadSettings(cfg.SettingsPath)
	if !ok || !s.AutoUpdatePi {
		t.Fatalf("autoUpdatePi not persisted: %+v ok=%v", s, ok)
	}
	// pi-web auto-update must be preserved when pi-web's own setAuto runs after.
	u := newUpdater(cfg, testWriter{t})
	if err := u.setAuto(true); err != nil {
		t.Fatalf("updater setAuto: %v", err)
	}
	s2, _ := loadSettings(cfg.SettingsPath)
	if !s2.AutoUpdate || !s2.AutoUpdatePi {
		t.Fatalf("toggles clobbered each other: %+v", s2)
	}
}

func TestPiManagerRegistryDefault(t *testing.T) {
	// Exercise the default fetchLatest against a fake npm registry.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"0.99.0","name":"@earendil-works/pi-coding-agent"}`))
	}))
	defer srv.Close()

	m := fakePiManager(t)
	m.client = srv.Client()
	// Point fetchLatest at the fake registry by wrapping it.
	got, err := fetchLatestFrom(m.client, srv.URL, context.Background())
	if err != nil {
		t.Fatalf("fetchLatestFrom: %v", err)
	}
	if got != "0.99.0" {
		t.Fatalf("version = %q, want 0.99.0", got)
	}
}
