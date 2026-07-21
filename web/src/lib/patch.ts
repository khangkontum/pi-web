// Unified-diff parsing for the git overlay. The server sends one patch string
// (`git diff` / `git show`); this splits it into per-file sections reusing the
// DiffRow shape DiffView already renders. Rows are data; components turn them
// into DOM through templates, never HTML strings.

import type { DiffRow } from "./diff";

export type PatchStatus = "add" | "del" | "rename" | "modify";

export interface PatchFile {
  path: string;
  oldPath: string | null;
  status: PatchStatus;
  binary: boolean;
  rows: DiffRow[];
  adds: number;
  dels: number;
}

function stripPrefix(p: string): string {
  const unquoted = p.startsWith('"') && p.endsWith('"') ? p.slice(1, -1) : p;
  if (unquoted === "/dev/null") return "/dev/null";
  return unquoted.replace(/^[ab]\//, "");
}

function newFile(): PatchFile {
  return { path: "", oldPath: null, status: "modify", binary: false, rows: [], adds: 0, dels: 0 };
}

export function parsePatch(patch: string): PatchFile[] {
  const files: PatchFile[] = [];
  let file: PatchFile | null = null;
  let inHunk = false;

  const push = () => {
    if (file && (file.path || file.oldPath)) files.push(file);
  };

  for (const line of patch.split("\n")) {
    if (line.startsWith("diff --git ")) {
      push();
      file = newFile();
      inHunk = false;
      // Fallback paths from the header; --- / +++ lines refine them.
      const m = line.match(/^diff --git "?a\/(.+?)"? "?b\/(.+?)"?$/);
      if (m) {
        file.oldPath = m[1];
        file.path = m[2];
      }
      continue;
    }
    if (!file) continue;

    if (!inHunk) {
      if (line.startsWith("new file mode")) {
        file.status = "add";
      } else if (line.startsWith("deleted file mode")) {
        file.status = "del";
      } else if (line.startsWith("rename from ")) {
        file.status = "rename";
        file.oldPath = line.slice("rename from ".length);
      } else if (line.startsWith("rename to ")) {
        file.path = line.slice("rename to ".length);
      } else if (line.startsWith("--- ")) {
        const p = stripPrefix(line.slice(4));
        if (p === "/dev/null") file.status = "add";
        else file.oldPath = p;
      } else if (line.startsWith("+++ ")) {
        const p = stripPrefix(line.slice(4));
        if (p === "/dev/null") file.status = "del";
        else file.path = p;
      } else if (line.startsWith("Binary files ") || line.startsWith("GIT binary patch")) {
        file.binary = true;
      }
    }

    if (line.startsWith("@@")) {
      inHunk = true;
      if (file.rows.length > 0) file.rows.push({ kind: "gap" });
      continue;
    }
    if (!inHunk) continue;

    if (line.startsWith("+")) {
      file.rows.push({ kind: "add", text: line.slice(1) });
      file.adds++;
    } else if (line.startsWith("-")) {
      file.rows.push({ kind: "del", text: line.slice(1) });
      file.dels++;
    } else if (line.startsWith(" ") || line === "") {
      file.rows.push({ kind: "ctx", text: line.slice(1) });
    }
    // "\ No newline at end of file" and anything else inside a hunk is dropped.
  }
  push();

  for (const f of files) {
    if (f.status === "del" && !f.path && f.oldPath) f.path = f.oldPath;
    if (!f.path && f.oldPath) f.path = f.oldPath;
  }
  return files;
}
