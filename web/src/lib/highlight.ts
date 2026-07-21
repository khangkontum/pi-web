// Lazy shiki wrapper shared by the file popup and every diff view. Output is
// token arrays (data) rendered through Svelte templates — nothing here
// produces HTML strings, keeping the app-wide no-innerHTML rule intact.
// Colors come from the css-variables theme; the actual values live in app.css
// as --shiki-* tokens mapped onto the existing palette.

import { bundledLanguages, createCssVariablesTheme, createHighlighter, type HighlighterGeneric } from "shiki";

export interface Token {
  content: string;
  color?: string;
}

export type TokenLine = Token[];

// Beyond this many characters, tokenizing jank outweighs the highlight —
// callers render plain text instead.
export const HIGHLIGHT_LIMIT = 200_000;

const extToLang: Record<string, string> = {
  ts: "typescript",
  tsx: "tsx",
  mts: "typescript",
  cts: "typescript",
  js: "javascript",
  jsx: "jsx",
  mjs: "javascript",
  cjs: "javascript",
  svelte: "svelte",
  vue: "vue",
  go: "go",
  py: "python",
  rs: "rust",
  rb: "ruby",
  java: "java",
  kt: "kotlin",
  c: "c",
  h: "c",
  cpp: "cpp",
  cc: "cpp",
  hpp: "cpp",
  cs: "csharp",
  swift: "swift",
  php: "php",
  sh: "shellscript",
  bash: "shellscript",
  zsh: "shellscript",
  fish: "fish",
  json: "json",
  jsonc: "jsonc",
  yaml: "yaml",
  yml: "yaml",
  toml: "toml",
  html: "html",
  css: "css",
  scss: "scss",
  less: "less",
  md: "markdown",
  sql: "sql",
  xml: "xml",
  lua: "lua",
  zig: "zig",
  graphql: "graphql",
  proto: "proto",
  ini: "ini",
  diff: "diff",
  tf: "terraform",
  dockerfile: "docker",
};

const nameToLang: Record<string, string> = {
  dockerfile: "docker",
  makefile: "make",
};

// langForPath maps a file path to a shiki language id, or null when the file
// should render plain.
export function langForPath(path: string): string | null {
  const base = path.slice(path.lastIndexOf("/") + 1).toLowerCase();
  if (nameToLang[base]) return nameToLang[base];
  const dot = base.lastIndexOf(".");
  if (dot < 0) return null;
  const lang = extToLang[base.slice(dot + 1)];
  return lang && lang in bundledLanguages ? lang : null;
}

const cssTheme = createCssVariablesTheme({ name: "css-variables", variablePrefix: "--shiki-" });

let highlighterPromise: Promise<HighlighterGeneric<never, never>> | null = null;
const loadedLangs = new Set<string>();

function getHighlighter(): Promise<HighlighterGeneric<never, never>> {
  highlighterPromise ??= createHighlighter({ themes: [cssTheme], langs: [] }) as unknown as Promise<
    HighlighterGeneric<never, never>
  >;
  return highlighterPromise;
}

// tokenize returns one token line per input line, or null when the language
// is unknown or the content is too large — callers then render plain text.
// It never throws; a shiki failure degrades to plain rendering.
export async function tokenize(code: string, lang: string | null): Promise<TokenLine[] | null> {
  if (!lang || code.length > HIGHLIGHT_LIMIT) return null;
  try {
    const hl = await getHighlighter();
    if (!loadedLangs.has(lang)) {
      await hl.loadLanguage(lang as never);
      loadedLangs.add(lang);
    }
    return hl.codeToTokensBase(code, { lang: lang as never, theme: "css-variables" });
  } catch {
    return null;
  }
}
