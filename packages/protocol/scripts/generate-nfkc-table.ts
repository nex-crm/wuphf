// generate-nfkc-table.ts — regenerate the frozen NFKC normalisation tables.
//
// `SanitizedString` normalises text to NFKC before and after the moat strip.
// `String.prototype.normalize("NFKC")` resolves against the Unicode data baked
// into the running runtime (Node 24 → Unicode 17.0, Bun 1.3 → 15.1). For a
// security boundary whose output is signed and re-verified on a *different*
// runtime, that is a moving target: a signer and a verifier on different
// Unicode versions produce different bytes (e.g. `U+A7F1` → "S" under 17.0,
// unchanged under 15.1). The moat must normalise against FROZEN tables.
//
//   bun run scripts/generate-nfkc-table.ts
//
// This script is the one place the runtime's Unicode normaliser is consulted.
// It derives the decomposition and composition tables from the pinned runtime
// and the canonical combining classes from the vendored official UCD file,
// then PROVES correctness — `normalizeNfkc` must equal `String.prototype.
// normalize("NFKC")` for every code point plus an adversarial multi-code-point
// corpus — before writing two artifacts from a single source of truth:
//   - src/nfkc-table.generated.ts  embedded tables the normaliser uses
//   - testdata/nfkc-table.json     cross-language wire artifact + vectors,
//                                  verified by verifier-reference.go
//
// Run it deliberately to bump the pinned Unicode version; the runtime it runs
// on MUST match the version of the vendored DerivedCombiningClass file.

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

// The pinned Unicode version. The vendored UCD file below is for this version;
// the runtime running this generator must match it, or the runtime-derived
// decomposition/composition would drift from the vendored combining classes.
const PINNED_UNICODE_VERSION = "15.1";
const DERIVED_COMBINING_CLASS_FILE = "ucd/DerivedCombiningClass-15.1.0.txt";

type DecompositionEntry = readonly [number, readonly number[]];
type CompositionEntry = readonly [number, number, number];
type CombiningClassEntry = readonly [number, number];

function isSurrogate(codePoint: number): boolean {
  return codePoint >= SURROGATE_START && codePoint <= SURROGATE_END;
}

function isHangulSyllable(codePoint: number): boolean {
  return codePoint >= HANGUL_S_BASE && codePoint < HANGUL_S_BASE + HANGUL_S_COUNT;
}

// Code points of a string, in order. `for...of` iterates by code point, so
// astral characters are handled; `codePointAt(0)` is always defined for the
// non-empty substrings `for...of` yields.
function codePointsOf(value: string): number[] {
  const out: number[] = [];
  for (const character of value) {
    const codePoint = character.codePointAt(0);
    if (codePoint !== undefined) {
      out.push(codePoint);
    }
  }
  return out;
}

// Parse the vendored official UCD DerivedCombiningClass file. Lines look like
// `0300..0314    ; 230 # Mn ...` or `0334  ; 1 # ...`; `#` begins a comment,
// blank lines are skipped. Only non-zero classes are kept (absent ⇒ class 0).
function parseCombiningClasses(fileText: string): CombiningClassEntry[] {
  const entries: CombiningClassEntry[] = [];
  for (const rawLine of fileText.split("\n")) {
    const line = rawLine.split("#", 1)[0]?.trim() ?? "";
    if (line.length === 0) {
      continue;
    }
    const [rangeField, classField] = line.split(";").map((field) => field.trim());
    if (rangeField === undefined || classField === undefined) {
      throw new Error(`DerivedCombiningClass: malformed line ${JSON.stringify(rawLine)}`);
    }
    const combiningClass = Number.parseInt(classField, 10);
    if (!Number.isInteger(combiningClass) || combiningClass < 0 || combiningClass > 254) {
      throw new Error(`DerivedCombiningClass: bad class ${JSON.stringify(classField)}`);
    }
    if (combiningClass === 0) {
      continue;
    }
    const bounds = rangeField.split("..");
    const start = Number.parseInt(bounds[0] ?? "", 16);
    const end = Number.parseInt(bounds[1] ?? bounds[0] ?? "", 16);
    if (!Number.isInteger(start) || !Number.isInteger(end) || start > end) {
      throw new Error(`DerivedCombiningClass: bad range ${JSON.stringify(rangeField)}`);
    }
    for (let codePoint = start; codePoint <= end; codePoint += 1) {
      entries.push([codePoint, combiningClass]);
    }
  }
  entries.sort((left, right) => left[0] - right[0]);
  return entries;
}

// Derive the full-recursive NFKD decomposition table from the runtime. Hangul
// syllables are excluded — the normaliser decomposes them algorithmically.
function buildDecomposition(): DecompositionEntry[] {
  const entries: DecompositionEntry[] = [];
  for (let codePoint = 0; codePoint <= MAX_CODE_POINT; codePoint += 1) {
    if (isSurrogate(codePoint) || isHangulSyllable(codePoint)) {
      continue;
    }
    const source = String.fromCodePoint(codePoint);
    const decomposed = source.normalize("NFKD");
    if (decomposed !== source) {
      entries.push([codePoint, codePointsOf(decomposed)]);
    }
  }
  return entries;
}

// Derive the canonical composition table from the runtime. The composition
// step needs SINGLE-LEVEL pairs — `(prefix, mark) → composite` — but
// `String.prototype.normalize("NFD")` only exposes the FULLY-RECURSIVE
// decomposition (`U+01EC` → `O + ogonek + macron`, never the single level
// `Ǫ + macron`).
//
// The outer mark of a primary composite's single-level decomposition is always
// the LAST code point of its recursive, canonically-ordered NFD: NFC composes
// marks in ascending combining-class (canonical) order, so the last-applied —
// hence outer — mark is the last in that order. The prefix is the rest,
// recomposed. A round-trip test (`NFC([prefix, mark]) === composite`) cannot be
// used to *pick* the mark: `NFC` re-decomposes and re-orders its input, so it
// also accepts non-canonical pairs (`NFC(["Ō", ogonek])` reorders to `O ogonek
// macron` and yields `U+01EC` too) — that false positive is exactly the bug
// this derivation avoids.
//
// `NFC(cp) === cp` filters to genuine primary composites: a Composition
// Exclusion or a singleton decomposes under NFC instead of staying put, so the
// exclusion list is never consulted by hand. Iterating every composite records
// every intermediate pair. Hangul is composed algorithmically and excluded.
function buildComposition(): CompositionEntry[] {
  const entries: CompositionEntry[] = [];
  for (let codePoint = 0; codePoint <= MAX_CODE_POINT; codePoint += 1) {
    if (isSurrogate(codePoint) || isHangulSyllable(codePoint)) {
      continue;
    }
    const source = String.fromCodePoint(codePoint);
    if (source.normalize("NFC") !== source) {
      continue; // a Composition Exclusion or singleton — never a primary composite
    }
    const decomposed = source.normalize("NFD");
    if (decomposed === source) {
      continue; // no canonical decomposition — not a composite
    }
    const full = codePointsOf(decomposed);
    const mark = full[full.length - 1];
    if (full.length < 2 || mark === undefined) {
      continue;
    }
    const prefixPoints = codePointsOf(
      String.fromCodePoint(...full.slice(0, full.length - 1)).normalize("NFC"),
    );
    const starter = prefixPoints[0];
    if (prefixPoints.length !== 1 || starter === undefined) {
      continue;
    }
    entries.push([starter, mark, codePoint]);
  }
  entries.sort((left, right) => left[0] - right[0] || left[1] - right[1]);
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

// Assert the tables are internally well-formed. A malformed table would make
// `normalizeNfkc` silently wrong, so this runs before the equivalence proof.
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
// targets; `expected` is computed from the runtime at generation time. These
// become the cross-language wire vectors run by both Vitest and the Go
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
    { input: "½", name: "U+00BD VULGAR FRACTION ONE HALF → '1/2'" },
    { input: "Ǆ", name: "U+01C4 LATIN CAPITAL LETTER DZ WITH CARON → 'DŽ'" },
    { input: "Ĳ", name: "U+0132 LATIN CAPITAL LIGATURE IJ → 'IJ'" },
    { input: "⁵", name: "U+2075 SUPERSCRIPT FIVE → '5'" },
    { input: "𝐀", name: "U+1D400 MATHEMATICAL BOLD CAPITAL A → 'A' (astral)" },
    { input: "𞤀", name: "U+1D900-area astral, ADLAM-adjacent — passthrough" },
    { input: " ", name: "U+00A0 NO-BREAK SPACE → SPACE" },
    { input: "Á̖́", name: "A + three marks across classes 230/220/230" },
    { input: "x͏́", name: "x + CGJ(0) + acute — CGJ is a starter, blocks composition" },
    { input: "ཱི", name: "U+0F73 TIBETAN — decomposes to two non-starters" },
    { input: "ְׄ", name: "Hebrew marks — reorder by combining class" },
    { input: "aְׄb", name: "marks between two base letters" },
    { input: "ế", name: "U+1EBF — recursive: e + circumflex + acute precomposed" },
    { input: "क़", name: "U+0958 DEVANAGARI QA — composition-excluded, stays decomposed form" },
  ];
}

// The full self-verification corpus: curated vectors plus a programmatic
// adversarial spread of mark orderings, triples, and Hangul jamo runs.
function verificationCorpus(combiningClass: readonly CombiningClassEntry[]): string[] {
  const corpus = curatedVectors().map((vector) => vector.input);

  // A spread of non-starter marks across distinct combining classes.
  const sampleMarks = [
    0x0300, 0x0301, 0x0302, 0x0307, 0x0308, 0x0316, 0x0323, 0x0327, 0x0328, 0x031b, 0x0334, 0x0345,
    0x05b0, 0x05c1, 0x05c2, 0x064b, 0x0653, 0x093c, 0x0f71, 0x0f72, 0x1dc0,
  ].filter((mark) => combiningClass.some(([codePoint]) => codePoint === mark));

  // Every ordered pair and triple of sample marks after a base letter — drives
  // the canonical-ordering and blocked-composition paths exhaustively.
  for (const first of sampleMarks) {
    for (const second of sampleMarks) {
      corpus.push(`a${String.fromCodePoint(first, second)}`);
      for (const third of sampleMarks) {
        corpus.push(`e${String.fromCodePoint(first, second, third)}`);
      }
    }
  }

  // Random multi-code-point strings drawn from a mark-heavy alphabet.
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
}

// Prove `normalizeNfkc` against the supplied tables equals the runtime's
// `String.prototype.normalize("NFKC")` — for every code point, then for the
// adversarial multi-code-point corpus, plus idempotence and NFKC-stability.
function verifyAgainstRuntime(
  tables: NfkcTables,
  combiningClass: readonly CombiningClassEntry[],
): VerificationResult {
  let singleCodePointChecks = 0;
  for (let codePoint = 0; codePoint <= MAX_CODE_POINT; codePoint += 1) {
    if (isSurrogate(codePoint)) {
      continue;
    }
    const source = String.fromCodePoint(codePoint);
    const frozen = normalizeNfkc(source, tables);
    const runtime = source.normalize("NFKC");
    if (frozen !== runtime) {
      throw new Error(
        `frozen NFKC disagrees with the runtime at U+${codePoint.toString(16)}: ` +
          `frozen=${JSON.stringify(frozen)} runtime=${JSON.stringify(runtime)}`,
      );
    }
    singleCodePointChecks += 1;
  }

  let corpusChecks = 0;
  for (const input of verificationCorpus(combiningClass)) {
    const frozen = normalizeNfkc(input, tables);
    const runtime = input.normalize("NFKC");
    if (frozen !== runtime) {
      throw new Error(
        `frozen NFKC disagrees with the runtime for ${JSON.stringify(input)}: ` +
          `frozen=${JSON.stringify(frozen)} runtime=${JSON.stringify(runtime)}`,
      );
    }
    if (normalizeNfkc(frozen, tables) !== frozen) {
      throw new Error(`frozen NFKC is not idempotent for ${JSON.stringify(input)}`);
    }
    corpusChecks += 1;
  }
  return { singleCodePointChecks, corpusChecks };
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
// Source of truth: scripts/generate-nfkc-table.ts
// Regenerate: bun run scripts/generate-nfkc-table.ts
//
// Frozen Unicode NFKC tables, captured at the version below. The moat
// normalises against THESE tables, not the host runtime's live Unicode data,
// so a signer and a verifier on different Node/Bun/ICU versions agree on the
// sanitized bytes. The cross-language wire artifact and Go reference live in
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
): string {
  const description =
    "Frozen Unicode NFKC tables for the SanitizedString moat. decomposition is " +
    "the fully-recursive NFKD mapping (Hangul excluded — decomposed " +
    "algorithmically); composition is the [starter, second, composite] " +
    "canonical pairs; combiningClass is the non-zero Canonical_Combining_Class " +
    "values. normalizationVectors is a curated corpus every language port must " +
    "agree on. Run by verifier-reference.go and tests/nfkc.spec.ts.";
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
      const expected = vector.input.normalize("NFKC");
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
  const runtimeUnicodeVersion = process.versions.unicode ?? "unknown";
  if (!runtimeUnicodeVersion.startsWith(PINNED_UNICODE_VERSION)) {
    throw new Error(
      `this generator must run on a runtime shipping Unicode ${PINNED_UNICODE_VERSION} ` +
        `(it derives the decomposition/composition tables from the runtime and pairs them ` +
        `with the vendored Unicode ${PINNED_UNICODE_VERSION} combining classes); ` +
        `the current runtime ships Unicode ${runtimeUnicodeVersion}. Use the pinned Bun.`,
    );
  }

  const scriptDir = dirname(fileURLToPath(import.meta.url));
  const packageRoot = join(scriptDir, "..");

  const combiningClassText = readFileSync(join(scriptDir, DERIVED_COMBINING_CLASS_FILE), "utf8");
  const combiningClass = parseCombiningClasses(combiningClassText);
  const decomposition = buildDecomposition();
  const composition = buildComposition();

  const tables = buildTables(decomposition, composition, combiningClass);
  assertWellFormed(decomposition, composition, tables);
  const result = verifyAgainstRuntime(tables, combiningClass);

  const tableModulePath = join(packageRoot, "src", "nfkc-table.generated.ts");
  writeFileSync(
    tableModulePath,
    renderTableModule(decomposition, composition, combiningClass),
    "utf8",
  );
  // A handful of long decomposition rows (Arabic presentation ligatures) exceed
  // Biome's line width; let Biome reflow the generated module so a fresh
  // `bun run` of this script produces a file that already passes `biome check`.
  execFileSync("bunx", ["biome", "format", "--write", tableModulePath], { cwd: packageRoot });

  const jsonPath = join(packageRoot, "testdata", "nfkc-table.json");
  writeFileSync(jsonPath, renderJsonArtifact(decomposition, composition, combiningClass), "utf8");

  process.stdout.write(
    `nfkc tables: Unicode ${PINNED_UNICODE_VERSION}, ${decomposition.length} decompositions, ` +
      `${composition.length} composition pairs, ${combiningClass.length} combining classes\n` +
      `  verified: ${result.singleCodePointChecks} single code points + ` +
      `${result.corpusChecks} corpus strings match the runtime NFKC + idempotent\n` +
      `  wrote ${tableModulePath}\n  wrote ${jsonPath}\n`,
  );
}

main();
