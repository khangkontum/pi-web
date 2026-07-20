// Small client-side preferences (not session state): persisted per-browser in
// localStorage. Server-side preferences (auto-update toggles) live in the Go
// settings file, not here.

const KEY = "pi-web:prefs";

interface Prefs {
  settleSound: boolean;
}

function read(): Prefs {
  try {
    const raw = localStorage.getItem(KEY);
    if (raw) return { settleSound: false, ...JSON.parse(raw) };
  } catch {
    /* fall through */
  }
  return { settleSound: false };
}

class PrefsStore {
  settleSound = $state(read().settleSound);

  setSettleSound(on: boolean): void {
    this.settleSound = on;
    try {
      localStorage.setItem(KEY, JSON.stringify({ settleSound: on }));
    } catch {
      /* best effort */
    }
  }
}

export const prefs = new PrefsStore();
