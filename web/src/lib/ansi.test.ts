import { describe, expect, it } from "vitest";
import { parseAnsi, stripAnsi } from "./ansi";

describe("parseAnsi", () => {
  it("passes plain text through as one span", () => {
    expect(parseAnsi("hello world")).toEqual([{ text: "hello world" }]);
  });

  it("parses 16-color foreground and reset", () => {
    expect(parseAnsi("\x1b[31mred\x1b[0m plain")).toEqual([
      { text: "red", fg: "var(--ansi-1)" },
      { text: " plain" },
    ]);
  });

  it("parses bright colors and backgrounds", () => {
    expect(parseAnsi("\x1b[92;41mx\x1b[m")).toEqual([
      { text: "x", fg: "var(--ansi-10)", bg: "var(--ansi-1)" },
    ]);
  });

  it("parses bold, dim, italic, underline and their resets", () => {
    expect(parseAnsi("\x1b[1;3mA\x1b[22mB\x1b[23mC")).toEqual([
      { text: "A", bold: true, italic: true },
      { text: "B", italic: true },
      { text: "C" },
    ]);
    expect(parseAnsi("\x1b[2;4mD\x1b[24mE")).toEqual([
      { text: "D", dim: true, underline: true },
      { text: "E", dim: true },
    ]);
  });

  it("parses 256-color palette, cube, and grayscale", () => {
    expect(parseAnsi("\x1b[38;5;9mx")).toEqual([{ text: "x", fg: "var(--ansi-9)" }]);
    // 16 + 36*5 + 6*0 + 0 = 196 -> pure red in the cube
    expect(parseAnsi("\x1b[38;5;196mx")).toEqual([{ text: "x", fg: "#ff0000" }]);
    expect(parseAnsi("\x1b[48;5;232mx")).toEqual([{ text: "x", bg: "#080808" }]);
  });

  it("parses 24-bit truecolor", () => {
    expect(parseAnsi("\x1b[38;2;10;20;30mx")).toEqual([{ text: "x", fg: "#0a141e" }]);
  });

  it("default fg/bg (39/49) clear only that channel", () => {
    expect(parseAnsi("\x1b[31;41ma\x1b[39mb\x1b[49mc")).toEqual([
      { text: "a", fg: "var(--ansi-1)", bg: "var(--ansi-1)" },
      { text: "b", bg: "var(--ansi-1)" },
      { text: "c" },
    ]);
  });

  it("strips OSC sequences and non-SGR CSI sequences", () => {
    expect(parseAnsi("\x1b]0;title\x07before\x1b[2Kafter")).toEqual([{ text: "beforeafter" }]);
  });

  it("drops carriage returns", () => {
    expect(parseAnsi("line\r\ndone")).toEqual([{ text: "line\ndone" }]);
  });

  it("merges adjacent spans with identical style", () => {
    expect(parseAnsi("\x1b[31ma\x1b[31mb")).toEqual([{ text: "ab", fg: "var(--ansi-1)" }]);
  });

  it("ignores unknown SGR parameters gracefully", () => {
    expect(parseAnsi("\x1b[73mx")).toEqual([{ text: "x" }]);
  });
});

describe("stripAnsi", () => {
  it("flattens to plain text", () => {
    expect(stripAnsi("\x1b[1;32mok\x1b[0m done")).toBe("ok done");
  });
});
