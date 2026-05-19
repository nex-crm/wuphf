import * as fc from "fast-check";
import { describe, expect, it } from "vitest";
import { frozenNfkc } from "../src/nfkc.ts";
import { NFKC_UNICODE_VERSION } from "../src/nfkc-table.generated.ts";
import { isMoatDisallowedCodePoint } from "../src/sanitized-string.ts";
import nfkcTableJson from "../testdata/nfkc-table.json";

const NUM_RUNS = 2000;

// NOTE on what this file does NOT test: the exhaustive proof that `frozenNfkc`
// equals the runtime's `String.prototype.normalize("NFKC")` for every code
// point. That comparison is only meaningful on a runtime shipping the pinned
// Unicode version (15.1), and Vitest executes specs in Node workers (Unicode
// 17.0) even under Bun — so an in-suite equivalence test could never run. The
// proof lives in scripts/generate-nfkc-table.ts (`verifyAgainstRuntime`, over
// all 1,112,064 code points) and is gated on every PR by the "Frozen NFKC
// table drift check" CI step, which regenerates on the pinned Bun and fails on
// any drift. The tests below are all runtime-independent: they pin `frozenNfkc`
// to the frozen tables, not to the host's Unicode data.

// A string contains a lone surrogate if any UTF-16 unit is an unpaired half.
function hasLoneSurrogate(value: string): boolean {
  for (let index = 0; index < value.length; index += 1) {
    const unit = value.charCodeAt(index);
    if (unit >= 0xd800 && unit <= 0xdbff) {
      const next = value.charCodeAt(index + 1);
      if (Number.isNaN(next) || next < 0xdc00 || next > 0xdfff) {
        return true;
      }
      index += 1;
    } else if (unit >= 0xdc00 && unit <= 0xdfff) {
      return true;
    }
  }
  return false;
}

// A mark-heavy alphabet: base letters, combining marks across several
// canonical combining classes, precomposed/compatibility/ligature/fullwidth
// decomposables, astral decomposables, and Hangul jamo + syllables. Random
// strings over this alphabet exercise decomposition, canonical ordering, and
// the blocked-composition rule far more densely than printable-text draws.
const NFKC_ALPHABET = [
  "a",
  "e",
  "n",
  "o",
  "u",
  "O",
  "U",
  "s",
  "̀",
  "́",
  "̂",
  "̇",
  "̈",
  "̖",
  "̣",
  "̧",
  "̨",
  "̛",
  "̴",
  "ͅ",
  "ְ",
  "ً",
  "़",
  "ཱ",
  "À",
  "Ü",
  "Ǖ",
  "ẛ",
  "ế",
  "̈́",
  "Ω",
  "Å",
  "ﬀ",
  "ﷺ",
  "Ａ",
  "①",
  "½",
  "㌀",
  "Ǆ",
  "ᄀ",
  "ᅡ",
  "ᆨ",
  "가",
  "힣",
  "\u{1d400}",
  "\u{1f600}",
];

const nfkcStringArb = fc
  .array(fc.constantFrom(...NFKC_ALPHABET), { minLength: 0, maxLength: 12 })
  .map((parts) => parts.join(""));

interface NormalizationVector {
  readonly input: string;
  readonly expected: string;
  readonly name: string;
}

const normalizationVectors: readonly NormalizationVector[] = nfkcTableJson.normalizationVectors;

describe("frozenNfkc", () => {
  it("pins the tables to a known Unicode version", () => {
    expect(NFKC_UNICODE_VERSION).toBe("15.1");
    expect(nfkcTableJson.unicodeVersion).toBe(NFKC_UNICODE_VERSION);
  });

  describe("cross-language vectors", () => {
    // The curated corpus in testdata/nfkc-table.json — `expected` was frozen
    // at table-generation time. verifier-reference.go checks the same corpus,
    // so TS and the Go reference are pinned to one oracle. Runtime-independent.
    it("has a non-empty curated corpus", () => {
      expect(normalizationVectors.length).toBeGreaterThan(0);
    });

    for (const vector of normalizationVectors) {
      it(`normalizes: ${vector.name}`, () => {
        expect(frozenNfkc(vector.input)).toBe(vector.expected);
      });
    }
  });

  describe("targeted edge cases", () => {
    it("returns empty for empty input", () => {
      expect(frozenNfkc("")).toBe("");
    });

    it("leaves pure ASCII untouched", () => {
      expect(frozenNfkc("plain ascii 123")).toBe("plain ascii 123");
    });

    it("resolves singleton decompositions", () => {
      expect(frozenNfkc("Ω")).toBe("Ω"); // OHM SIGN → GREEK CAPITAL OMEGA
      expect(frozenNfkc("K")).toBe("K"); // KELVIN SIGN → LATIN CAPITAL K
      expect(frozenNfkc("Å")).toBe("Å"); // ANGSTROM SIGN → Å (A + ring)
    });

    it("composes a base and a combining mark", () => {
      expect(frozenNfkc("Å")).toBe("Å"); // A + ring above → Å
      expect(frozenNfkc("Å")).toBe("Å"); // already composed
    });

    it("recomposes multi-mark precomposed characters", () => {
      // U+01D5 = Ǖ. Decomposes recursively to U + diaeresis + macron; the
      // single-level composition is Ü + macron. Both spellings normalize equal.
      expect(frozenNfkc("Ǖ")).toBe("Ǖ");
      expect(frozenNfkc("Ǖ")).toBe("Ǖ");
      expect(frozenNfkc("Ǖ")).toBe("Ǖ");
    });

    it("canonically orders combining marks by combining class", () => {
      // dot above is class 230, dot below is class 220 — output is class-sorted
      // regardless of input order, and the two orderings normalize equal.
      expect(frozenNfkc("q̣̇")).toBe("q̣̇");
      expect(frozenNfkc("q̣̇")).toBe("q̣̇");
    });

    it("expands compatibility decompositions", () => {
      expect(frozenNfkc("ﬀ")).toBe("ff"); // LATIN SMALL LIGATURE FF
      expect(frozenNfkc("Ａ")).toBe("A"); // FULLWIDTH LATIN CAPITAL A
      expect(frozenNfkc("①")).toBe("1"); // CIRCLED DIGIT ONE
      expect(frozenNfkc("½")).toBe("1⁄2"); // VULGAR FRACTION ONE HALF
    });

    it("decomposes and recomposes Hangul algorithmically", () => {
      expect(frozenNfkc("각")).toBe("각"); // L+V+T jamo → 각
      expect(frozenNfkc("가")).toBe("가"); // L+V jamo → 가
      expect(frozenNfkc("가")).toBe("가"); // syllable stays composed
      expect(frozenNfkc("힣")).toBe("힣"); // last syllable stays composed
    });

    it("normalizes astral compatibility characters", () => {
      expect(frozenNfkc("\u{1d400}")).toBe("A"); // MATHEMATICAL BOLD CAPITAL A
    });
  });

  describe("invariants", () => {
    it("is idempotent", () => {
      fc.assert(
        fc.property(nfkcStringArb, (input) => {
          const once = frozenNfkc(input);
          expect(frozenNfkc(once)).toBe(once);
        }),
        { numRuns: NUM_RUNS },
      );
    });

    it("never emits a lone surrogate for well-formed input", () => {
      fc.assert(
        fc.property(
          fc
            .string({ unit: "grapheme", maxLength: 24 })
            .filter((value) => !hasLoneSurrogate(value)),
          (input) => {
            expect(hasLoneSurrogate(frozenNfkc(input))).toBe(false);
          },
        ),
        { numRuns: NUM_RUNS },
      );
    });

    it("never introduces a moat-disallowed code point", () => {
      // `sanitizeText` strips moat-disallowed code points ONCE, between two
      // `frozenNfkc` passes, and relies on the second pass not re-introducing
      // one. NFKC compatibility/canonical mappings only ever expand to
      // letters, marks, digits, and punctuation — never `\p{C}` or
      // default-ignorables. This pins that: any disallowed code point in the
      // output must have been present in the input, never created by NFKC.
      fc.assert(
        fc.property(nfkcStringArb, (input) => {
          const inputCodePoints = new Set<number>();
          for (const character of input) {
            const codePoint = character.codePointAt(0);
            if (codePoint !== undefined) {
              inputCodePoints.add(codePoint);
            }
          }
          for (const character of frozenNfkc(input)) {
            const codePoint = character.codePointAt(0);
            if (codePoint !== undefined && isMoatDisallowedCodePoint(codePoint)) {
              expect(inputCodePoints.has(codePoint)).toBe(true);
            }
          }
        }),
        { numRuns: NUM_RUNS },
      );
    });
  });
});
