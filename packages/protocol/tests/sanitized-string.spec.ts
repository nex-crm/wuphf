import * as fc from "fast-check";
import { describe, expect, it } from "vitest";
import { MAX_SANITIZED_JSON_NODES, MAX_SANITIZED_STRING_BYTES } from "../src/budgets.ts";
import {
  MOAT_DISALLOWED_RE,
  SanitizedString,
  type SanitizedStringOptions,
  type SanitizedStringPolicy,
} from "../src/sanitized-string.ts";

type JsonPrimitive = null | boolean | number | string;
type JsonValue = JsonPrimitive | JsonValue[] | JsonRecord;
interface JsonRecord {
  readonly [key: string]: JsonValue;
}

const MOAT_NUM_RUNS = 1000;
const JSON_NUM_RUNS = 1000;

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

    it("never produces output longer than input (allowlist is purely subtractive after NFKC)", () => {
      fc.assert(
        fc.property(sanitizableStringArb, (input) => {
          const out = SanitizedString.fromUnknown(input, opts).value;
          // NFKC can shrink (e.g. \ufb01 \u2192 fi is +1 char, but \ufb01 is itself one
          // code point); after normalization any additional reduction is
          // strictly removal. The right invariant is: output is a subsequence
          // of NFKC(input) \u2014 same order, same multiplicity, never inserts,
          // reorders, or duplicates. Per-character containment alone would
          // pass for a sanitizer that emitted "ba" or "aa" from "ab".
          const normalized = [...input.normalize("NFKC")];
          let cursor = 0;
          for (const ch of out) {
            while (cursor < normalized.length && normalized[cursor] !== ch) {
              cursor += 1;
            }
            expect(cursor).toBeLessThan(normalized.length);
            cursor += 1;
          }
        }),
        { numRuns: MOAT_NUM_RUNS },
      );
    });

    it("is idempotent under repeated allowlist sanitization", () => {
      fc.assert(
        fc.property(sanitizableStringArb, (input) => {
          const once = SanitizedString.fromUnknown(input, opts).value;
          const twice = SanitizedString.fromUnknown(once, opts).value;
          expect(twice).toBe(once);
        }),
        { numRuns: MOAT_NUM_RUNS },
      );
    });

    it("never lets a Unicode C* code point through, except allowed whitespace", () => {
      fc.assert(
        fc.property(sanitizableStringArb, (input) => {
          const out = SanitizedString.fromUnknown(input, opts).value;
          // No assigned format, unassigned, private-use, or surrogate code
          // points should survive. Tab/newline/carriage return are `Cc` but
          // intentionally-allowed whitespace, so strip them before asserting.
          // Lone surrogates are already rejected earlier.
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
        fc.property(sanitizableStringArb, (input) => {
          const out = SanitizedString.fromUnknown(input, opts).value;
          // Tab/newline/carriage return match `\p{C}` but are intentionally
          // allowed; strip them before asserting the moat rejected the rest.
          const withoutAllowedWhitespace = out.replace(/[\t\n\r]/g, "");
          expect(MOAT_DISALLOWED_RE.test(withoutAllowedWhitespace)).toBe(false);
        }),
        { numRuns: MOAT_NUM_RUNS },
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
      for (const malformed of ["allowlist", [], 42, true]) {
        expect(() =>
          SanitizedString.fromUnknown("x", malformed as unknown as SanitizedStringOptions),
        ).toThrow(/options must be a plain object/);
      }
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
    fc.assert(
      // `jsonObjectArb` is prefiltered with the default policy, so it still
      // admits objects that only become invalid under allowlist stripping
      // (e.g. `{ "­": 1, "": 2 }` — both keys sanitize to "" once the
      // moat strips the soft hyphen, a collision). Re-filter with the same
      // policy under test so the property only sees inputs the allowlist
      // contract can actually round-trip.
      fc.property(
        jsonObjectArb.filter((input) => canExpectedRoundTripJson(input, "allowlist")),
        (input) => {
          const result = SanitizedString.fromUnknown(input, { policy: "allowlist" }).value;
          const parsed = JSON.parse(result) as JsonValue;
          const expected = expectedSanitizeJsonValue(projectJson(input), "allowlist");

          expect(typeof result).toBe("string");
          expect(parsed).toEqual(expected);
        },
      ),
      { numRuns: JSON_NUM_RUNS },
    );
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
  const normalized = input.normalize("NFKC");
  rejectLoneSurrogates(normalized);
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
  return out;
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
  // whitespace controls (tab/newline/carriage return). Imports the production
  // `MOAT_DISALLOWED_RE` directly so the oracle cannot drift from the impl.
  if (
    policy === "allowlist" &&
    codePoint !== 0x09 &&
    codePoint !== 0x0a &&
    codePoint !== 0x0d &&
    MOAT_DISALLOWED_RE.test(String.fromCodePoint(codePoint))
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
