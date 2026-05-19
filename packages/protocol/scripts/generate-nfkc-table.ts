// generate-nfkc-table.ts — regenerate the frozen NFKC normalisation tables.
//
// `SanitizedString` normalises text to NFKC before and after the moat strip.
// `String.prototype.normalize("NFKC")` resolves against the Unicode data baked
// into the running runtime, and that data is NOT reliably the version it
// claims: Bun 1.3.13 reports `process.versions.unicode === "15.1"` on every
// platform, yet on a recent macOS its `.normalize()` uses the OS ICU (Unicode
// 16.0+). For a security boundary whose output is signed and re-verified on a
// different runtime, the tables cannot come from `.normalize()` at all.
//
//   bun run scripts/generate-nfkc-table.ts
//
// So this generator derives the tables ENTIRELY from vendored, authoritative
// Unicode Character Database text files (scripts/ucd/), never from the runtime
// — it is fully deterministic on any host. It writes two artifacts from a
// single source of truth:
//   - src/nfkc-table.generated.ts  embedded tables the normaliser uses
//   - testdata/nfkc-table.json     cross-language wire artifact + vectors,
//                                  verified by verifier-reference.go
//
// When run on a host whose runtime genuinely ships the pinned Unicode version,
// it ALSO cross-checks `frozenNfkc` against `String.prototype.normalize` for
// every code point as a bonus proof; on a newer runtime that cross-check is
// skipped (the vendored UCD remains the source of truth).
//
// To bump the pinned Unicode version, replace the vendored UCD files and the
// version constants below; nothing else depends on the host.

import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { composeKey, type NfkcTables, normalizeNfkc } from "../src/nfkc-core.ts";

const MAX_CODE_POINT = 0x10ffff;
const SURROGATE_START = 0xd800;
const SURROGATE_END = 0xdfff;
const HANGUL_S_BASE = 0xac00;
const HANGUL_S_COUNT = 11172;

// The pinned Unicode version. The vendored UCD files below are for this
// version; both artifacts are stamped with it.
const PINNED_UNICODE_VERSION = "15.1";
const UNICODE_DATA_FILE = "ucd/UnicodeData-15.1.0.txt";
const COMPOSITION_EXCLUSIONS_FILE = "ucd/CompositionExclusions-15.1.0.txt";

// A code point assigned in Unicode 16.0 but unassigned in 15.1 (Symbols for
// Legacy Computing Supplement). Used only to detect whether the host runtime
// ships data newer than the pin, so the optional runtime cross-check knows
// whether a mismatch is expected drift or a genuine bug.
const POST_PIN_SENTINEL = 0x1ccd6;

type DecompositionEntry = readonly [number, readonly number[]];
type CompositionEntry = readonly [number, number, number];
type CombiningClassEntry = readonly [number, number];

interface DecompositionMapping {
  readonly compat: boolean;
  readonly to: readonly number[];
}

function isSurrogate(codePoint: number): boolean {
  return codePoint >= SURROGATE_START && codePoint <= SURROGATE_END;
}

function isHangulSyllable(codePoint: number): boolean {
  return codePoint >= HANGUL_S_BASE && codePoint < HANGUL_S_BASE + HANGUL_S_COUNT;
}

interface UnicodeDataTables {
  // Code point → Canonical_Combining_Class, non-zero entries only.
  readonly combiningClass: Map<number, number>;
  // Code point → its SINGLE-LEVEL decomposition mapping (UnicodeData.txt
  // field 5). `compat` is true when the mapping carries a `<tag>`.
  readonly decompositionMappings: Map<number, DecompositionMapping>;
}

// Parse the vendored official UnicodeData.txt. Each line is 15 semicolon-
// separated fields; field 0 is the code point (hex), field 3 the canonical
// combining class (decimal), field 5 the decomposition mapping — empty, or
// `0041 0300` (canonical), or `<compat> 0020 0308` (a tagged compatibility
// mapping). Range markers (`<…, First>` / `<…, Last>`) carry neither a
// non-zero class nor a mapping, so processing line-by-line is sufficient.
function parseUnicodeData(fileText: string): UnicodeDataTables {
  const combiningClass = new Map<number, number>();
  const decompositionMappings = new Map<number, DecompositionMapping>();
  for (const rawLine of fileText.split("\n")) {
    const line = rawLine.trim();
    if (line.length === 0) {
      continue;
    }
    const fields = line.split(";");
    if (fields.length < 6) {
      throw new Error(`UnicodeData: malformed line ${JSON.stringify(rawLine)}`);
    }
    const codePoint = Number.parseInt(fields[0] ?? "", 16);
    if (!Number.isInteger(codePoint)) {
      throw new Error(`UnicodeData: bad code point ${JSON.stringify(fields[0])}`);
    }

    const combiningClassValue = Number.parseInt(fields[3] ?? "", 10);
    if (!Number.isInteger(combiningClassValue) || combiningClassValue < 0) {
      throw new Error(`UnicodeData: bad combining class for U+${codePoint.toString(16)}`);
    }
    if (combiningClassValue !== 0) {
      combiningClass.set(codePoint, combiningClassValue);
    }

    const decompositionField = (fields[5] ?? "").trim();
    if (decompositionField.length > 0) {
      const tokens = decompositionField.split(/\s+/);
      const compat = tokens[0]?.startsWith("<") ?? false;
      const mapping = (compat ? tokens.slice(1) : tokens).map((token) => {
        const value = Number.parseInt(token, 16);
        if (!Number.isInteger(value)) {
          throw new Error(`UnicodeData: bad decomposition token ${JSON.stringify(token)}`);
        }
        return value;
      });
      decompositionMappings.set(codePoint, { compat, to: mapping });
    }
  }
  return { combiningClass, decompositionMappings };
}

// Parse the vendored official CompositionExclusions.txt — one code point (hex)
// per non-comment line. These are the script-specific and post-composition-
// version exclusions; singletons and non-starter decomposables are excluded
// structurally by `buildComposition` and are not listed here.
function parseCompositionExclusions(fileText: string): Set<number> {
  const exclusions = new Set<number>();
  for (const rawLine of fileText.split("\n")) {
    const line = rawLine.split("#", 1)[0]?.trim() ?? "";
    if (line.length === 0) {
      continue;
    }
    const codePoint = Number.parseInt(line, 16);
    if (!Number.isInteger(codePoint)) {
      throw new Error(`CompositionExclusions: bad code point ${JSON.stringify(rawLine)}`);
    }
    exclusions.add(codePoint);
  }
  return exclusions;
}

function combiningClassOf(codePoint: number, combiningClass: ReadonlyMap<number, number>): number {
  return combiningClass.get(codePoint) ?? 0;
}

// Recursively expand a code point's full compatibility decomposition (NFKD,
// pre-ordering): replace each code point with its single-level mapping until
// no code point decomposes. The depth guard catches a malformed (cyclic) UCD.
function fullyDecompose(
  codePoint: number,
  decompositionMappings: ReadonlyMap<number, DecompositionMapping>,
  out: number[],
  depth: number,
): void {
  if (depth > 32) {
    throw new Error(`decomposition of U+${codePoint.toString(16)} exceeds depth 32 — cyclic UCD?`);
  }
  const mapping = decompositionMappings.get(codePoint);
  if (mapping === undefined) {
    out.push(codePoint);
    return;
  }
  for (const part of mapping.to) {
    fullyDecompose(part, decompositionMappings, out, depth + 1);
  }
}

// Stable canonical ordering: sort each maximal run of non-starters by combining
// class, preserving input order within an equal class. Same algorithm as
// `canonicalOrder` in nfkc-core.ts (inlined so the generator does not depend on
// a non-exported helper).
function canonicalOrderInPlace(
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
    for (let cursor = index + 1; cursor < runEnd; cursor += 1) {
      const cursorCodePoint = codePoints[cursor];
      if (cursorCodePoint === undefined) {
        continue;
      }
      const cursorClass = combiningClassOf(cursorCodePoint, combiningClass);
      let probe = cursor - 1;
      while (probe >= index) {
        const probeCodePoint = codePoints[probe];
        if (probeCodePoint === undefined) {
          break;
        }
        if (combiningClassOf(probeCodePoint, combiningClass) <= cursorClass) {
          break;
        }
        codePoints[probe + 1] = probeCodePoint;
        probe -= 1;
      }
      codePoints[probe + 1] = cursorCodePoint;
    }
    index = runEnd;
  }
}

// Build the fully-recursive NFKD decomposition table. Hangul syllables are
// excluded (the normaliser decomposes them algorithmically).
function buildDecomposition(
  decompositionMappings: ReadonlyMap<number, DecompositionMapping>,
  combiningClass: ReadonlyMap<number, number>,
): DecompositionEntry[] {
  const entries: DecompositionEntry[] = [];
  for (const codePoint of [...decompositionMappings.keys()].sort((a, b) => a - b)) {
    if (isSurrogate(codePoint) || isHangulSyllable(codePoint)) {
      continue;
    }
    const decomposed: number[] = [];
    fullyDecompose(codePoint, decompositionMappings, decomposed, 0);
    canonicalOrderInPlace(decomposed, combiningClass);
    entries.push([codePoint, decomposed]);
  }
  return entries;
}

// Build the canonical composition table. A primary composite is a code point
// whose CANONICAL (untagged) decomposition is exactly two code points, whose
// first element is a starter, and which is not a Composition Exclusion.
// Singletons (length 1) and non-starter decomposables (first element a
// non-starter) are excluded by the length and starter checks. Hangul is
// composed algorithmically and excluded.
function buildComposition(
  decompositionMappings: ReadonlyMap<number, DecompositionMapping>,
  combiningClass: ReadonlyMap<number, number>,
  exclusions: ReadonlySet<number>,
): CompositionEntry[] {
  const entries: CompositionEntry[] = [];
  for (const codePoint of [...decompositionMappings.keys()].sort((a, b) => a - b)) {
    if (isSurrogate(codePoint) || isHangulSyllable(codePoint) || exclusions.has(codePoint)) {
      continue;
    }
    const mapping = decompositionMappings.get(codePoint);
    if (mapping === undefined || mapping.compat || mapping.to.length !== 2) {
      continue;
    }
    const [starter, second] = mapping.to;
    if (starter === undefined || second === undefined) {
      continue;
    }
    if (combiningClassOf(starter, combiningClass) !== 0) {
      continue; // non-starter decomposable — never a primary composite
    }
    entries.push([starter, second, codePoint]);
  }
  return entries;
}

function buildTables(
  decomposition: readonly DecompositionEntry[],
  composition: readonly CompositionEntry[],
  combiningClass: readonly CombiningClassEntry[],
): NfkcTables {
  return {
    decomposition: new Map(decomposition),
    composition: new Map(
      composition.map(([starter, second, composed]) => [composeKey(starter, second), composed]),
    ),
    combiningClass: new Map(combiningClass),
  };
}

// Assert the tables are internally well-formed before they are written.
function assertWellFormed(
  decomposition: readonly DecompositionEntry[],
  composition: readonly CompositionEntry[],
  tables: NfkcTables,
): void {
  for (const [codePoint, parts] of decomposition) {
    if (parts.length === 0) {
      throw new Error(`decomposition of U+${codePoint.toString(16)} is empty`);
    }
    for (const part of parts) {
      if (isSurrogate(part)) {
        throw new Error(`decomposition of U+${codePoint.toString(16)} contains a surrogate`);
      }
      if (isHangulSyllable(part)) {
        throw new Error(`decomposition of U+${codePoint.toString(16)} contains a Hangul syllable`);
      }
      if (tables.decomposition.has(part)) {
        // Would break the single-splice (non-recursive) decomposer.
        throw new Error(
          `decomposition of U+${codePoint.toString(16)} contains the further-decomposable ` +
            `U+${part.toString(16)} — the table is not fully recursive`,
        );
      }
    }
  }
  for (const [starter, second, composed] of composition) {
    if ((tables.combiningClass.get(starter) ?? 0) !== 0) {
      throw new Error(
        `composition pair (U+${starter.toString(16)}, U+${second.toString(16)}) → ` +
          `U+${composed.toString(16)} has a non-starter first element`,
      );
    }
  }
}

// Deterministic PRNG (mulberry32) so the adversarial corpus is reproducible.
function mulberry32(seed: number): () => number {
  let state = seed >>> 0;
  return () => {
    state = (state + 0x6d2b79f5) | 0;
    let mixed = Math.imul(state ^ (state >>> 15), 1 | state);
    mixed = (mixed + Math.imul(mixed ^ (mixed >>> 7), 61 | mixed)) ^ mixed;
    return ((mixed ^ (mixed >>> 14)) >>> 0) / 4294967296;
  };
}

interface NamedVector {
  readonly input: string;
  readonly name: string;
}

// Curated adversarial corpus. Each input names the normalisation trap it
// targets; `expected` is computed from the FROZEN tables at generation time.
// These become the cross-language wire vectors run by both Vitest and the Go
// reference, so every language port is pinned to the same corpus.
function curatedVectors(): NamedVector[] {
  return [
    { input: "", name: "empty string" },
    { input: "abc", name: "pure ASCII, no decomposition" },
    { input: "Å", name: "A + combining ring → composes to U+00C5" },
    { input: "Å", name: "precomposed U+00C5 stays U+00C5" },
    { input: "Å", name: "U+212B ANGSTROM SIGN — singleton → A + ring → U+00C5" },
    { input: "Ω", name: "U+2126 OHM SIGN — singleton → U+03A9" },
    { input: "K", name: "U+212A KELVIN SIGN — singleton → U+004B" },
    { input: "ẛ̣", name: "U+1E9B + dot-below — recursive decomposition + reorder" },
    { input: "q̣̇", name: "q + dot-above(230) + dot-below(220) — canonical reorder" },
    { input: "q̣̇", name: "q + dot-below(220) + dot-above(230) — already ordered" },
    { input: "ȩ́", name: "e + acute(230) + cedilla(202) — reorder then compose" },
    { input: "ȩ́", name: "e + cedilla(202) + acute(230) — compose cedilla, keep acute" },
    { input: "á́", name: "two equal-class marks — stable, only the first composes" },
    { input: "̈́", name: "U+0344 — non-starter decomposable, must NOT recompose" },
    { input: "Ạ̀", name: "precomposed U+00C0 + dot-below — decompose, reorder, recompose" },
    { input: "각", name: "Hangul jamo L+V+T → LVT syllable" },
    { input: "가", name: "Hangul jamo L+V → LV syllable" },
    { input: "가", name: "Hangul syllable U+AC00 stays composed" },
    { input: "힣", name: "Hangul syllable U+D7A3 (last) stays composed" },
    { input: "각ᄀ", name: "Hangul LVT + stray L jamo — no spurious composition" },
    { input: "가ᅡ", name: "Hangul L+V+V — second V does not attach" },
    { input: "ﬀ", name: "U+FB00 LATIN SMALL LIGATURE FF → ff" },
    { input: "ﷺ", name: "U+FDFA ARABIC LIGATURE — 18-code-point decomposition" },
    { input: "㌀", name: "U+3300 SQUARE APAATO — compatibility decomposition" },
    { input: "ＡＢ", name: "fullwidth AB → ASCII AB" },
    { input: "①", name: "U+2460 CIRCLED DIGIT ONE → '1'" },
    { input: "½", name: "U+00BD VULGAR FRACTION ONE HALF → '1⁄2'" },
    { input: "Ǆ", name: "U+01C4 LATIN CAPITAL LETTER DZ WITH CARON → 'DŽ'" },
    { input: "Ĳ", name: "U+0132 LATIN CAPITAL LIGATURE IJ → 'IJ'" },
    { input: "⁵", name: "U+2075 SUPERSCRIPT FIVE → '5'" },
    { input: "\u{1d400}", name: "U+1D400 MATHEMATICAL BOLD CAPITAL A → 'A' (astral)" },
    { input: "\u{1e900}", name: "U+1E900 ADLAM CAPITAL ALPHA — astral passthrough" },
    { input: " ", name: "U+00A0 NO-BREAK SPACE → SPACE" },
    { input: "Á̖́", name: "A + three marks across classes 230/220/230" },
    { input: "x͏́", name: "x + CGJ(0) + acute — CGJ is a starter, blocks composition" },
    { input: "ཱི", name: "U+0F73 TIBETAN — decomposes to two non-starters" },
    { input: "ְ֔", name: "Hebrew marks — reorder by combining class" },
    { input: "aְ֔b", name: "marks between two base letters" },
    { input: "ế", name: "U+1EBF — recursive: e + circumflex + acute precomposed" },
    { input: "क़", name: "U+0958 DEVANAGARI QA — composition-excluded, stays decomposed" },
  ];
}

// The runtime cross-check corpus: curated inputs plus a programmatic spread of
// mark orderings, triples, and Hangul jamo runs.
function verificationCorpus(combiningClass: ReadonlyMap<number, number>): string[] {
  const corpus = curatedVectors().map((vector) => vector.input);

  // A spread of non-starter marks across distinct combining classes. The list
  // is FIXED; every entry must be a real non-starter in the pinned data, or the
  // run fails loudly — a silent prune would weaken the corpus.
  const sampleMarks = [
    0x0300, 0x0301, 0x0302, 0x0307, 0x0308, 0x0316, 0x0323, 0x0327, 0x0328, 0x031b, 0x0334, 0x0345,
    0x05b0, 0x05c1, 0x05c2, 0x064b, 0x0653, 0x093c, 0x0f71, 0x0f72, 0x1dc0,
  ];
  for (const mark of sampleMarks) {
    if (combiningClassOf(mark, combiningClass) === 0) {
      throw new Error(
        `corpus sample mark U+${mark.toString(16)} is a starter in the pinned data — ` +
          `the fixed sample-mark list is stale`,
      );
    }
  }

  for (const first of sampleMarks) {
    for (const second of sampleMarks) {
      corpus.push(`a${String.fromCodePoint(first, second)}`);
      for (const third of sampleMarks) {
        corpus.push(`e${String.fromCodePoint(first, second, third)}`);
      }
    }
  }

  const random = mulberry32(0x6e666b63);
  const alphabet = [0x61, 0x65, 0x6e, 0x6f, 0xac00, 0x1100, 0x1161, 0x11a8, ...sampleMarks];
  for (let sample = 0; sample < 4000; sample += 1) {
    const length = 1 + Math.floor(random() * 8);
    let built = "";
    for (let position = 0; position < length; position += 1) {
      const pick = alphabet[Math.floor(random() * alphabet.length)] ?? 0x61;
      built += String.fromCodePoint(pick);
    }
    corpus.push(built);
  }
  return corpus;
}

interface VerificationResult {
  readonly singleCodePointChecks: number;
  readonly corpusChecks: number;
  readonly runtimeNewerThanPin: boolean;
  readonly runtimeMismatches: number;
}

// Cross-check `normalizeNfkc` (driven by the just-built tables) against the
// host runtime's `String.prototype.normalize("NFKC")`. The tables are the
// authoritative output — derived from the vendored UCD — so this is a bonus
// proof, not the source of truth: on a runtime whose Unicode data is newer
// than the pin, mismatches on code points the pin leaves unassigned are
// EXPECTED and reported, not fatal; on a runtime that matches the pin, ANY
// mismatch is a genuine bug and aborts. Idempotence is runtime-independent and
// is always asserted hard.
function verifyAgainstRuntime(
  tables: NfkcTables,
  combiningClass: ReadonlyMap<number, number>,
): VerificationResult {
  const sentinel = String.fromCodePoint(POST_PIN_SENTINEL);
  const runtimeNewerThanPin = sentinel.normalize("NFKD") !== sentinel;

  let runtimeMismatches = 0;
  const crossCheck = (input: string, label: string): void => {
    const frozen = normalizeNfkc(input, tables);
    if (normalizeNfkc(frozen, tables) !== frozen) {
      throw new Error(`frozen NFKC is not idempotent for ${label}`);
    }
    if (frozen !== input.normalize("NFKC")) {
      runtimeMismatches += 1;
      if (!runtimeNewerThanPin) {
        throw new Error(
          `frozen NFKC disagrees with the runtime for ${label}: ` +
            `frozen=${JSON.stringify(frozen)} runtime=${JSON.stringify(input.normalize("NFKC"))}`,
        );
      }
    }
  };

  let singleCodePointChecks = 0;
  for (let codePoint = 0; codePoint <= MAX_CODE_POINT; codePoint += 1) {
    if (isSurrogate(codePoint)) {
      continue;
    }
    crossCheck(String.fromCodePoint(codePoint), `U+${codePoint.toString(16)}`);
    singleCodePointChecks += 1;
  }

  let corpusChecks = 0;
  for (const input of verificationCorpus(combiningClass)) {
    crossCheck(input, JSON.stringify(input));
    corpusChecks += 1;
  }
  return { singleCodePointChecks, corpusChecks, runtimeNewerThanPin, runtimeMismatches };
}

function hex(value: number): string {
  return `0x${value.toString(16)}`;
}

function renderTableModule(
  decomposition: readonly DecompositionEntry[],
  composition: readonly CompositionEntry[],
  combiningClass: readonly CombiningClassEntry[],
): string {
  const decompositionLines = decomposition
    .map(([codePoint, parts]) => `  [${hex(codePoint)}, [${parts.map(hex).join(", ")}]],`)
    .join("\n");
  const compositionLines = composition
    .map(([starter, second, composed]) => `  [${hex(starter)}, ${hex(second)}, ${hex(composed)}],`)
    .join("\n");
  const combiningClassLines = combiningClass
    .map(([codePoint, value]) => `  [${hex(codePoint)}, ${value}],`)
    .join("\n");
  return `// GENERATED FILE — do not edit by hand.
// Source of truth: scripts/generate-nfkc-table.ts + the vendored UCD files in
// scripts/ucd/. Regenerate: bun run scripts/generate-nfkc-table.ts
//
// Frozen Unicode NFKC tables, derived from the vendored Unicode Character
// Database at the version below. The moat normalises against THESE tables, not
// the host runtime's live Unicode data, so a signer and a verifier on
// different Node/Bun/ICU versions agree on the sanitized bytes. The
// cross-language wire artifact and Go reference live in
// testdata/nfkc-table.json. Hangul is decomposed/composed algorithmically (see
// nfkc-core.ts), so its 11,172 syllables are intentionally absent below.

export const NFKC_UNICODE_VERSION = ${JSON.stringify(PINNED_UNICODE_VERSION)};

// Code point → fully-recursive NFKD decomposition (canonically ordered).
export const NFKC_DECOMPOSITION_ENTRIES: readonly (readonly [number, readonly number[]])[] = [
${decompositionLines}
];

// [starter, second, composite] canonical composition pairs (non-Hangul).
export const NFKC_COMPOSITION_ENTRIES: readonly (readonly [number, number, number])[] = [
${compositionLines}
];

// [code point, Canonical_Combining_Class] for every non-starter (class > 0).
export const NFKC_COMBINING_CLASS_ENTRIES: readonly (readonly [number, number])[] = [
${combiningClassLines}
];
`;
}

// Hand-rolled compact JSON so each entry sits on one line — JSON.stringify with
// indentation explodes every pair across many lines, making the diff of a
// multi-thousand-entry table unreviewable.
function renderJsonArtifact(
  decomposition: readonly DecompositionEntry[],
  composition: readonly CompositionEntry[],
  combiningClass: readonly CombiningClassEntry[],
  tables: NfkcTables,
): string {
  const description =
    "Frozen Unicode NFKC tables for the SanitizedString moat, derived from the " +
    "vendored Unicode Character Database. decomposition is the fully-recursive " +
    "NFKD mapping (Hangul excluded — decomposed algorithmically); composition " +
    "is the [starter, second, composite] canonical pairs; combiningClass is the " +
    "non-zero Canonical_Combining_Class values. normalizationVectors is a " +
    "curated corpus every language port must agree on. Run by " +
    "verifier-reference.go and tests/nfkc.spec.ts.";
  const decompositionLines = decomposition
    .map(([codePoint, parts]) => `    {"cp": ${codePoint}, "to": [${parts.join(", ")}]}`)
    .join(",\n");
  const compositionLines = composition
    .map(([starter, second, composed]) => `    [${starter}, ${second}, ${composed}]`)
    .join(",\n");
  const combiningClassLines = combiningClass
    .map(([codePoint, value]) => `    [${codePoint}, ${value}]`)
    .join(",\n");
  const vectorLines = curatedVectors()
    .map((vector) => {
      // `expected` is the FROZEN normalisation, not the runtime's — the
      // artifact must not depend on the host that generated it.
      const expected = normalizeNfkc(vector.input, tables);
      return (
        `    {"input": ${JSON.stringify(vector.input)}, ` +
        `"expected": ${JSON.stringify(expected)}, ` +
        `"name": ${JSON.stringify(vector.name)}}`
      );
    })
    .join(",\n");
  return `{
  "description": ${JSON.stringify(description)},
  "unicodeVersion": ${JSON.stringify(PINNED_UNICODE_VERSION)},
  "generatedBy": "packages/protocol/scripts/generate-nfkc-table.ts",
  "decomposition": [
${decompositionLines}
  ],
  "composition": [
${compositionLines}
  ],
  "combiningClass": [
${combiningClassLines}
  ],
  "normalizationVectors": [
${vectorLines}
  ]
}
`;
}

function main(): void {
  const scriptDir = dirname(fileURLToPath(import.meta.url));
  const packageRoot = join(scriptDir, "..");

  const unicodeData = parseUnicodeData(readFileSync(join(scriptDir, UNICODE_DATA_FILE), "utf8"));
  const exclusions = parseCompositionExclusions(
    readFileSync(join(scriptDir, COMPOSITION_EXCLUSIONS_FILE), "utf8"),
  );

  const combiningClass: CombiningClassEntry[] = [...unicodeData.combiningClass.entries()].sort(
    (left, right) => left[0] - right[0],
  );
  const decomposition = buildDecomposition(
    unicodeData.decompositionMappings,
    unicodeData.combiningClass,
  );
  const composition = buildComposition(
    unicodeData.decompositionMappings,
    unicodeData.combiningClass,
    exclusions,
  );

  const tables = buildTables(decomposition, composition, combiningClass);
  assertWellFormed(decomposition, composition, tables);
  const result = verifyAgainstRuntime(tables, unicodeData.combiningClass);

  const tableModulePath = join(packageRoot, "src", "nfkc-table.generated.ts");
  writeFileSync(
    tableModulePath,
    renderTableModule(decomposition, composition, combiningClass),
    "utf8",
  );
  // A handful of long decomposition rows (Arabic presentation ligatures) exceed
  // Biome's line width; let Biome reflow the generated module so a fresh
  // `bun run` of this script produces a file that already passes `biome check`.
  // Invoke the repo-pinned Biome from node_modules directly — not `bunx`, which
  // could resolve a different version and silently desync the committed file's
  // formatting from what CI's `biome check` expects.
  const biomeBin = join(packageRoot, "node_modules", ".bin", "biome");
  execFileSync(biomeBin, ["format", "--write", tableModulePath], { cwd: packageRoot });

  const jsonPath = join(packageRoot, "testdata", "nfkc-table.json");
  writeFileSync(
    jsonPath,
    renderJsonArtifact(decomposition, composition, combiningClass, tables),
    "utf8",
  );

  const crossCheck = result.runtimeNewerThanPin
    ? `  runtime ships Unicode newer than ${PINNED_UNICODE_VERSION}; ` +
      `runtime cross-check skipped (${result.runtimeMismatches} expected drift mismatches over ` +
      `${result.singleCodePointChecks} code points + ${result.corpusChecks} corpus strings)\n`
    : `  verified: frozen NFKC matches the runtime for ${result.singleCodePointChecks} code ` +
      `points + ${result.corpusChecks} corpus strings + idempotent\n`;
  process.stdout.write(
    `nfkc tables: Unicode ${PINNED_UNICODE_VERSION}, ${decomposition.length} decompositions, ` +
      `${composition.length} composition pairs, ${combiningClass.length} combining classes\n` +
      crossCheck +
      `  wrote ${tableModulePath}\n  wrote ${jsonPath}\n`,
  );
}

main();
