// generate-moat-table.ts — regenerate the frozen moat code-point table.
//
// The `allowlist` ("moat") policy rejects every Unicode `\p{C}` (Cc, Cf, Cn,
// Co, Cs) and every `\p{Default_Ignorable_Code_Point}`. Those property escapes
// resolve against the ICU/Unicode data baked into the *running* V8 — and that
// data shifts every Unicode release (`Cn` unassigned code points become
// assigned letters; new default-ignorables appear). For a security boundary
// whose output is signed and re-verified on a *different* runtime, the moat
// boundary cannot be a moving target: a signer on Node 22 and a verifier on
// Node 24 would disagree on whether a string is moat-clean.
//
// So the moat classifies against a FROZEN table, captured once at a known
// Unicode version, not against the live `\p{...}` data. This script is the
// one place the live data is consulted. Run it deliberately to bump the
// pinned Unicode version; never let the sanitizer read `\p{...}` at runtime.
//
//   bun run scripts/generate-moat-table.ts
//
// It writes two artifacts from a single source of truth:
//   - src/moat-disallowed-table.ts    embedded range table the sanitizer uses
//   - testdata/moat-disallowed-table.json   cross-language wire artifact +
//                                           curated classification vectors,
//                                           verified by verifier-reference.go
//
// NOTE: NFKC normalization is also Unicode-version-dependent and is NOT
// frozen here — `String.prototype.normalize` still uses the runtime's data.
// Freezing NFKC would mean shipping a normalizer; that is a separate, larger
// effort. This script closes the classification half of the coupling.

import { writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const MOAT_PROPERTY_RE = /[\p{C}\p{Default_Ignorable_Code_Point}]/u;
const MAX_CODE_POINT = 0x10ffff;

type Range = readonly [number, number];

// Build the disallowed set as sorted, non-overlapping, non-adjacent inclusive
// ranges by sweeping the entire code-point space once.
function buildDisallowedRanges(): Range[] {
  const ranges: Range[] = [];
  let start = -1;
  for (let cp = 0; cp <= MAX_CODE_POINT; cp++) {
    const disallowed = MOAT_PROPERTY_RE.test(String.fromCodePoint(cp));
    if (disallowed && start === -1) {
      start = cp;
    } else if (!disallowed && start !== -1) {
      ranges.push([start, cp - 1]);
      start = -1;
    }
  }
  if (start !== -1) {
    ranges.push([start, MAX_CODE_POINT]);
  }
  return ranges;
}

function inRanges(codePoint: number, ranges: readonly Range[]): boolean {
  for (const [rangeStart, rangeEnd] of ranges) {
    if (codePoint >= rangeStart && codePoint <= rangeEnd) {
      return true;
    }
  }
  return false;
}

// Curated adversarial corpus. Each `disallowed` value is the HUMAN-asserted
// expectation; the generator cross-checks it against the freshly built ranges
// and aborts on any mismatch, so the vectors and the table cannot drift.
interface VectorSpec {
  readonly codePoint: number;
  readonly name: string;
  readonly disallowed: boolean;
}

const VECTOR_SPECS: readonly VectorSpec[] = [
  // Allowed: ordinary assigned text.
  { codePoint: 0x41, name: "U+0041 LATIN CAPITAL LETTER A", disallowed: false },
  { codePoint: 0x7a, name: "U+007A LATIN SMALL LETTER Z", disallowed: false },
  { codePoint: 0x30, name: "U+0030 DIGIT ZERO", disallowed: false },
  { codePoint: 0x20, name: "U+0020 SPACE", disallowed: false },
  { codePoint: 0xa0, name: "U+00A0 NO-BREAK SPACE", disallowed: false },
  { codePoint: 0x301, name: "U+0301 COMBINING ACUTE ACCENT", disallowed: false },
  { codePoint: 0x4e2d, name: "U+4E2D CJK UNIFIED IDEOGRAPH-4E2D", disallowed: false },
  { codePoint: 0x10000, name: "U+10000 LINEAR B SYLLABLE B008 A", disallowed: false },
  { codePoint: 0x1f600, name: "U+1F600 GRINNING FACE", disallowed: false },
  // Disallowed: Cc control (tab/LF/CR are Cc too — the table is the raw
  // property union; the sanitizer applies the tab/LF/CR carve-out on top).
  { codePoint: 0x00, name: "U+0000 NULL", disallowed: true },
  { codePoint: 0x09, name: "U+0009 CHARACTER TABULATION", disallowed: true },
  { codePoint: 0x0a, name: "U+000A LINE FEED", disallowed: true },
  { codePoint: 0x0d, name: "U+000D CARRIAGE RETURN", disallowed: true },
  { codePoint: 0x1f, name: "U+001F INFORMATION SEPARATOR ONE", disallowed: true },
  { codePoint: 0x7f, name: "U+007F DELETE", disallowed: true },
  { codePoint: 0x9f, name: "U+009F APPLICATION PROGRAM COMMAND", disallowed: true },
  // Disallowed: Cf format.
  { codePoint: 0xad, name: "U+00AD SOFT HYPHEN", disallowed: true },
  { codePoint: 0x61c, name: "U+061C ARABIC LETTER MARK", disallowed: true },
  { codePoint: 0x200d, name: "U+200D ZERO WIDTH JOINER", disallowed: true },
  { codePoint: 0x200e, name: "U+200E LEFT-TO-RIGHT MARK", disallowed: true },
  { codePoint: 0xfeff, name: "U+FEFF ZERO WIDTH NO-BREAK SPACE (BOM)", disallowed: true },
  // Disallowed: Default_Ignorable that is NOT \p{C} (the denylist and a bare
  // \p{C} both miss these — the canonical reason the moat unions both sets).
  { codePoint: 0x34f, name: "U+034F COMBINING GRAPHEME JOINER", disallowed: true },
  { codePoint: 0x115f, name: "U+115F HANGUL CHOSEONG FILLER", disallowed: true },
  { codePoint: 0x1160, name: "U+1160 HANGUL JUNGSEONG FILLER", disallowed: true },
  { codePoint: 0x17b4, name: "U+17B4 KHMER VOWEL INHERENT AQ", disallowed: true },
  { codePoint: 0xfe0f, name: "U+FE0F VARIATION SELECTOR-16", disallowed: true },
  { codePoint: 0xe0100, name: "U+E0100 VARIATION SELECTOR-17", disallowed: true },
  // Disallowed: Co private use, BMP and both astral planes.
  { codePoint: 0xe000, name: "U+E000 PRIVATE USE", disallowed: true },
  { codePoint: 0xf8ff, name: "U+F8FF PRIVATE USE (Apple logo)", disallowed: true },
  { codePoint: 0xf0000, name: "U+F0000 SUPPLEMENTARY PRIVATE USE AREA-A", disallowed: true },
  { codePoint: 0x100000, name: "U+100000 SUPPLEMENTARY PRIVATE USE AREA-B", disallowed: true },
  // Disallowed: Cs surrogate.
  { codePoint: 0xd800, name: "U+D800 HIGH SURROGATE", disallowed: true },
  { codePoint: 0xdfff, name: "U+DFFF LOW SURROGATE", disallowed: true },
  // Disallowed: Cn unassigned.
  { codePoint: 0x378, name: "U+0378 (unassigned)", disallowed: true },
  // Range-edge probes: the code points immediately around the first range.
  { codePoint: 0x1e, name: "U+001E INFORMATION SEPARATOR TWO (in range)", disallowed: true },
  { codePoint: 0xa1, name: "U+00A1 INVERTED EXCLAMATION MARK (just past Cc)", disallowed: false },
];

function verifyVectors(ranges: readonly Range[]): void {
  for (const vector of VECTOR_SPECS) {
    const actual = inRanges(vector.codePoint, ranges);
    if (actual !== vector.disallowed) {
      throw new Error(
        `vector mismatch for ${vector.name}: hand-asserted disallowed=${vector.disallowed} ` +
          `but the generated ranges say ${actual}. Fix the VECTOR_SPECS entry or the ` +
          `expectation — the curated corpus must agree with the live \\p{...} data.`,
      );
    }
  }
}

// Assert the ranges are well-formed: sorted, non-overlapping, non-adjacent.
// A malformed table silently breaks the binary search the sanitizer relies on.
function assertWellFormed(ranges: readonly Range[]): void {
  let previousEnd = -2;
  for (const [rangeStart, rangeEnd] of ranges) {
    if (rangeStart > rangeEnd) {
      throw new Error(`inverted range [${rangeStart}, ${rangeEnd}]`);
    }
    if (rangeStart <= previousEnd + 1) {
      throw new Error(`range [${rangeStart}, ${rangeEnd}] is not strictly after ${previousEnd}`);
    }
    previousEnd = rangeEnd;
  }
}

function hex(value: number): string {
  return `0x${value.toString(16)}`;
}

function renderTableModule(unicodeVersion: string, ranges: readonly Range[]): string {
  const rangeLines = ranges.map(([start, end]) => `  [${hex(start)}, ${hex(end)}],`).join("\n");
  return `// GENERATED FILE — do not edit by hand.
// Source of truth: scripts/generate-moat-table.ts
// Regenerate: bun run scripts/generate-moat-table.ts
//
// Frozen union of Unicode \\p{C} (Cc, Cf, Cn, Co, Cs) and
// \\p{Default_Ignorable_Code_Point}, captured at the Unicode version below.
// The moat (\`allowlist\` policy) classifies against THIS table, not the host
// runtime's live \\p{...} data, so the classification boundary is the same on
// any Node/ICU version. (NFKC normalization remains runtime-coupled; see the
// LIMITATION note in sanitized-string.ts.) The cross-language wire artifact
// and Go reference live in testdata/moat-disallowed-table.json.

export const MOAT_UNICODE_VERSION = ${JSON.stringify(unicodeVersion)};

// Sorted, non-overlapping, non-adjacent inclusive [start, end] code-point
// ranges. Binary-searched by isMoatDisallowedCodePoint in sanitized-string.ts.
export const MOAT_DISALLOWED_RANGES: readonly (readonly [number, number])[] = [
${rangeLines}
];
`;
}

// Hand-rolled so each [start, end] range and each vector sits on one line —
// JSON.stringify(_, null, 2) explodes every pair onto four lines, making a
// 746-range diff unreviewable.
function renderJsonArtifact(unicodeVersion: string, ranges: readonly Range[]): string {
  const description =
    "Frozen Unicode \\p{C} + \\p{Default_Ignorable_Code_Point} union for the " +
    "SanitizedString allowlist (moat) policy. disallowedRanges is the wire " +
    "contract; tab/LF/CR are Cc and therefore listed as disallowed here — the " +
    "sanitizer applies the tab/LF/CR carve-out on top of this raw table. " +
    "classificationVectors is a curated corpus every language port must agree on.";
  const rangeLines = ranges.map(([start, end]) => `    [${start}, ${end}]`).join(",\n");
  const vectorLines = VECTOR_SPECS.map(
    (vector) =>
      `    {"codePoint": ${vector.codePoint}, "name": ${JSON.stringify(vector.name)}, ` +
      `"disallowed": ${vector.disallowed}}`,
  ).join(",\n");
  return `{
  "description": ${JSON.stringify(description)},
  "unicodeVersion": ${JSON.stringify(unicodeVersion)},
  "generatedBy": "packages/protocol/scripts/generate-moat-table.ts",
  "disallowedRanges": [
${rangeLines}
  ],
  "classificationVectors": [
${vectorLines}
  ]
}
`;
}

function main(): void {
  const unicodeVersion = process.versions.unicode ?? "unknown";
  if (unicodeVersion === "unknown") {
    throw new Error("process.versions.unicode is unavailable; cannot pin the moat table");
  }

  const ranges = buildDisallowedRanges();
  assertWellFormed(ranges);
  verifyVectors(ranges);

  const scriptDir = dirname(fileURLToPath(import.meta.url));
  const packageRoot = join(scriptDir, "..");

  const tableModulePath = join(packageRoot, "src", "moat-disallowed-table.ts");
  writeFileSync(tableModulePath, renderTableModule(unicodeVersion, ranges), "utf8");

  const jsonPath = join(packageRoot, "testdata", "moat-disallowed-table.json");
  writeFileSync(jsonPath, renderJsonArtifact(unicodeVersion, ranges), "utf8");

  process.stdout.write(
    `moat table: Unicode ${unicodeVersion}, ${ranges.length} ranges, ` +
      `${VECTOR_SPECS.length} vectors\n` +
      `  wrote ${tableModulePath}\n  wrote ${jsonPath}\n`,
  );
}

main();
