package piweb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// settings is pi-web's only persisted state: a small user-preference file.
// It deliberately steps outside the otherwise-stateless design so the
// auto-update choice survives restarts; nothing session-related is stored
// here. An empty path disables persistence (the value stays in memory).
type settings struct {
	AutoUpdate bool `json:"autoUpdate"`
}

var settingsMu sync.Mutex

// loadSettings reads the preference file. A missing or unreadable file is a
// normal "no stored preference" state, reported by ok=false.
func loadSettings(path string) (settings, bool) {
	if path == "" {
		return settings{}, false
	}
	settingsMu.Lock()
	defer settingsMu.Unlock()
	data, err := os.ReadFile(path)
	if err != nil {
		return settings{}, false
	}
	var s settings
	if err := json.Unmarshal(data, &s); err != nil {
		return settings{}, false
	}
	return s, true
}

// saveSettings writes the preference file, creating its directory. An empty
// path is a no-op.
func saveSettings(path string, s settings) error {
	if path == "" {
		return nil
	}
	settingsMu.Lock()
	defer settingsMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// defaultSettingsPath is the persisted-preference location under the user's
// config directory (honouring XDG_CONFIG_HOME on Linux).
func defaultSettingsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "pi-web", "settings.json")
}
