// nfkc-core.ts — the frozen NFKC algorithm, parameterised by Unicode tables.
//
// This module implements Unicode Normalization Form KC (UAX #15) WITHOUT ever
// calling `String.prototype.normalize`. The host runtime's `.normalize("NFKC")`
// resolves against whatever Unicode version the runtime ships (Node 24 →
// Unicode 17.0, Bun 1.3 → 15.1), so a signer and a verifier on different
// runtimes produce different bytes. The moat signs and re-verifies across
// runtimes, so it must normalise against a FROZEN, version-pinned table.
//
// The three Unicode tables are passed in as `NfkcTables`, not imported here:
//   - `scripts/generate-nfkc-table.ts` drives this algorithm with in-memory
//     tables, so it can verify correctness BEFORE writing any artifact;
//   - `src/nfkc.ts` drives it with the frozen generated tables.
// One algorithm, two callers — they cannot drift.
//
// Pipeline (UAX #15 §1.3): full compatibility decomposition → canonical
// ordering → canonical composition. Hangul is handled algorithmically (its
// syllable layout is mathematical and stable since Unicode 2.0), so the
// 11,172 Hangul syllables are NOT in the decomposition table.

export interface NfkcTables {
  // Code point → its fully-recursive compatibility decomposition (NFKD),
  // canonically ordered. Absent ⇒ the code point does not decompose. Hangul
  // syllables are absent (decomposed algorithmically). Because the stored
  // decomposition is already fully recursive, the decomposer splices it in
  // with no recursion of its own.
  readonly decomposition: ReadonlyMap<number, readonly number[]>;
  // Packed pair key (see `composeKey`) → the primary composite code point.
  // Holds only non-Hangul canonical composition pairs; Hangul composition is
  // algorithmic.
  readonly composition: ReadonlyMap<number, number>;
  // Code point → Canonical_Combining_Class. Absent ⇒ class 0 (Not_Reordered,
  // a "starter"). Only non-zero classes are stored.
  readonly combiningClass: ReadonlyMap<number, number>;
}

// Hangul algorithmic constants — Unicode §3.12 / UAX #15. Mathematical, not
// version-pinned: the Hangul syllable block layout has been fixed since
// Unicode 2.0, so these are constants, not frozen-table data.
const HANGUL_S_BASE = 0xac00;
const HANGUL_L_BASE = 0x1100;
const HANGUL_V_BASE = 0x1161;
const HANGUL_T_BASE = 0x11a7;
const HANGUL_L_COUNT = 19;
const HANGUL_V_COUNT = 21;
const HANGUL_T_COUNT = 28;
const HANGUL_N_COUNT = HANGUL_V_COUNT * HANGUL_T_COUNT; // 588
const HANGUL_S_COUNT = HANGUL_L_COUNT * HANGUL_N_COUNT; // 11172

// Packs a (starter, second) code-point pair into one composition-table key.
// Both code points are < 0x110000, so the key is < 0x110000² ≈ 1.24e12, well
// inside Number.MAX_SAFE_INTEGER (2^53). A numeric key avoids per-lookup
// string allocation on the composition hot path.
export function composeKey(starter: number, second: number): number {
  return starter * 0x110000 + second;
}

function combiningClassOf(codePoint: number, combiningClass: ReadonlyMap<number, number>): number {
  return combiningClass.get(codePoint) ?? 0;
}

// Step D — full compatibility decomposition into a flat code-point array.
function decompose(input: string, tables: NfkcTables): number[] {
  const out: number[] = [];
  // `for...of` over a string iterates by code point (surrogate pairs are
  // yielded as one two-unit substring), so astral code points decompose
  // correctly. Lone surrogates — already rejected upstream by
  // `rejectLoneSurrogates` before the moat calls this — pass through
  // unchanged as non-decomposing, class-0 code points.
  for (const character of input) {
    const codePoint = character.codePointAt(0);
    /* v8 ignore next 3 -- unreachable: `for...of` always yields a non-empty
       character, so `codePointAt(0)` is always defined; the guard exists only
       to satisfy `noUncheckedIndexedAccess` without a forbidden `!`/`as`. */
    if (codePoint === undefined) {
      continue;
    }
    decomposeCodePoint(codePoint, tables, out);
  }
  return out;
}

function decomposeCodePoint(codePoint: number, tables: NfkcTables, out: number[]): void {
  if (codePoint >= HANGUL_S_BASE && codePoint < HANGUL_S_BASE + HANGUL_S_COUNT) {
    const syllableIndex = codePoint - HANGUL_S_BASE;
    out.push(HANGUL_L_BASE + Math.floor(syllableIndex / HANGUL_N_COUNT));
    out.push(HANGUL_V_BASE + Math.floor((syllableIndex % HANGUL_N_COUNT) / HANGUL_T_COUNT));
    const trailingIndex = syllableIndex % HANGUL_T_COUNT;
    if (trailingIndex > 0) {
      out.push(HANGUL_T_BASE + trailingIndex);
    }
    return;
  }
  const mapped = tables.decomposition.get(codePoint);
  if (mapped === undefined) {
    out.push(codePoint);
    return;
  }
  // The table stores fully-recursive NFKD, so a single splice is complete —
  // no element of `mapped` is itself decomposable (the generator asserts it).
  for (const part of mapped) {
    out.push(part);
  }
}

// Step R — canonical ordering. Stably sort each maximal run of non-starters
// (Canonical_Combining_Class > 0) by ascending combining class. Exported so the
// table generator drives the SAME ordering when it builds the frozen NFKD
// table — no second copy of this algorithm to drift.
export function canonicalOrder(
  codePoints: number[],
  combiningClass: ReadonlyMap<number, number>,
): void {
  let index = 0;
  while (index < codePoints.length) {
    const codePoint = codePoints[index];
    if (codePoint === undefined || combiningClassOf(codePoint, combiningClass) === 0) {
      index += 1;
      continue;
    }
    let runEnd = index;
    while (runEnd < codePoints.length) {
      const runCodePoint = codePoints[runEnd];
      if (runCodePoint === undefined || combiningClassOf(runCodePoint, combiningClass) === 0) {
        break;
      }
      runEnd += 1;
    }
    sortNonStarterRun(codePoints, index, runEnd, combiningClass);
    index = runEnd;
  }
}

// Stable insertion sort of `codePoints[start, end)` by combining class. Hand-
// rolled rather than `Array.prototype.sort` so stability is self-evident: an
// element only shifts past a STRICTLY greater class, so equal-class marks keep
// input order — required, since reordering equal-class marks is not a
// canonically-equivalent transformation. Runs are tiny (almost always ≤ 3).
function sortNonStarterRun(
  codePoints: number[],
  start: number,
  end: number,
  combiningClass: ReadonlyMap<number, number>,
): void {
  for (let cursor = start + 1; cursor < end; cursor += 1) {
    const codePoint = codePoints[cursor];
    /* v8 ignore next 3 -- unreachable: `cursor < end <= codePoints.length`, so
       the index is in bounds; the guard satisfies `noUncheckedIndexedAccess`. */
    if (codePoint === undefined) {
      continue;
    }
    const codePointClass = combiningClassOf(codePoint, combiningClass);
    let probe = cursor - 1;
    while (probe >= start) {
      const probeCodePoint = codePoints[probe];
      /* v8 ignore next 3 -- unreachable: `start <= probe < cursor`, so the
         index is in bounds; the guard satisfies `noUncheckedIndexedAccess`. */
      if (probeCodePoint === undefined) {
        break;
      }
      if (combiningClassOf(probeCodePoint, combiningClass) <= codePointClass) {
        break;
      }
      codePoints[probe + 1] = probeCodePoint;
      probe -= 1;
    }
    codePoints[probe + 1] = codePoint;
  }
}

// Hangul-aware primary composition of an ordered (starter, second) pair.
function composePair(starter: number, second: number, tables: NfkcTables): number | undefined {
  // Hangul L + V → LV syllable.
  if (
    starter >= HANGUL_L_BASE &&
    starter < HANGUL_L_BASE + HANGUL_L_COUNT &&
    second >= HANGUL_V_BASE &&
    second < HANGUL_V_BASE + HANGUL_V_COUNT
  ) {
    const leadingIndex = starter - HANGUL_L_BASE;
    const vowelIndex = second - HANGUL_V_BASE;
    return HANGUL_S_BASE + (leadingIndex * HANGUL_V_COUNT + vowelIndex) * HANGUL_T_COUNT;
  }
  // Hangul LV + T → LVT syllable. `(starter - S_BASE) % T_COUNT === 0` selects
  // exactly the LV syllables (trailing index 0); `second > T_BASE` excludes
  // the T_BASE sentinel that stands for "no trailing jamo".
  if (
    starter >= HANGUL_S_BASE &&
    starter < HANGUL_S_BASE + HANGUL_S_COUNT &&
    (starter - HANGUL_S_BASE) % HANGUL_T_COUNT === 0 &&
    second > HANGUL_T_BASE &&
    second < HANGUL_T_BASE + HANGUL_T_COUNT
  ) {
    return starter + (second - HANGUL_T_BASE);
  }
  return tables.composition.get(composeKey(starter, second));
}

// Step C — canonical composition. Walk the decomposed, canonically-ordered
// sequence, composing each code point into the most recent starter when the
// UAX #15 "blocked" rule permits.
function compose(codePoints: readonly number[], tables: NfkcTables): number[] {
  const out: number[] = [];
  // Index in `out` of the most recent starter, and its current (possibly
  // already-composed) value. -1 ⇒ no starter seen yet.
  let starterIndex = -1;
  let starterCodePoint = -1;
  // Combining class of the last code point KEPT in `out` since that starter.
  // -1 ⇒ nothing kept since the starter, i.e. the next code point is adjacent
  // to it. A code point C is blocked from the starter iff something is kept
  // between them whose class is >= class(C); because step R left the kept
  // non-starters in non-decreasing class order, the last kept class is their
  // maximum, so this single value is a sufficient blocked test.
  let lastKeptClass = -1;
  for (const codePoint of codePoints) {
    const codePointClass = combiningClassOf(codePoint, tables.combiningClass);
    const notBlocked = lastKeptClass === -1 || lastKeptClass < codePointClass;
    if (starterIndex >= 0 && notBlocked) {
      const composed = composePair(starterCodePoint, codePoint, tables);
      if (composed !== undefined) {
        out[starterIndex] = composed;
        starterCodePoint = composed;
        // `codePoint` was consumed into the starter — not kept, not "between".
        continue;
      }
    }
    out.push(codePoint);
    if (codePointClass === 0) {
      starterIndex = out.length - 1;
      starterCodePoint = codePoint;
      lastKeptClass = -1;
    } else {
      lastKeptClass = codePointClass;
    }
  }
  return out;
}

function codePointsToString(codePoints: readonly number[]): string {
  let out = "";
  for (const codePoint of codePoints) {
    out += String.fromCodePoint(codePoint);
  }
  return out;
}

// Normalise `input` to NFKC against the supplied frozen tables. Pure: no
// `String.prototype.normalize`, no host Unicode data, no I/O.
export function normalizeNfkc(input: string, tables: NfkcTables): string {
  if (input.length === 0) {
    return "";
  }
  const decomposed = decompose(input, tables);
  canonicalOrder(decomposed, tables.combiningClass);
  return codePointsToString(compose(decomposed, tables));
}
