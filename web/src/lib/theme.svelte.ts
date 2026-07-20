// Theme: system by default, manual light/dark override persisted in
// localStorage. index.html resolves the initial theme pre-paint; this store
// keeps data-theme in sync afterwards (including live OS changes in system
// mode). data-theme always holds the *resolved* theme so CSS stays simple.

export type ThemeMode = "system" | "light" | "dark";

const KEY = "pi-web:theme";
const media = matchMedia("(prefers-color-scheme: dark)");

function readMode(): ThemeMode {
  try {
    const v = localStorage.getItem(KEY);
    return v === "light" || v === "dark" ? v : "system";
  } catch {
    return "system";
  }
}

class Theme {
  mode = $state<ThemeMode>(readMode());

  constructor() {
    media.addEventListener("change", () => this.apply());
    this.apply();
  }

  get resolved(): "light" | "dark" {
    if (this.mode === "system") return media.matches ? "dark" : "light";
    return this.mode;
  }

  apply(): void {
    document.documentElement.setAttribute("data-theme", this.resolved);
  }

  set(mode: ThemeMode): void {
    this.mode = mode;
    try {
      if (mode === "system") localStorage.removeItem(KEY);
      else localStorage.setItem(KEY, mode);
    } catch {
      /* best effort */
    }
    this.apply();
  }
}

export const theme = new Theme();
