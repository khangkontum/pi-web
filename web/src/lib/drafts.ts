// Per-session composer drafts in localStorage: survive reload and session
// switches. Storage failures (private mode, quota) degrade to no persistence.

const PREFIX = "pi-web:draft:";
// the not-yet-created session composes under this key
export const NEW_SESSION = "new";

export function loadDraft(sessionId: string): string {
  try {
    return localStorage.getItem(PREFIX + sessionId) ?? "";
  } catch {
    return "";
  }
}

export function saveDraft(sessionId: string, text: string): void {
  try {
    if (text) localStorage.setItem(PREFIX + sessionId, text);
    else localStorage.removeItem(PREFIX + sessionId);
  } catch {
    /* best effort */
  }
}

export function clearDraft(sessionId: string): void {
  saveDraft(sessionId, "");
}
