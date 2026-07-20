// SGR-subset ANSI parser for tool/bash output. Produces styled spans that a
// Svelte template renders — never HTML strings (feed XSS = shell RCE).
//
// Supported: reset, bold, dim, italic, underline, inverse off/on pairs,
// 16-color + bright, 256-color, 24-bit truecolor, default fg/bg. All other
// escape sequences (cursor movement, OSC titles, …) are stripped.

export interface AnsiSpan {
  text: string;
  // CSS color: "var(--ansi-N)" for palette colors, "#rrggbb" for extended
  fg?: string;
  bg?: string;
  bold?: boolean;
  dim?: boolean;
  italic?: boolean;
  underline?: boolean;
}

interface Style {
  fg?: string;
  bg?: string;
  bold?: boolean;
  dim?: boolean;
  italic?: boolean;
  underline?: boolean;
}

const CUBE = [0, 95, 135, 175, 215, 255];

function hex(n: number): string {
  return n.toString(16).padStart(2, "0");
}

function color256(n: number): string | undefined {
  if (n < 0 || n > 255) return undefined;
  if (n < 16) return `var(--ansi-${n})`;
  if (n < 232) {
    const i = n - 16;
    const r = CUBE[Math.floor(i / 36)];
    const g = CUBE[Math.floor(i / 6) % 6];
    const b = CUBE[i % 6];
    return `#${hex(r)}${hex(g)}${hex(b)}`;
  }
  const v = 8 + 10 * (n - 232);
  return `#${hex(v)}${hex(v)}${hex(v)}`;
}

// applySGR mutates style for one SGR parameter list; returns nothing.
function applySGR(style: Style, params: number[]): void {
  if (params.length === 0) params = [0];
  for (let i = 0; i < params.length; i++) {
    const p = params[i];
    if (p === 0) {
      delete style.fg;
      delete style.bg;
      delete style.bold;
      delete style.dim;
      delete style.italic;
      delete style.underline;
    } else if (p === 1) style.bold = true;
    else if (p === 2) style.dim = true;
    else if (p === 3) style.italic = true;
    else if (p === 4) style.underline = true;
    else if (p === 22) {
      delete style.bold;
      delete style.dim;
    } else if (p === 23) delete style.italic;
    else if (p === 24) delete style.underline;
    else if (p >= 30 && p <= 37) style.fg = `var(--ansi-${p - 30})`;
    else if (p === 39) delete style.fg;
    else if (p >= 40 && p <= 47) style.bg = `var(--ansi-${p - 40})`;
    else if (p === 49) delete style.bg;
    else if (p >= 90 && p <= 97) style.fg = `var(--ansi-${p - 90 + 8})`;
    else if (p >= 100 && p <= 107) style.bg = `var(--ansi-${p - 100 + 8})`;
    else if (p === 38 || p === 48) {
      const target = p === 38 ? "fg" : "bg";
      if (params[i + 1] === 5) {
        const c = color256(params[i + 2] ?? -1);
        if (c) style[target] = c;
        i += 2;
      } else if (params[i + 1] === 2) {
        const [r, g, b] = [params[i + 2] ?? 0, params[i + 3] ?? 0, params[i + 4] ?? 0];
        style[target] = `#${hex(r & 255)}${hex(g & 255)}${hex(b & 255)}`;
        i += 4;
      }
    }
    // anything else: ignore
  }
}

function pushSpan(spans: AnsiSpan[], text: string, style: Style): void {
  if (!text) return;
  const last = spans[spans.length - 1];
  const span: AnsiSpan = { text, ...style };
  if (
    last &&
    last.fg === span.fg &&
    last.bg === span.bg &&
    last.bold === span.bold &&
    last.dim === span.dim &&
    last.italic === span.italic &&
    last.underline === span.underline
  ) {
    last.text += text;
    return;
  }
  spans.push(span);
}

// parseAnsi turns raw terminal output into styled spans. \r is dropped;
// unknown CSI/OSC sequences are stripped.
export function parseAnsi(input: string): AnsiSpan[] {
  const spans: AnsiSpan[] = [];
  const style: Style = {};
  let plain = "";
  let i = 0;
  while (i < input.length) {
    const ch = input[i];
    if (ch === "\x1b") {
      pushSpan(spans, plain, style);
      plain = "";
      const next = input[i + 1];
      if (next === "[") {
        // CSI: ESC [ params final-byte(@-~)
        let j = i + 2;
        while (j < input.length && !(input[j] >= "@" && input[j] <= "~")) j++;
        if (j < input.length && input[j] === "m") {
          const raw = input.slice(i + 2, j);
          const params = raw
            .split(/[;:]/)
            .map((s) => (s === "" ? 0 : parseInt(s, 10)))
            .map((n) => (Number.isNaN(n) ? 0 : n));
          applySGR(style, params);
        }
        i = j + 1;
      } else if (next === "]") {
        // OSC: ESC ] ... (BEL or ESC \)
        let j = i + 2;
        while (j < input.length && input[j] !== "\x07" && !(input[j] === "\x1b" && input[j + 1] === "\\")) j++;
        i = input[j] === "\x07" ? j + 1 : j + 2;
      } else {
        i += 2;
      }
    } else if (ch === "\r") {
      i++;
    } else {
      plain += ch;
      i++;
    }
  }
  pushSpan(spans, plain, style);
  return spans;
}

// stripAnsi flattens output for measurements and plain-text uses.
export function stripAnsi(input: string): string {
  return parseAnsi(input)
    .map((s) => s.text)
    .join("");
}
