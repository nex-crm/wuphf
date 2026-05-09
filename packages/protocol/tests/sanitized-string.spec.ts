import * as fc from "fast-check";
import { describe, expect, it } from "vitest";
import { MAX_SANITIZED_STRING_BYTES } from "../src/budgets.ts";
import { SanitizedString, type SanitizedStringPolicy } from "../src/sanitized-string.ts";

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

// `fc.string()` in fast-check 3.x generates from `fc.char()`, which is
// restricted to printable ASCII (U+0020..U+007E). For a sanitizer that has to
// handle Unicode adversarially, that's a coverage hole — the property tests
// would never see combining marks, NFKC-decomposable homoglyphs, RTL marks,
// or astral-plane code points. `fullUnicodeString` draws from the full BMP +
// astral planes; `canExpectedSanitizeText` filters out the inputs we already
// know the production sanitizer rejects (lone surrogates, etc.).
const sanitizableStringArb = fc.fullUnicodeString().filter(canExpectedSanitizeText);
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

function canExpectedRoundTripJson(input: JsonRecord): boolean {
  try {
    expectedSanitizeJsonValue(projectJson(input));
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

function isJsonRecord(value: unknown): value is JsonRecord {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
