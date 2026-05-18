import * as fc from "fast-check";
import { describe, expect, it } from "vitest";
import { MAX_SANITIZED_JSON_NODES, MAX_SANITIZED_STRING_BYTES } from "../src/budgets.ts";
import { MOAT_DISALLOWED_RANGES, MOAT_UNICODE_VERSION } from "../src/moat-disallowed-table.ts";
import {
  isMoatDisallowedCodePoint,
  SanitizedString,
  type SanitizedStringOptions,
  type SanitizedStringPolicy,
} from "../src/sanitized-string.ts";
import moatTableJson from "../testdata/moat-disallowed-table.json";

type JsonPrimitive = null | boolean | number | string;
type JsonValue = JsonPrimitive | JsonValue[] | JsonRecord;
interface JsonRecord {
  readonly [key: string]: JsonValue;
}

const MOAT_NUM_RUNS = 1000;
const JSON_NUM_RUNS = 1000;

// The Unicode version of the runtime executing this test file. The frozen
// moat table is pinned to a fixed version; the live-Unicode cross-check below
// only applies when the two match.
const { unicode: RUNTIME_UNICODE_VERSION } = process.versions;

const BIDI_CHARS = [
  "\u202a",
  "\u202b",
  "\u202c",
  "\u202d",
  "\u202e",
  "\u2066",
  "\u2067",
  "\u2068",
  "\u2069",
] as const;
const FORMAT_INVISIBLE_CHARS = [
  "\u180e",
  "\u2060",
  "\u2061",
  "\u2062",
  "\u2063",
  "\u2064",
] as const;
const INVISIBLE_CODE_POINTS = [
  0x180e,
  0x200b,
  0x200c,
  0x200d,
  0x202a,
  0x202b,
  0x202c,
  0x202d,
  0x202e,
  0x2060,
  0x2061,
  0x2062,
  0x2063,
  0x2064,
  0x2066,
  0x2067,
  0x2068,
  0x2069,
  0xfeff,
  ...Array.from({ length: 0x80 }, (_, index) => 0xe0000 + index),
] as const;

// `fc.string()` defaults to `unit: 'grapheme-ascii'` (printable ASCII only).
// For a sanitizer that has to handle Unicode adversarially, that's a coverage
// hole — the property tests would never see combining marks, NFKC-decomposable
// homoglyphs, RTL marks, or astral-plane code points. `unit: 'grapheme'`
// (fast-check 4 successor to v3's `fullUnicodeString`) draws printable
// graphemes spanning the BMP and astral planes; `canExpectedSanitizeText`
// filters out the inputs we already know the production sanitizer rejects
// (lone surrogates, etc.).
const sanitizableStringArb = fc.string({ unit: "grapheme" }).filter(canExpectedSanitizeText);
const bidiCharArb = fc.constantFrom(...BIDI_CHARS);
const bidiInterleavedStringArb = fc
  .array(fc.tuple(sanitizableStringArb, bidiCharArb), { minLength: 1, maxLength: 16 })
  .chain((segments) => fc.tuple(fc.constant(segments), sanitizableStringArb))
  .map(
    ([segments, tail]) => `${segments.map(([chunk, bidi]) => `${chunk}${bidi}`).join("")}${tail}`,
  );
const formatInvisibleCharArb = fc.constantFrom(...FORMAT_INVISIBLE_CHARS);
const formatInvisibleInterleavedStringArb = fc
  .array(fc.tuple(sanitizableStringArb, formatInvisibleCharArb), {
    minLength: 1,
    maxLength: 16,
  })
  .chain((segments) => fc.tuple(fc.constant(segments), sanitizableStringArb))
  .map(
    ([segments, tail]) =>
      `${segments.map(([chunk, invisible]) => `${chunk}${invisible}`).join("")}${tail}`,
  );
const disallowedC0CharArb = fc
  .integer({ min: 0x00, max: 0x1f })
  .filter((code) => code !== 0x09 && code !== 0x0a && code !== 0x0d)
  .map((code) => String.fromCharCode(code));
const tagCharArb = fc
  .integer({ min: 0xe0000, max: 0xe007f })
  .map((codePoint) => String.fromCodePoint(codePoint));
const jsonObjectArb = fc
  .jsonValue()
  .filter((value): value is JsonRecord => isJsonRecord(value) && canExpectedRoundTripJson(value));

// Code points the moat (allowlist) policy must strip but the default denylist
// keeps — at least one per rejected class (Cc/Cf/Cn/Co + non-`C*`
// Default_Ignorable). `Cs` (surrogates) is intentionally absent: a lone
// surrogate is rejected by `rejectLoneSurrogates` before the moat ever runs,
// and a paired surrogate is a valid astral character. `fc.string({ unit:
// "grapheme" })` emits printable text only and structurally almost never
// produces these, so the moat property tests below would run near-vacuously
// without an arbitrary that deliberately injects them.
const MOAT_DISALLOWED_SAMPLE = [
  "\u00ad", // SOFT HYPHEN — Cf
  "\u061c", // ARABIC LETTER MARK — Cf
  "\u200e", // LEFT-TO-RIGHT MARK — Cf
  "\ufeff", // BYTE ORDER MARK — Cf
  "\u0378", // UNASSIGNED — Cn
  "\u034f", // COMBINING GRAPHEME JOINER — Default_Ignorable, not C*
  "\u115f", // HANGUL CHOSEONG FILLER — Default_Ignorable, Lo
  "\u1160", // HANGUL JUNGSEONG FILLER — Default_Ignorable, Lo
  "\ufe0f", // VARIATION SELECTOR-16 — Default_Ignorable, Mn
  "\u17b4", // KHMER VOWEL INHERENT AQ — Default_Ignorable
  "\ue000", // PRIVATE USE — Co (BMP)
  String.fromCodePoint(0xe0100), // VARIATION SELECTOR-17 — astral Default_Ignorable
  String.fromCodePoint(0xf0000), // SUPPLEMENTARY PRIVATE USE AREA-A — astral Co
  String.fromCodePoint(0x100000), // SUPPLEMENTARY PRIVATE USE AREA-B — astral Co
] as const;
const moatDisallowedCharArb = fc.constantFrom(...MOAT_DISALLOWED_SAMPLE);
// Interleave moat-disallowed code points through ordinary sanitizable text so
// every property run feeds the moat something it must actually strip.
const moatInterleavedStringArb = fc
  .array(fc.tuple(sanitizableStringArb, moatDisallowedCharArb), { minLength: 1, maxLength: 16 })
  .chain((segments) => fc.tuple(fc.constant(segments), sanitizableStringArb))
  .map(
    ([segments, tail]) =>
      `${segments.map(([chunk, invisible]) => `${chunk}${invisible}`).join("")}${tail}`,
  );
// Bare combining marks. `fc.string({ unit: "grapheme" })` never emits a string
// that *starts* with one (a grapheme cluster always opens with a base), so
// `moatInterleavedStringArb` can never place a moat code point between a base
// letter and a combining mark — exactly the boundary the MAJOR-1 idempotence
// fix exists for. This arbitrary builds that boundary explicitly: stripping
// the middle code point from `base + <moat> + mark` leaves `base + mark`
// adjacent for NFKC to compose. Without the re-normalize fix the
// NFKC-stability and idempotence properties below pass vacuously.
const COMBINING_MARK_SAMPLE = [
  "\u0301", // COMBINING ACUTE ACCENT
  "\u0300", // COMBINING GRAVE ACCENT
  "\u0308", // COMBINING DIAERESIS
  "\u0327", // COMBINING CEDILLA
  "\u093c", // DEVANAGARI SIGN NUKTA
] as const;
const baseLetterArb = fc.constantFrom("a", "e", "n", "o", "u", "c", "\u0928");
const combiningMarkArb = fc.constantFrom(...COMBINING_MARK_SAMPLE);
const moatComposableStringArb = fc
  .array(fc.tuple(baseLetterArb, moatDisallowedCharArb, combiningMarkArb), {
    minLength: 1,
    maxLength: 12,
  })
  .map((triples) =>
    triples.map(([base, invisible, mark]) => `${base}${invisible}${mark}`).join(""),
  );
// Union of ordinary interleaving and the composition-boundary case, so the
// MAJOR-1 property tests exercise both stripping coverage and re-composition.
const moatStressStringArb = fc.oneof(moatInterleavedStringArb, moatComposableStringArb);
// JSON whose string keys and values carry moat-disallowed code points, so the
// allowlist JSON round-trip property exercises moat key/value stripping —
// `fc.jsonValue()` keys are printable ASCII only and never trigger it.
const moatJsonStringArb = fc
  .tuple(sanitizableStringArb, moatDisallowedCharArb, sanitizableStringArb)
  .map(([prefix, invisible, suffix]) => `${prefix}${invisible}${suffix}`)
  .filter(canExpectedSanitizeText);
const moatJsonObjectArb = fc
  .dictionary(moatJsonStringArb, fc.oneof(moatJsonStringArb, fc.integer(), fc.boolean()), {
    minKeys: 1,
  })
  .filter((value) => canExpectedRoundTripJson(value, "allowlist"));

describe("SanitizedString", () => {
  it("is idempotent after NFKC and stripping", () => {
    fc.assert(
      fc.property(sanitizableStringArb, (input) => {
        const once = SanitizedString.fromUnknown(input).value;
        const twice = SanitizedString.fromUnknown(once).value;
        expect(twice).toBe(once);
      }),
      { numRuns: MOAT_NUM_RUNS },
    );
  });

  it("strips bidi overrides and isolates", () => {
    fc.assert(
      fc.property(bidiInterleavedStringArb, (input) => {
        const result = SanitizedString.fromUnknown(input).value;
        expect(containsBidiOverride(result)).toBe(false);
      }),
      { numRuns: MOAT_NUM_RUNS },
    );
  });

  it("strips C0 controls except tab, newline, and carriage return", () => {
    fc.assert(
      fc.property(
        sanitizableStringArb,
        disallowedC0CharArb,
        sanitizableStringArb,
        (prefix, control, suffix) => {
          const result = SanitizedString.fromUnknown(`${prefix}${control}${suffix}`).value;
          expect(result).not.toContain(control);
          expect(containsDisallowedC0(result)).toBe(false);
        },
      ),
      { numRuns: MOAT_NUM_RUNS },
    );

    expect(SanitizedString.fromUnknown("\t\n\r").value).toBe("\t\n\r");
  });

  it("strips invisible-space tag characters", () => {
    fc.assert(
      fc.property(sanitizableStringArb, tagCharArb, sanitizableStringArb, (prefix, tag, suffix) => {
        const result = SanitizedString.fromUnknown(`${prefix}${tag}${suffix}`).value;
        expect(containsInvisibleTag(result)).toBe(false);
      }),
      { numRuns: MOAT_NUM_RUNS },
    );
  });

  it("strips invisible format characters wherever they are interleaved", () => {
    fc.assert(
      fc.property(formatInvisibleInterleavedStringArb, (input) => {
        const result = SanitizedString.fromUnknown(input).value;
        expect(containsFormatInvisible(result)).toBe(false);
      }),
      { numRuns: MOAT_NUM_RUNS },
    );
  });

  it("normalizes compatibility homoglyphs with NFKC", () => {
    expect(SanitizedString.fromUnknown("\ufb01le").value).toBe("file");
  });

  it("coerces safe primitive values to renderable text", () => {
    expect(SanitizedString.fromUnknown(743).value).toBe("743");
    expect(SanitizedString.fromUnknown(true).value).toBe("true");
    expect(SanitizedString.fromUnknown(false).value).toBe("false");
  });

  it("enforces the byte budget at the direct string boundary", () => {
    expect(SanitizedString.fromUnknown("x".repeat(MAX_SANITIZED_STRING_BYTES)).value).toHaveLength(
      MAX_SANITIZED_STRING_BYTES,
    );

    expect(() => SanitizedString.fromUnknown("x".repeat(MAX_SANITIZED_STRING_BYTES + 1))).toThrow(
      /SanitizedString input bytes exceeds MAX_SANITIZED_STRING_BYTES/,
    );
  });

  it("enforces the byte budget on object JSON projections", () => {
    const jsonOverheadBytes = '{"v":""}'.length;
    const atCap = { v: "x".repeat(MAX_SANITIZED_STRING_BYTES - jsonOverheadBytes) };
    const overCap = { v: "x".repeat(MAX_SANITIZED_STRING_BYTES - jsonOverheadBytes + 1) };

    expect(SanitizedString.fromUnknown(atCap).value).toHaveLength(MAX_SANITIZED_STRING_BYTES);
    expect(() => SanitizedString.fromUnknown(overCap)).toThrow(
      /SanitizedString JSON projection bytes exceeds MAX_SANITIZED_STRING_BYTES/,
    );
  });

  it("enforces the JSON node budget for flat arrays at the edge", () => {
    const atCap = Array.from({ length: MAX_SANITIZED_JSON_NODES - 1 }, () => null);
    const overCap = Array.from({ length: MAX_SANITIZED_JSON_NODES }, () => null);

    expect(JSON.parse(SanitizedString.fromUnknown(atCap).value) as unknown[]).toHaveLength(
      MAX_SANITIZED_JSON_NODES - 1,
    );
    expect(() => SanitizedString.fromUnknown(overCap)).toThrow(
      new RegExp(
        `SanitizedString JSON node count at \\$\\[${
          MAX_SANITIZED_JSON_NODES - 1
        }\\] exceeds budget`,
      ),
    );
  });

  it("enforces the JSON node budget for flat objects before descriptor copying", () => {
    const atCap = flatNullObject(MAX_SANITIZED_JSON_NODES - 1);
    const overCap = flatNullObject(MAX_SANITIZED_JSON_NODES);

    expect(
      Object.keys(JSON.parse(SanitizedString.fromUnknown(atCap).value) as object),
    ).toHaveLength(MAX_SANITIZED_JSON_NODES - 1);
    expect(() => SanitizedString.fromUnknown(overCap)).toThrow(
      new RegExp(
        `SanitizedString JSON node count at \\$\\.k${MAX_SANITIZED_JSON_NODES - 1} exceeds budget`,
      ),
    );
  });

  it("strips zero-width characters by default and can preserve ZWJ by policy", () => {
    expect(SanitizedString.fromUnknown("ze\u200bro").value).toBe("zero");
    expect(SanitizedString.fromUnknown("x\u200dy", { policy: "allow-zwj" }).value).toBe("x\u200dy");
    expect(SanitizedString.fromUnknown("x\u200cy", { policy: "allow-zwj" }).value).toBe("xy");
  });

  it("strips other commonly-weaponized invisible format characters (UTS #39)", () => {
    // U+180E MONGOLIAN VOWEL SEPARATOR: Default_Ignorable / Restricted under
    // UTS #39; NFKC does not remove it.
    expect(SanitizedString.fromUnknown("ad\u180emin").value).toBe("admin");
    // U+2060 WORD JOINER + U+2061..U+2064 INVISIBLE OPERATORS: same class.
    expect(SanitizedString.fromUnknown("ev\u2060il").value).toBe("evil");
    expect(SanitizedString.fromUnknown("a\u2061b").value).toBe("ab");
    expect(SanitizedString.fromUnknown("a\u2062b").value).toBe("ab");
    expect(SanitizedString.fromUnknown("a\u2063b").value).toBe("ab");
    expect(SanitizedString.fromUnknown("a\u2064b").value).toBe("ab");
    // U+2065 is OUTSIDE the rejected range \u2014 sanity check we don't over-strip.
    expect(SanitizedString.fromUnknown("a\u2065b").value).toBe("a\u2065b");
  });

  it("strips every invisible code point in the UTS #39 and R6 expansion list", () => {
    for (const codePoint of INVISIBLE_CODE_POINTS) {
      const invisible = String.fromCodePoint(codePoint);

      expect(SanitizedString.fromUnknown(`a${invisible}b`).value).toBe("ab");
    }
  });

  it("preserves ZWJ only under the explicit allow-zwj policy", () => {
    expect(SanitizedString.fromUnknown("a\u200db").value).toBe("ab");
    expect(SanitizedString.fromUnknown("a\u200db", { policy: "allow-zwj" }).value).toBe("a\u200db");
  });

  describe("allowlist policy", () => {
    const opts = { policy: "allowlist" as const };

    it("passes printable ASCII unchanged", () => {
      expect(SanitizedString.fromUnknown("hello world", opts).value).toBe("hello world");
      expect(SanitizedString.fromUnknown("a1b2_c-d.e", opts).value).toBe("a1b2_c-d.e");
    });

    it("preserves tab, newline, and carriage return (allowed whitespace, not invisibles)", () => {
      // U+0009/000A/000D are `Cc` and match `\p{C}`, but the moat must not
      // strip them — they carry the line structure a signed multi-line
      // payload depends on. Every other policy keeps them; allowlist must too.
      expect(SanitizedString.fromUnknown("a\tb\nc\rd", opts).value).toBe("a\tb\nc\rd");
      expect(SanitizedString.fromUnknown("line one\nline two", opts).value).toBe(
        "line one\nline two",
      );
    });

    it("passes common non-Latin scripts (letters, marks, numbers, punctuation)", () => {
      // Hiragana, Katakana, Han, Hangul, Cyrillic, Arabic, Devanagari, Greek.
      expect(SanitizedString.fromUnknown("\u3053\u3093\u306b\u3061\u306f", opts).value).toBe(
        "\u3053\u3093\u306b\u3061\u306f",
      );
      expect(SanitizedString.fromUnknown("\u5317\u4eac", opts).value).toBe("\u5317\u4eac");
      expect(SanitizedString.fromUnknown("\uc548\ub155", opts).value).toBe("\uc548\ub155");
      expect(SanitizedString.fromUnknown("\u041f\u0440\u0438\u0432\u0435\u0442", opts).value).toBe(
        "\u041f\u0440\u0438\u0432\u0435\u0442",
      );
      expect(SanitizedString.fromUnknown("\u0645\u0631\u062d\u0628\u0627", opts).value).toBe(
        "\u0645\u0631\u062d\u0628\u0627",
      );
      expect(SanitizedString.fromUnknown("\u0928\u092e\u0938\u094d\u0924\u0947", opts).value).toBe(
        "\u0928\u092e\u0938\u094d\u0924\u0947",
      );
      expect(
        SanitizedString.fromUnknown("\u039a\u03b1\u03bb\u03b7\u03bc\u03ad\u03c1\u03b1", opts).value,
      ).toBe("\u039a\u03b1\u03bb\u03b7\u03bc\u03ad\u03c1\u03b1");
    });

    it("passes emoji (without ZWJ, since ZWJ is Cf)", () => {
      expect(SanitizedString.fromUnknown("\ud83d\ude80", opts).value).toBe("\ud83d\ude80");
      expect(SanitizedString.fromUnknown("\ud83d\ude00\u2b50\ud83d\udcaf", opts).value).toBe(
        "\ud83d\ude00\u2b50\ud83d\udcaf",
      );
    });

    it("strips soft hyphen U+00AD (Cf \u2014 accepted by default policy, rejected by allowlist)", () => {
      // U+00AD is Cf "Format". The default denylist does not strip it; the
      // allowlist moat does. This is the canonical example of the policy
      // adding coverage beyond strip-zero-width.
      expect(SanitizedString.fromUnknown("foo\u00adbar").value).toBe("foo\u00adbar");
      expect(SanitizedString.fromUnknown("foo\u00adbar", opts).value).toBe("foobar");
    });

    it("strips private-use code points (Co \u2014 never legitimate text)", () => {
      // U+E000 sits in the BMP Private Use Area. Apps sometimes use these for
      // app-specific glyphs but the rendered text contract MUST NOT accept
      // them \u2014 they are invisible-on-most-systems and trivially used for
      // tracking / homograph attacks.
      expect(SanitizedString.fromUnknown("a\ue000b", opts).value).toBe("ab");
      // U+F8FF is the Apple logo PUA point.
      expect(SanitizedString.fromUnknown("a\uf8ffb", opts).value).toBe("ab");
      // Supplementary Private Use Area-A (U+F0000) and -B (U+100000) are
      // astral Co code points. The moat iterates by code point, so an astral
      // regression would not be caught by the BMP cases above.
      expect(SanitizedString.fromUnknown(`a${String.fromCodePoint(0xf0000)}b`, opts).value).toBe(
        "ab",
      );
      expect(SanitizedString.fromUnknown(`a${String.fromCodePoint(0x100000)}b`, opts).value).toBe(
        "ab",
      );
    });

    it("strips every bidi/format control, not just the U+202A-E and U+2066-9 ranges", () => {
      // U+061C ARABIC LETTER MARK is Cf and is NOT in the default denylist.
      // Allowlist must catch it.
      expect(SanitizedString.fromUnknown("a\u061cb").value).toBe("a\u061cb");
      expect(SanitizedString.fromUnknown("a\u061cb", opts).value).toBe("ab");
      // U+200E LEFT-TO-RIGHT MARK is Cf, also not in the default denylist.
      expect(SanitizedString.fromUnknown("a\u200eb").value).toBe("a\u200eb");
      expect(SanitizedString.fromUnknown("a\u200eb", opts).value).toBe("ab");
    });

    it("strips ZWJ (allowlist supersedes allow-zwj)", () => {
      // allow-zwj is meant to coexist with the default denylist for emoji
      // sequences. The allowlist contract is stricter \u2014 even under an
      // explicit `policy: "allowlist"` ZWJ is stripped because it's Cf.
      expect(SanitizedString.fromUnknown("a\u200db", opts).value).toBe("ab");
    });

    it("re-normalizes after stripping so output is NFKC-stable", () => {
      // MAJOR-1 regression. Stripping a code point can leave neighbours that
      // NFKC would compose (strip U+034F from `a U+034F U+0301` and the now-
      // adjacent `a U+0301` composes to `\u00e1`). The moat re-normalizes after
      // stripping; without that, output is not NFKC-stable and re-sanitizing
      // yields different bytes \u2014 fatal for the cosign path, which re-sanitizes
      // at its own trust boundary and compares bytes. `moatStressStringArb`
      // includes the `base + <moat> + combining-mark` case, so reverting the
      // re-normalize fix makes this property fail (verified) rather than pass
      // vacuously.
      fc.assert(
        fc.property(moatStressStringArb, (input) => {
          const out = SanitizedString.fromUnknown(input, opts).value;
          expect(out).toBe(out.normalize("NFKC"));
          // Stripping plus canonical re-composition is non-increasing in code
          // points: the moat only ever removes, never inserts.
          expect([...out].length).toBeLessThanOrEqual([...input.normalize("NFKC")].length);
        }),
        { numRuns: MOAT_NUM_RUNS },
      );
    });

    it("is idempotent under repeated allowlist sanitization", () => {
      // Re-sanitizing a moat-clean string must be a no-op \u2014 the cosign codec
      // re-sanitizes under `allowlist` at its own trust boundary. Driven from
      // `moatStressStringArb` (which exposes a re-composition boundary) so the
      // property fails against the pre-fix code instead of passing vacuously.
      fc.assert(
        fc.property(moatStressStringArb, (input) => {
          const once = SanitizedString.fromUnknown(input, opts).value;
          const twice = SanitizedString.fromUnknown(once, opts).value;
          expect(twice).toBe(once);
        }),
        { numRuns: MOAT_NUM_RUNS },
      );
    });

    it("never lets a Unicode C* code point through, except allowed whitespace", () => {
      fc.assert(
        fc.property(moatInterleavedStringArb, (input) => {
          const out = SanitizedString.fromUnknown(input, opts).value;
          // No assigned format, unassigned, private-use, or surrogate code
          // points should survive. Tab/newline/carriage return are `Cc` but
          // intentionally-allowed whitespace, so strip them before asserting.
          // Lone surrogates are already rejected earlier. The live `\p{C}`
          // regex is an independent cross-check against the frozen table.
          const withoutAllowedWhitespace = out.replace(/[\t\n\r]/g, "");
          expect(/\p{C}/u.test(withoutAllowedWhitespace)).toBe(false);
        }),
        { numRuns: MOAT_NUM_RUNS },
      );
    });

    it.each([
      { label: "U+115F HANGUL CHOSEONG FILLER", codePoint: 0x115f },
      { label: "U+1160 HANGUL JUNGSEONG FILLER", codePoint: 0x1160 },
      { label: "U+034F COMBINING GRAPHEME JOINER", codePoint: 0x034f },
      { label: "U+FE0F VARIATION SELECTOR-16", codePoint: 0xfe0f },
      { label: "U+E0100 VARIATION SELECTOR-17", codePoint: 0xe0100 },
      { label: "U+17B4 KHMER VOWEL INHERENT AQ", codePoint: 0x17b4 },
    ])("strips $label — a default-ignorable the denylist + \\p{C} both miss", ({ codePoint }) => {
      const ch = String.fromCodePoint(codePoint);
      // Prove the regex distinction: \p{C} alone would NOT catch this.
      expect(/\p{C}/u.test(ch)).toBe(false);
      expect(/\p{Default_Ignorable_Code_Point}/u.test(ch)).toBe(true);
      // Default policy keeps it; the moat strips it.
      expect(SanitizedString.fromUnknown(`a${ch}b`).value).toBe(`a${ch}b`);
      expect(SanitizedString.fromUnknown(`a${ch}b`, opts).value).toBe("ab");
    });

    it("never lets any default-ignorable code point through, except allowed whitespace", () => {
      fc.assert(
        fc.property(moatInterleavedStringArb, (input) => {
          const out = SanitizedString.fromUnknown(input, opts).value;
          // Tab/newline/carriage return match `\p{C}` but are intentionally
          // allowed; strip them before asserting the moat rejected the rest.
          // The live `\p{C}` + `\p{Default_Ignorable_Code_Point}` regex is an
          // independent cross-check against the frozen table the moat uses.
          const withoutAllowedWhitespace = out.replace(/[\t\n\r]/g, "");
          expect(/[\p{C}\p{Default_Ignorable_Code_Point}]/u.test(withoutAllowedWhitespace)).toBe(
            false,
          );
        }),
        { numRuns: MOAT_NUM_RUNS },
      );
    });

    it("re-composes neighbours exposed by stripping a default-ignorable joiner", () => {
      // The concrete MAJOR-1 case: U+034F COMBINING GRAPHEME JOINER sits
      // between `a` and U+0301 COMBINING ACUTE specifically to block their
      // composition. Stripping it must not leave a non-NFKC string behind.
      const once = SanitizedString.fromUnknown("a\u034f\u0301", opts).value;
      expect(once).toBe("\u00e1");
      expect(SanitizedString.fromUnknown(once, opts).value).toBe(once);
      // Devanagari: NA + VARIATION SELECTOR-1 + nukta composes to U+0929.
      const devanagari = SanitizedString.fromUnknown("\u0928\ufe00\u093c", opts).value;
      expect(devanagari).toBe("\u0929");
      expect(SanitizedString.fromUnknown(devanagari, opts).value).toBe(devanagari);
    });

    it("returns empty text for empty and all-disallowed input", () => {
      // Edge cases the interleave arbitraries never generate (they always mix
      // in a sanitizable chunk): empty input, and input that is *entirely*
      // moat-disallowed must sanitize to "" \u2014 not throw, not pass anything
      // through.
      expect(SanitizedString.fromUnknown("", opts).value).toBe("");
      expect(SanitizedString.fromUnknown("\u00ad\u200e\ufeff\u034f", opts).value).toBe("");
      expect(SanitizedString.fromUnknown(String.fromCodePoint(0xf0000, 0xe0100), opts).value).toBe(
        "",
      );
    });
  });

  describe("policy validation", () => {
    it("throws on an unknown policy string instead of silently using the default", () => {
      // The security-critical failure mode: an untyped caller passes a typo
      // ("allow-list") and silently gets the weaker default denylist. The
      // sanitizer must fail closed.
      expect(() =>
        SanitizedString.fromUnknown("x", { policy: "allow-list" as SanitizedStringPolicy }),
      ).toThrow(/unknown policy "allow-list"/);
      expect(() =>
        SanitizedString.fromUnknown("x", { policy: "" as SanitizedStringPolicy }),
      ).toThrow(/unknown policy/);
    });

    it("throws on a non-object options argument instead of silently using the default", () => {
      // An untyped caller that passes the policy positionally (a bare
      // "allowlist" string, or an array) has `.policy` read as `undefined`
      // and would silently get the weaker default denylist. Fail closed.
      // `null` is the easy-to-miss case: `typeof null === "object"`, so a
      // loose object test would let it through. Keep it in the table.
      for (const malformed of ["allowlist", [], null, 42, true]) {
        expect(() =>
          SanitizedString.fromUnknown("x", malformed as unknown as SanitizedStringOptions),
        ).toThrow(/options must be a plain object/);
      }
    });

    it("throws on non-plain-object or prototype-smuggled options instead of downgrading", () => {
      // A Map / class instance / Object.create(...) carrier passes a loose
      // `typeof === "object"` test but is not a plain object; its `.policy`
      // reads as undefined (silent default) or is reachable only through the
      // prototype chain. Require an Object.prototype/null prototype.
      const mapOptions = new Map([["policy", "allowlist"]]);
      expect(() =>
        SanitizedString.fromUnknown("x", mapOptions as unknown as SanitizedStringOptions),
      ).toThrow(/options must be a plain object/);
      const inheritedPolicy = Object.create({ policy: "allowlist" }) as SanitizedStringOptions;
      expect(() => SanitizedString.fromUnknown("x", inheritedPolicy)).toThrow(
        /options must be a plain object/,
      );
    });

    it("throws on an accessor `policy` instead of running caller code", () => {
      // An accessor `policy` would execute arbitrary caller code before
      // sanitization runs. Resolve `policy` only as an own data property.
      const accessorOptions = {} as SanitizedStringOptions;
      Object.defineProperty(accessorOptions, "policy", {
        get() {
          return "allowlist";
        },
        enumerable: true,
        configurable: true,
      });
      expect(() => SanitizedString.fromUnknown("x", accessorOptions)).toThrow(
        /policy must be an own data property/,
      );
    });

    it("accepts a null-prototype options object with an own data policy", () => {
      // A null-prototype plain object has no prototype-smuggling surface, so
      // it is a valid carrier; an own data `policy` resolves normally.
      const nullProtoOptions = Object.create(null) as SanitizedStringOptions;
      Object.defineProperty(nullProtoOptions, "policy", {
        value: "allowlist",
        enumerable: true,
      });
      expect(SanitizedString.fromUnknown("a\u00adb", nullProtoOptions).value).toBe("ab");
    });

    it("throws on a non-string policy instead of silently using the default", () => {
      expect(() =>
        SanitizedString.fromUnknown("x", { policy: 1 as unknown as SanitizedStringPolicy }),
      ).toThrow(/policy must be a string/);
    });

    it("accepts every declared policy without throwing", () => {
      const policies: readonly SanitizedStringPolicy[] = [
        "strip-zero-width",
        "allow-zwj",
        "allowlist",
      ];
      for (const policy of policies) {
        expect(SanitizedString.fromUnknown("ok", { policy }).value).toBe("ok");
      }
      // Absent policy → default, no throw.
      expect(SanitizedString.fromUnknown("ok").value).toBe("ok");
    });
  });

  it.each([
    { left: "e\u0301", right: "\u00e9", expected: "\u00e9" },
    { left: "\u212b", right: "\u00c5", expected: "\u00c5" },
    { left: "\uff21\uff22\uff23", right: "ABC", expected: "ABC" },
    { left: "\u2460", right: "1", expected: "1" },
    { left: "\ufb01", right: "fi", expected: "fi" },
  ])("normalizes NFKC-equivalent forms to $expected", ({ left, right, expected }) => {
    expect(SanitizedString.fromUnknown(left).value).toBe(expected);
    expect(SanitizedString.fromUnknown(right).value).toBe(expected);
  });

  it("freezes the returned instance", () => {
    fc.assert(
      fc.property(sanitizableStringArb, (input) => {
        const result = SanitizedString.fromUnknown(input);
        const original = result.value;
        expect(Object.isFrozen(result)).toBe(true);

        try {
          Object.assign(result, { value: "mutated" });
        } catch (error) {
          expect(error).toBeInstanceOf(TypeError);
        }

        expect(result.value).toBe(original);
      }),
      { numRuns: MOAT_NUM_RUNS },
    );
  });

  it("exposes length and toString for the sanitized value", () => {
    const result = SanitizedString.fromUnknown("e\u0301\u200b");

    expect(result.value).toBe("\u00e9");
    expect(result.length).toBe(1);
    expect(result.toString()).toBe("\u00e9");
  });

  it("keeps JSON object output parseable with the same normalized logical structure", () => {
    fc.assert(
      fc.property(jsonObjectArb, (input) => {
        const result = SanitizedString.fromUnknown(input).value;
        const parsed = JSON.parse(result) as JsonValue;
        const expected = expectedSanitizeJsonValue(projectJson(input));

        expect(typeof result).toBe("string");
        expect(parsed).toEqual(expected);
      }),
      { numRuns: JSON_NUM_RUNS },
    );
  });

  it("keeps JSON object output parseable under the allowlist policy", () => {
    // `moatJsonObjectArb` injects moat-disallowed code points into every key
    // and value, so this property actually exercises moat key/value stripping
    // — `fc.jsonValue()` keys are printable ASCII only and never would. The
    // arbitrary already re-filters with `canExpectedRoundTripJson(_,
    // "allowlist")`, so any input whose keys only collide *after* the moat
    // strips them (e.g. `{ "x\u00ady": 1, "xy": 2 }`) is excluded here and
    // covered instead by the explicit collision test below.
    fc.assert(
      fc.property(moatJsonObjectArb, (input) => {
        const result = SanitizedString.fromUnknown(input, { policy: "allowlist" }).value;
        const parsed = JSON.parse(result) as JsonValue;
        const expected = expectedSanitizeJsonValue(projectJson(input), "allowlist");

        expect(typeof result).toBe("string");
        expect(parsed).toEqual(expected);
      }),
      { numRuns: JSON_NUM_RUNS },
    );
  });

  it("throws on a moat-induced JSON key collision under the allowlist policy", () => {
    // Two keys distinct under the default denylist collapse to the same key
    // once the moat strips the soft hyphen. The sanitizer must reject the
    // ambiguous object rather than silently drop a field.
    const collision = JSON.parse('{"x\u00ady": 1, "xy": 2}') as JsonValue;
    expect(() => SanitizedString.fromUnknown(collision, { policy: "allowlist" })).toThrow(
      /sanitized key collision/,
    );
    // The same object is fine under the default policy — U+00AD is kept, so
    // the two keys stay distinct.
    expect(() => SanitizedString.fromUnknown(collision)).not.toThrow();
  });

  it("coerces null and undefined to empty text", () => {
    expect(SanitizedString.fromUnknown(null).value).toBe("");
    expect(SanitizedString.fromUnknown(undefined).value).toBe("");
  });

  it("rejects lone surrogate code units", () => {
    expect(() => SanitizedString.fromUnknown("\ud800")).toThrow(/lone surrogate/);
    expect(() => SanitizedString.fromUnknown("\ud800x")).toThrow(/lone surrogate/);
    expect(() => SanitizedString.fromUnknown("\udc00")).toThrow(/lone surrogate/);
  });

  it("rejects lone surrogate code units under the allowlist policy too", () => {
    // `Cs` (surrogate) is in the moat's rejected set, but production rejects
    // lone surrogates before the policy branch ever runs. Pin that the
    // high-stakes path is no weaker than the default one.
    const opts = { policy: "allowlist" as const };
    expect(() => SanitizedString.fromUnknown("\ud800", opts)).toThrow(/lone surrogate/);
    expect(() => SanitizedString.fromUnknown("\ud800x", opts)).toThrow(/lone surrogate/);
    expect(() => SanitizedString.fromUnknown("\udc00", opts)).toThrow(/lone surrogate/);
  });

  it.each([
    {
      name: "script markup passes through for downstream HTML encoding",
      input: "<script>alert(1)</script>",
      expected: "<script>alert(1)</script>",
    },
    { name: "bidi override is stripped", input: "\u202ehello", expected: "hello" },
    { name: "zero-width run is stripped", input: "a\u200b\u200c\u200dbc", expected: "abc" },
    { name: "backspace control is stripped", input: "\u0008backspace", expected: "backspace" },
    { name: "BOM is stripped", input: "safe\ufeffstring", expected: "safestring" },
    {
      name: "language tag is stripped",
      input: "\u{e0000}1invisible-tag",
      expected: "1invisible-tag",
    },
    { name: "normal text passes unchanged", input: "normal-text", expected: "normal-text" },
    {
      name: "emoji and decomposed accent normalize safely",
      input: "emoji \u{1f389} with combiner e\u0301",
      expected: "emoji \u{1f389} with combiner \u00e9",
    },
  ])("neutralizes bypass corpus case: $name", ({ input, expected }) => {
    const result = SanitizedString.fromUnknown(input).value;
    expect(result).toBe(expected);
    expect(containsBidiOverride(result)).toBe(false);
    expect(containsDisallowedC0(result)).toBe(false);
    expect(containsInvisibleTag(result)).toBe(false);
    expect(containsZeroWidth(result)).toBe(false);
  });

  it("rejects __proto__ injection so the prototype is never mutated", () => {
    const value = JSON.parse('{"__proto__":{"polluted":"yes"},"safe":"ok"}') as object;
    expect(() => SanitizedString.fromUnknown(value)).toThrow(/forbidden key/);
  });

  it("rejects constructor and prototype keys", () => {
    expect(() => SanitizedString.fromUnknown({ constructor: 1 })).toThrow(/forbidden key/);
    expect(() => SanitizedString.fromUnknown({ prototype: 1 })).toThrow(/forbidden key/);
  });

  it("rejects NFKC key collisions", () => {
    // U+FB01 is the "fi" ligature; NFKC normalizes it to "fi" — collides with
    // the explicit "fi" key.
    const colliding = JSON.parse('{"\\uFB01":1,"fi":2}') as object;
    expect(() => SanitizedString.fromUnknown(colliding)).toThrow(/collision/);
  });

  it("serializes sanitized arrays through descriptor-checked projection", () => {
    const result = SanitizedString.fromUnknown(["a\u200bb", null, true, 7]).value;

    expect(JSON.parse(result) as unknown).toEqual(["ab", null, true, 7]);
  });

  it("rejects symbol input", () => {
    expect(() => SanitizedString.fromUnknown(Symbol("leak"))).toThrow(/symbol/);
  });

  it("rejects function input", () => {
    expect(() => SanitizedString.fromUnknown(() => "leak")).toThrow(/function/);
  });

  it("rejects bigint input", () => {
    expect(() => SanitizedString.fromUnknown(1n)).toThrow(/bigint/);
  });

  it("rejects non-finite numbers inside object graphs", () => {
    expect(() => SanitizedString.fromUnknown({ value: Number.NaN })).toThrow(/non-finite number/);
    expect(() => SanitizedString.fromUnknown([Number.POSITIVE_INFINITY])).toThrow(
      /non-finite number/,
    );
  });

  it("rejects depth beyond MAX_DEPTH", () => {
    let nested: unknown = "leaf";
    for (let i = 0; i < 100; i++) {
      nested = { next: nested };
    }
    expect(() => SanitizedString.fromUnknown(nested)).toThrow(/depth/);
  });

  it.each([
    {
      name: "accessor property",
      input: {
        get foo(): string {
          return "bar";
        },
      },
    },
    {
      name: "toJSON method",
      input: {
        toJSON(): string {
          return "spoofed";
        },
      },
    },
    {
      name: "nested toJSON method",
      input: {
        outer: {
          toJSON(): string {
            return "spoofed";
          },
        },
      },
    },
    { name: "Date instance", input: new Date() },
    { name: "Map instance", input: new Map([["a", 1]]) },
    { name: "Set instance", input: new Set([1, 2]) },
    { name: "RegExp instance", input: /foo/ },
    { name: "typed array", input: new Uint8Array([1, 2]) },
    { name: "symbol-keyed property", input: { [Symbol("k")]: "v", legit: "ok" } },
    {
      name: "class instance",
      input: new (class Foo {
        readonly x = 1;
      })(),
    },
    {
      name: "non-enumerable property",
      input: (() => {
        const object: Record<string, unknown> = {};
        Object.defineProperty(object, "x", { value: 1, enumerable: false });
        return object;
      })(),
    },
    { name: "inherited property", input: Object.create({ inherited: 1 }) as object },
    { name: "undefined value", input: { x: undefined } },
    { name: "function value", input: { x: () => "leak" } },
    { name: "symbol value", input: { x: Symbol("leak") } },
    { name: "bigint value", input: { x: 1n } },
  ])("rejects unsafe object graph shape: $name", ({ input }) => {
    expect(() => SanitizedString.fromUnknown(input)).toThrow();
  });

  it("rejects accessors without invoking them", () => {
    let fired = false;
    const input = {
      get x(): number {
        fired = true;
        return 1;
      },
    };

    expect(() => SanitizedString.fromUnknown(input)).toThrow();
    expect(fired).toBe(false);
  });

  it("rejects array accessors without invoking them", () => {
    const input: unknown[] = [];
    let fired = false;
    Object.defineProperty(input, "0", {
      enumerable: true,
      get() {
        fired = true;
        return "leak";
      },
    });

    expect(() => SanitizedString.fromUnknown(input)).toThrow(/accessor property/);
    expect(fired).toBe(false);
  });

  it("rejects accessor toJSON descriptors without invoking them", () => {
    let fired = false;
    const input: Record<string, unknown> = {};
    Object.defineProperty(input, "toJSON", {
      enumerable: true,
      get() {
        fired = true;
        return () => "spoofed";
      },
    });

    expect(() => SanitizedString.fromUnknown(input)).toThrow(/accessor toJSON/);
    expect(fired).toBe(false);
  });

  it.each([
    {
      name: "sparse array hole",
      input: () => new Array<unknown>(1),
      message: /sparse array hole/,
    },
    {
      name: "array side property",
      input: () => {
        const value: unknown[] = ["x"];
        Object.defineProperty(value, "extra", { value: "y", enumerable: true });
        return value;
      },
      message: /non-index array property/,
    },
    {
      name: "array symbol key",
      input: () => {
        const value: unknown[] = ["x"];
        Object.defineProperty(value, Symbol("x"), { value: "y", enumerable: true });
        return value;
      },
      message: /symbol keys/,
    },
    {
      name: "non-enumerable array index",
      input: () => {
        const value: unknown[] = ["x"];
        Object.defineProperty(value, "0", { value: "x", enumerable: false });
        return value;
      },
      message: /non-enumerable own property/,
    },
    {
      name: "uint32 boundary array property",
      input: () => {
        const value: unknown[] = [];
        Object.defineProperty(value, "4294967295", { value: "x", enumerable: true });
        return value;
      },
      message: /non-index array property/,
    },
    {
      name: "non-plain array prototype",
      input: () => {
        const value: unknown[] = [];
        Object.setPrototypeOf(value, null);
        return value;
      },
      message: /non-plain array/,
    },
  ])("rejects unsafe array graph shape: $name", ({ input, message }) => {
    expect(() => SanitizedString.fromUnknown(input())).toThrow(message);
  });

  it("rejects enumerable properties inherited from Object.prototype", () => {
    const inheritedKey = "__sanitizedStringEnumerableTest";
    Object.defineProperty(Object.prototype, inheritedKey, {
      value: "leak",
      enumerable: true,
      configurable: true,
      writable: true,
    });
    try {
      expect(() => SanitizedString.fromUnknown({ ok: true })).toThrow(
        /inherited enumerable property/,
      );
    } finally {
      Reflect.deleteProperty(Object.prototype, inheritedKey);
    }
  });

  it("rejects circular object references", () => {
    const input: { self?: unknown } = {};
    input.self = input;

    expect(() => SanitizedString.fromUnknown(input)).toThrow(/circular reference/);
  });

  it("rejects circular array references", () => {
    const input: unknown[] = [];
    input.push(input);

    expect(() => SanitizedString.fromUnknown(input)).toThrow(/circular reference/);
  });
});

describe("frozen moat table", () => {
  // The moat classifies against a frozen range table, not the runtime's live
  // `\p{...}` data, so the classification boundary is the same on every
  // Node/Bun/ICU version. (NFKC normalization is still runtime-coupled — see
  // the LIMITATION note in sanitized-string.ts.) These tests pin the embedded
  // table to the cross-language wire artifact testdata/moat-disallowed-table.json
  // (which the Go reference verifier-reference.go independently checks).

  it("matches the cross-language testdata artifact", () => {
    expect(MOAT_UNICODE_VERSION).toBe(moatTableJson.unicodeVersion);
    expect(MOAT_DISALLOWED_RANGES).toEqual(moatTableJson.disallowedRanges);
  });

  it("is sorted, non-overlapping, and non-adjacent", () => {
    // A malformed table silently breaks the binary search the moat relies on.
    // Guard against a vacuous pass on an empty table — the loop body would
    // never run and every assertion below would be skipped.
    expect(MOAT_DISALLOWED_RANGES.length).toBeGreaterThan(0);
    let previousEnd = -2;
    for (const [start, end] of MOAT_DISALLOWED_RANGES) {
      expect(start).toBeLessThanOrEqual(end);
      expect(start).toBeGreaterThan(previousEnd + 1);
      previousEnd = end;
    }
  });

  it("classifies every curated vector as the artifact expects", () => {
    // The vectors encode human-verified expectations across Cc/Cf/Cn/Co/Cs and
    // default-ignorables; the Go reference asserts the same set independently.
    // Guard against a vacuous pass on an empty vector array.
    expect(moatTableJson.classificationVectors.length).toBeGreaterThan(0);
    for (const vector of moatTableJson.classificationVectors) {
      expect(isMoatDisallowedCodePoint(vector.codePoint)).toBe(vector.disallowed);
    }
  });

  // Belt-and-suspenders: on a runtime whose Unicode version matches the pinned
  // table, the frozen ranges must equal the live `\p{C}` +
  // `\p{Default_Ignorable_Code_Point}` union — this catches a stale table
  // after an intentional Unicode bump that forgot to regenerate. The frozen
  // table legitimately differs from a *newer* runtime's live data (that is the
  // whole point of freezing), so the check is genuinely inapplicable off the
  // pinned version. `skipIf` records it as an explicit, visible skip rather
  // than a silent always-pass — a reviewer can see it did not run and why.
  it.skipIf(RUNTIME_UNICODE_VERSION !== MOAT_UNICODE_VERSION)(
    `agrees with the live Unicode ${MOAT_UNICODE_VERSION} property data`,
    () => {
      const live = /[\p{C}\p{Default_Ignorable_Code_Point}]/u;
      for (let cp = 0; cp <= 0x10ffff; cp++) {
        if (isMoatDisallowedCodePoint(cp) !== live.test(String.fromCodePoint(cp))) {
          throw new Error(`frozen table disagrees with live \\p{...} at U+${cp.toString(16)}`);
        }
      }
    },
  );
});

function canExpectedSanitizeText(input: string): boolean {
  try {
    expectedSanitizeText(input);
    return true;
  } catch {
    return false;
  }
}

function canExpectedRoundTripJson(
  input: JsonRecord,
  policy: SanitizedStringPolicy = "strip-zero-width",
): boolean {
  try {
    expectedSanitizeJsonValue(projectJson(input), policy);
    return true;
  } catch {
    return false;
  }
}

function projectJson(input: JsonRecord): JsonValue {
  const serialized = JSON.stringify(input);
  if (serialized === undefined) {
    throw new Error("expected JSON object to serialize");
  }
  return JSON.parse(serialized) as JsonValue;
}

const FORBIDDEN_JSON_KEYS = new Set(["__proto__", "constructor", "prototype"]);

function expectedSanitizeJsonValue(
  value: JsonValue,
  policy: SanitizedStringPolicy = "strip-zero-width",
): JsonValue {
  if (typeof value === "string") {
    return expectedSanitizeText(value, policy);
  }

  if (value === null || typeof value === "number" || typeof value === "boolean") {
    return value;
  }

  if (Array.isArray(value)) {
    return value.map((item) => expectedSanitizeJsonValue(item, policy));
  }

  const out: Record<string, JsonValue> = Object.create(null) as Record<string, JsonValue>;
  for (const [key, child] of Object.entries(value)) {
    const sanitizedKey = expectedSanitizeText(key, policy);
    if (FORBIDDEN_JSON_KEYS.has(sanitizedKey)) {
      throw new Error(`forbidden key "${sanitizedKey}"`);
    }
    if (Object.hasOwn(out, sanitizedKey)) {
      throw new Error(`collision on "${sanitizedKey}"`);
    }
    out[sanitizedKey] = expectedSanitizeJsonValue(child, policy);
  }
  return out;
}

function expectedSanitizeText(
  input: string,
  policy: SanitizedStringPolicy = "strip-zero-width",
): string {
  // Reject lone surrogates on the raw input, before NFKC — production does it
  // in this order, and the oracle must mirror production so it can never mask
  // an order-sensitive bug.
  rejectLoneSurrogates(input);
  const normalized = input.normalize("NFKC");
  let out = "";
  for (let i = 0; i < normalized.length; ) {
    const codePoint = normalized.codePointAt(i);
    if (codePoint === undefined) {
      break;
    }
    if (!isExpectedDisallowedCodePoint(codePoint, policy)) {
      out += String.fromCodePoint(codePoint);
    }
    i += codePoint > 0xffff ? 2 : 1;
  }
  // Re-normalize after stripping — mirrors production's second NFKC pass that
  // makes the moat idempotent and its output NFKC-stable.
  return out.normalize("NFKC");
}

function containsBidiOverride(value: string): boolean {
  return containsCodePoint(
    value,
    (codePoint) =>
      (codePoint >= 0x202a && codePoint <= 0x202e) || (codePoint >= 0x2066 && codePoint <= 0x2069),
  );
}

function containsDisallowedC0(value: string): boolean {
  return containsCodePoint(
    value,
    (codePoint) =>
      codePoint <= 0x1f && codePoint !== 0x09 && codePoint !== 0x0a && codePoint !== 0x0d,
  );
}

function containsInvisibleTag(value: string): boolean {
  return containsCodePoint(value, (codePoint) => codePoint >= 0xe0000 && codePoint <= 0xe007f);
}

function containsFormatInvisible(value: string): boolean {
  return FORMAT_INVISIBLE_CHARS.some((char) => value.includes(char));
}

function containsZeroWidth(value: string): boolean {
  return containsCodePoint(
    value,
    (codePoint) =>
      codePoint === 0x200b || codePoint === 0x200c || codePoint === 0x200d || codePoint === 0xfeff,
  );
}

function isExpectedDisallowedCodePoint(codePoint: number, policy: SanitizedStringPolicy): boolean {
  if (containsDisallowedC0(String.fromCodePoint(codePoint))) {
    return true;
  }

  if (
    containsBidiOverride(String.fromCodePoint(codePoint)) ||
    containsInvisibleTag(String.fromCodePoint(codePoint)) ||
    codePoint === 0xfeff
  ) {
    return true;
  }

  if (codePoint === 0x200b || codePoint === 0x200c) {
    return true;
  }

  if (codePoint === 0x200d) {
    return policy !== "allow-zwj";
  }

  // Mirrors production: U+180E + U+2060..U+2064 invisible format chars.
  if (codePoint === 0x180e || (codePoint >= 0x2060 && codePoint <= 0x2064)) {
    return true;
  }

  // Mirrors production's `allowlist` (moat) branch: strip every `C*` AND
  // every Default_Ignorable_Code_Point, except the intentionally-allowed
  // whitespace controls (tab/newline/carriage return). Classifies against the
  // same frozen table production uses; the cross-language Go reference in
  // verifier-reference.go is the independent oracle for that table.
  if (
    policy === "allowlist" &&
    codePoint !== 0x09 &&
    codePoint !== 0x0a &&
    codePoint !== 0x0d &&
    isMoatDisallowedCodePoint(codePoint)
  ) {
    return true;
  }

  return false;
}

function containsCodePoint(value: string, predicate: (codePoint: number) => boolean): boolean {
  for (let i = 0; i < value.length; ) {
    const codePoint = value.codePointAt(i);
    if (codePoint === undefined) {
      return false;
    }
    if (predicate(codePoint)) {
      return true;
    }
    i += codePoint > 0xffff ? 2 : 1;
  }
  return false;
}

function rejectLoneSurrogates(value: string): void {
  for (let i = 0; i < value.length; i++) {
    const code = value.charCodeAt(i);
    if (code >= 0xd800 && code <= 0xdbff) {
      if (i + 1 >= value.length) {
        throw new Error("lone surrogate");
      }
      const next = value.charCodeAt(i + 1);
      if (next < 0xdc00 || next > 0xdfff) {
        throw new Error("lone surrogate");
      }
      i += 1;
      continue;
    }

    if (code >= 0xdc00 && code <= 0xdfff) {
      throw new Error("lone surrogate");
    }
  }
}

function flatNullObject(properties: number): Record<string, null> {
  const value: Record<string, null> = Object.create(null) as Record<string, null>;
  for (let i = 0; i < properties; i++) {
    value[`k${i}`] = null;
  }
  return value;
}

function isJsonRecord(value: unknown): value is JsonRecord {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
