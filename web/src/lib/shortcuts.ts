// The keyboard map, in one place: the global handler and the help popover
// both read from this table so they cannot drift.

export interface Shortcut {
  keys: string;
  does: string;
}

export const SHORTCUTS: Shortcut[] = [
  { keys: "Enter", does: "Send message (steers while the agent runs)" },
  { keys: "Shift+Enter", does: "New line" },
  { keys: "! command", does: "Run a shell command in the session cwd" },
  { keys: "@", does: "Insert a file path (fuzzy finder)" },
  { keys: "Esc", does: "Close overlay / leave finder" },
  { keys: "⌘K / Ctrl+K", does: "New session" },
  { keys: "⌘B / Ctrl+B", does: "Toggle session rail" },
  { keys: "⌘E / Ctrl+E", does: "Toggle file explorer" },
  { keys: "Ctrl+`", does: "Toggle private terminal (the agent cannot see it)" },
  { keys: "⌘, / Ctrl+,", does: "Settings" },
  { keys: "⌘. / Ctrl+.", does: "Stop the current turn" },
  { keys: "?", does: "This help (when composer is empty)" },
];

// matchChord normalizes a KeyboardEvent to the chord strings used above.
export function matchChord(e: KeyboardEvent): string | null {
  const mod = e.metaKey || e.ctrlKey;
  if (mod && !e.shiftKey && !e.altKey) {
    if (e.key === "k") return "new";
    if (e.key === "b") return "rail";
    if (e.key === "e") return "explorer";
    if (e.key === "`") return "terminal";
    if (e.key === ",") return "settings";
    if (e.key === ".") return "stop";
  }
  return null;
}
