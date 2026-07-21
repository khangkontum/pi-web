import { describe, expect, it } from "vitest";
import { parsePatch } from "./patch";

const MODIFY = `diff --git a/src/a.ts b/src/a.ts
index 1111111..2222222 100644
--- a/src/a.ts
+++ b/src/a.ts
@@ -1,3 +1,3 @@
 const x = 1;
-const y = 2;
+const y = 3;
 export { x, y };
@@ -10,2 +10,3 @@
 tail();
+more();
`;

const ADD_UNTRACKED = `diff --git a/new.txt b/new.txt
new file mode 100644
index 0000000..e69de29
--- /dev/null
+++ b/new.txt
@@ -0,0 +1,2 @@
+fresh
+lines
`;

const DELETE = `diff --git a/gone.txt b/gone.txt
deleted file mode 100644
index e69de29..0000000
--- a/gone.txt
+++ /dev/null
@@ -1,1 +0,0 @@
-old
`;

const RENAME = `diff --git a/old-name.ts b/new-name.ts
similarity index 95%
rename from old-name.ts
rename to new-name.ts
index 1111111..2222222 100644
--- a/old-name.ts
+++ b/new-name.ts
@@ -1,1 +1,1 @@
-a
+b
`;

const BINARY = `diff --git a/img.png b/img.png
new file mode 100644
index 0000000..1234567
Binary files /dev/null and b/img.png differ
`;

describe("parsePatch", () => {
  it("parses a modification with multiple hunks", () => {
    const files = parsePatch(MODIFY);
    expect(files).toHaveLength(1);
    const f = files[0];
    expect(f.path).toBe("src/a.ts");
    expect(f.status).toBe("modify");
    expect(f.adds).toBe(2);
    expect(f.dels).toBe(1);
    // gap row separates the two hunks
    expect(f.rows.filter((r) => r.kind === "gap")).toHaveLength(1);
    expect(f.rows[0]).toEqual({ kind: "ctx", text: "const x = 1;" });
    expect(f.rows[1]).toEqual({ kind: "del", text: "const y = 2;" });
    expect(f.rows[2]).toEqual({ kind: "add", text: "const y = 3;" });
  });

  it("marks added files including untracked --no-index output", () => {
    const files = parsePatch(ADD_UNTRACKED);
    expect(files).toHaveLength(1);
    expect(files[0].status).toBe("add");
    expect(files[0].path).toBe("new.txt");
    expect(files[0].adds).toBe(2);
  });

  it("marks deleted files and keeps a display path", () => {
    const files = parsePatch(DELETE);
    expect(files[0].status).toBe("del");
    expect(files[0].path).toBe("gone.txt");
    expect(files[0].dels).toBe(1);
  });

  it("tracks renames with both paths", () => {
    const files = parsePatch(RENAME);
    expect(files[0].status).toBe("rename");
    expect(files[0].oldPath).toBe("old-name.ts");
    expect(files[0].path).toBe("new-name.ts");
  });

  it("flags binary files without rows", () => {
    const files = parsePatch(BINARY);
    expect(files[0].binary).toBe(true);
    expect(files[0].rows).toHaveLength(0);
  });

  it("splits multi-file patches", () => {
    const files = parsePatch(MODIFY + ADD_UNTRACKED + DELETE);
    expect(files.map((f) => f.path)).toEqual(["src/a.ts", "new.txt", "gone.txt"]);
  });

  it("returns nothing for an empty patch", () => {
    expect(parsePatch("")).toEqual([]);
  });
});
