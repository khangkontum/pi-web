// Small client-side preferences (not session state): persisted per-browser in
// localStorage. Server-side preferences (auto-update toggles) live in the Go
// settings file, not here.

const KEY = "pi-web:prefs";

interface Prefs {
  settleSound: boolean;
}

const DEFAULTS: Prefs = { settleSound: false };

function read(): Prefs {
  try {
    const raw = localStorage.getItem(KEY);
    if (raw) return { ...DEFAULTS, ...JSON.parse(raw) };
  } catch {
    /* fall through */
  }
  return { ...DEFAULTS };
}

const stored = read();

class PrefsStore {
  settleSound = $state(stored.settleSound);

  setSettleSound(on: boolean): void {
    this.settleSound = on;
    this.save();
  }

  private save(): void {
    const p: Prefs = {
      settleSound: this.settleSound,
    };
    try {
      localStorage.setItem(KEY, JSON.stringify(p));
    } catch {
      /* best effort */
    }
  }
}

export const prefs = new PrefsStore();
