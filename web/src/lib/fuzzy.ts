// Client-side fuzzy path matching for the @-file finder. Subsequence match
// with fzf-style scoring: word/path-boundary and consecutive-run bonuses,
// gap penalties, and a preference for matches near the basename.

export interface FuzzyMatch {
  text: string;
  score: number;
  // matched character indexes, for highlight rendering
  positions: number[];
}

const BONUS_CONSECUTIVE = 8;
const BONUS_BOUNDARY = 12;
const BONUS_CAMEL = 6;
const PENALTY_GAP = -1;
const PENALTY_LEAD = -0.5;

function isBoundary(prev: string): boolean {
  return prev === "/" || prev === "_" || prev === "-" || prev === "." || prev === " ";
}

// matchFuzzy returns null when query is not a subsequence of text. Greedy
// forward scan with local lookahead — not optimal alignment, but stable and
// fast enough for 20k paths.
export function matchFuzzy(query: string, text: string): FuzzyMatch | null {
  if (query.length === 0) return { text, score: 0, positions: [] };
  const q = query.toLowerCase();
  const t = text.toLowerCase();
  if (q.length > t.length) return null;

  const positions: number[] = [];
  let score = 0;
  let ti = 0;
  let lastMatch = -2;
  for (let qi = 0; qi < q.length; qi++) {
    const idx = t.indexOf(q[qi], ti);
    if (idx === -1) return null;
    // prefer a boundary occurrence over an immediate mid-word hit
    let use = idx;
    if (idx > 0 && !isBoundary(text[idx - 1])) {
      for (let k = idx + 1; k < t.length; k++) {
        if (t[k] === q[qi] && (k === 0 || isBoundary(text[k - 1]))) {
          use = k;
          break;
        }
      }
      // only jump ahead if it doesn't break the rest of the subsequence
      if (use !== idx && t.slice(use + 1).length < q.length - qi - 1) use = idx;
      if (use !== idx) {
        let ok = true;
        let scan = use + 1;
        for (let rest = qi + 1; rest < q.length; rest++) {
          const found = t.indexOf(q[rest], scan);
          if (found === -1) {
            ok = false;
            break;
          }
          scan = found + 1;
        }
        if (!ok) use = idx;
      }
    }
    positions.push(use);
    if (use === lastMatch + 1) score += BONUS_CONSECUTIVE;
    if (use === 0 || isBoundary(text[use - 1])) score += BONUS_BOUNDARY;
    else if (text[use] !== t[use] /* uppercase in original */) score += BONUS_CAMEL;
    else score += PENALTY_GAP * Math.min(use - lastMatch - 1, 8);
    lastMatch = use;
    ti = use + 1;
  }
  // matches close to the basename beat matches deep in a long prefix
  const slash = text.lastIndexOf("/");
  if (positions[0] > slash) score += BONUS_BOUNDARY;
  score += PENALTY_LEAD * Math.min(positions[0], 20);
  score -= text.length * 0.01;
  return { text, score, positions };
}

// filterFuzzy ranks candidates and returns the top `limit` matches.
export function filterFuzzy(query: string, candidates: string[], limit = 50): FuzzyMatch[] {
  const out: FuzzyMatch[] = [];
  for (const c of candidates) {
    const m = matchFuzzy(query, c);
    if (m) out.push(m);
  }
  out.sort((a, b) => b.score - a.score || a.text.length - b.text.length);
  return out.slice(0, limit);
}
