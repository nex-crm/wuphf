import { Buffer } from "node:buffer";

import {
  MAX_SANITIZED_JSON_NODES,
  MAX_SANITIZED_STRING_BYTES,
  validateSanitizedJsonNodeBudget,
} from "./budgets.ts";

// SanitizedString policies trade off renderer permissiveness vs. moat width.
// Every policy STRIPS the offending code points after NFKC — it does not
// throw or reject the whole string. Only malformed surrogate input and
// unsafe object-graph shapes throw.
//
// - `strip-zero-width` (default) — denylist of known-weaponized invisible code
//   points (bidi overrides, ZWSP/ZWNJ/ZWJ, U+E0000 tag block, etc.). Anything
//   not on the denylist is kept. Use for general renderer text.
// - `allow-zwj` — same as `strip-zero-width` but preserves ZWJ (needed for
//   emoji sequences like 👨‍👩‍👧).
// - `allowlist` — moat model: on top of the existing denylist, strip every
//   Unicode `C*` code point (Cc, Cf, Cn, Co, Cs) AND every
//   Default_Ignorable_Code_Point (variation selectors, the Hangul fillers
//   U+115F/U+1160, U+034F CGJ, etc. — invisible code points that are not in
//   `C*` but are still weaponizable). Closes the broad "unassigned /
//   private-use / control / format / default-ignorable" injection class at
//   the cost of stripping characters legitimate text rarely contains. Use for
//   high-stakes writes (audit-chain payloads, signed receipts, anything the
//   v1 cosign path will sign).
//
// NOTE ON PROVENANCE: a `SanitizedString` instance does NOT record which
// policy produced it. A value sanitized under `allowlist` is structurally
// indistinguishable from one sanitized under `strip-zero-width`. Code that
// requires the moat invariant (e.g. the branch-12 cosign codec) MUST
// re-sanitize at its own trust boundary with an explicit `allowlist` policy
// rather than trusting a passed-in `SanitizedString`.
export type SanitizedStringPolicy = "strip-zero-width" | "allow-zwj" | "allowlist";

const SANITIZED_STRING_POLICIES: ReadonlySet<string> = new Set<SanitizedStringPolicy>([
  "strip-zero-width",
  "allow-zwj",
  "allowlist",
]);

export interface SanitizedStringOptions {
  readonly policy?: SanitizedStringPolicy | undefined;
}

// `SanitizedStringOptions` is a compile-time type only; an untyped JS caller
// (or a config/test fixture) can pass `{ policy: "allow-list" }` and — without
// this guard — silently get the weaker default denylist instead of the moat.
// For a security boundary that must fail closed, an unknown policy is an
// error, not a fall-through. Called once at the `fromUnknown` entry point.
function resolveSanitizedStringPolicy(options: SanitizedStringOptions): SanitizedStringPolicy {
  const policy = options.policy ?? "strip-zero-width";
  if (!SANITIZED_STRING_POLICIES.has(policy)) {
    throw new Error(
      `SanitizedString: unknown policy ${JSON.stringify(policy)} (expected one of ${[
        ...SANITIZED_STRING_POLICIES,
      ]
        .map((value) => JSON.stringify(value))
        .join(", ")})`,
    );
  }
  return policy;
}

const MAX_DEPTH = 64;
const FORBIDDEN_KEYS: ReadonlySet<string> = new Set(["__proto__", "constructor", "prototype"]);

type SanitizedJsonPrimitive = null | boolean | number | string;
type SanitizedJsonValue = SanitizedJsonPrimitive | SanitizedJsonValue[] | SanitizedJsonRecord;
interface SanitizedJsonRecord {
  readonly [key: string]: SanitizedJsonValue;
}
interface SanitizedJsonNodeCounter {
  count: number;
}

export class SanitizedString {
  private constructor(readonly value: string) {
    Object.freeze(this);
  }

  static fromUnknown(input: unknown, options: SanitizedStringOptions = {}): SanitizedString {
    // Fail closed on an unknown policy before any sanitization runs — a typo
    // on the security policy must not silently degrade to the default.
    const resolved: SanitizedStringOptions = {
      policy: resolveSanitizedStringPolicy(options),
    };
    if (typeof input === "string") {
      assertSanitizedStringByteBudget(input, "SanitizedString input bytes");
    }
    const coerced = coerceToString(input, resolved);
    if (typeof input === "object" && input !== null) {
      assertSanitizedStringByteBudget(coerced, "SanitizedString JSON projection bytes");
    }
    const value = sanitizeText(coerced, resolved);
    assertSanitizedStringByteBudget(value, "SanitizedString value bytes");
    return new SanitizedString(value);
  }

  get length(): number {
    return this.value.length;
  }

  toString(): string {
    return this.value;
  }
}

function coerceToString(input: unknown, options: SanitizedStringOptions): string {
  // Coercion is explicit at the renderer boundary. null/undefined become empty
  // text so absent optional fields do not render as "null" or "undefined".
  // Symbols and functions are rejected — `String(Symbol(x))` leaks the
  // description and `String(fn)` leaks function source. Objects are walked via
  // descriptors before serialization so getters and toJSON cannot run first.
  if (input === null || input === undefined) {
    return "";
  }

  switch (typeof input) {
    case "string":
      return input;
    case "number":
    case "boolean":
      return String(input);
    case "bigint":
      throw new Error("SanitizedString: bigint is not JSON-representable");
    case "symbol":
      throw new Error("SanitizedString: symbol is not representable");
    case "function":
      throw new Error("SanitizedString: function is not representable");
    case "object":
      return stringifySanitizedJson(input, options);
  }

  return String(input);
}

function stringifySanitizedJson(input: object, options: SanitizedStringOptions): string {
  const nodeCounter: SanitizedJsonNodeCounter = { count: 0 };
  const sanitizedProjection = sanitizeJsonValue(
    input,
    options,
    0,
    "$",
    new WeakSet<object>(),
    nodeCounter,
    false,
  );
  const sanitizedSerialized = JSON.stringify(sanitizedProjection);
  if (sanitizedSerialized === undefined) {
    throw new Error("SanitizedString: sanitized projection is not JSON-representable");
  }
  return sanitizedSerialized;
}

function sanitizeJsonValue(
  value: unknown,
  options: SanitizedStringOptions,
  depth: number,
  path: string,
  ancestors: WeakSet<object>,
  nodeCounter: SanitizedJsonNodeCounter,
  nodeAlreadyCounted: boolean,
): SanitizedJsonValue {
  if (!nodeAlreadyCounted) {
    countSanitizedJsonNode(path, nodeCounter);
  }

  if (depth > MAX_DEPTH) {
    throw new Error("SanitizedString: max recursion depth exceeded");
  }

  if (value === null) {
    return null;
  }

  switch (typeof value) {
    case "string":
      assertSanitizedStringByteBudget(value, `SanitizedString JSON string at ${path} bytes`);
      return sanitizeText(value, options);
    case "number":
      if (!Number.isFinite(value)) {
        throw new Error(`SanitizedString: non-finite number at ${path}`);
      }
      return value;
    case "boolean":
      return value;
    case "undefined":
      throw new Error(`SanitizedString: undefined at ${path} is not JSON-representable`);
    case "function":
      throw new Error(`SanitizedString: function at ${path} is not JSON-representable`);
    case "symbol":
      throw new Error(`SanitizedString: symbol at ${path} is not JSON-representable`);
    case "bigint":
      throw new Error(`SanitizedString: bigint at ${path} is not JSON-representable`);
  }

  if (ArrayBuffer.isView(value)) {
    throw new Error(`SanitizedString: typed array at ${path} is not JSON-representable`);
  }

  // ECMAScript has no side-effect-free Proxy test; reflective inspection can
  // invoke proxy traps. The no-getter/toJSON guarantee below applies to
  // ordinary arrays and objects.
  if (Array.isArray(value)) {
    return sanitizeJsonArray(value, options, depth, path, ancestors, nodeCounter);
  }

  return sanitizeJsonObject(value, options, depth, path, ancestors, nodeCounter);
}

function countSanitizedJsonNode(path: string, nodeCounter: SanitizedJsonNodeCounter): void {
  nodeCounter.count += 1;
  const budget = validateSanitizedJsonNodeBudget(nodeCounter.count, path);
  if (!budget.ok) {
    throw new Error(`SanitizedString: ${budget.reason}`);
  }
}

function reserveSanitizedJsonChildNodes(
  count: number,
  nodeCounter: SanitizedJsonNodeCounter,
  childPath: (index: number) => string,
): void {
  const nextCount = nodeCounter.count + count;
  if (nextCount > MAX_SANITIZED_JSON_NODES) {
    const firstRejectedIndex = MAX_SANITIZED_JSON_NODES - nodeCounter.count;
    const budget = validateSanitizedJsonNodeBudget(
      MAX_SANITIZED_JSON_NODES + 1,
      childPath(firstRejectedIndex),
    );
    if (!budget.ok) {
      throw new Error(`SanitizedString: ${budget.reason}`);
    }
  }
  nodeCounter.count = nextCount;
}

function assertSanitizedStringByteBudget(value: string, label: string): void {
  const bytes = Buffer.byteLength(value, "utf8");
  if (bytes > MAX_SANITIZED_STRING_BYTES) {
    throw new Error(
      `${label} exceeds MAX_SANITIZED_STRING_BYTES (got ${bytes}, max ${MAX_SANITIZED_STRING_BYTES})`,
    );
  }
}

function sanitizeJsonArray(
  value: readonly unknown[],
  options: SanitizedStringOptions,
  depth: number,
  path: string,
  ancestors: WeakSet<object>,
  nodeCounter: SanitizedJsonNodeCounter,
): SanitizedJsonValue[] {
  return withJsonAncestor(value, path, ancestors, () => {
    assertNoCallableToJson(value, path);
    const proto = Object.getPrototypeOf(value);
    if (proto !== Array.prototype) {
      throw new Error(`SanitizedString: non-plain array at ${path}`);
    }
    assertNoEnumerableInheritedProperties(value, path);

    const lengthDescriptor = Object.getOwnPropertyDescriptor(value, "length");
    if (lengthDescriptor === undefined || isAccessorDescriptor(lengthDescriptor)) {
      throw new Error(`SanitizedString: invalid array length descriptor at ${path}`);
    }
    const lengthValue: unknown = lengthDescriptor.value;
    if (typeof lengthValue !== "number" || !Number.isSafeInteger(lengthValue) || lengthValue < 0) {
      throw new Error(`SanitizedString: invalid array length at ${path}`);
    }
    const length = lengthValue;
    reserveSanitizedJsonChildNodes(length, nodeCounter, (index) => `${path}[${index}]`);

    for (const rawKey of Reflect.ownKeys(value)) {
      if (typeof rawKey === "symbol") {
        throw new Error(`SanitizedString: symbol keys are not JSON-representable at ${path}`);
      }
      if (rawKey === "length") {
        continue;
      }
      const index = parseArrayIndexKey(rawKey);
      if (index === undefined || index >= length) {
        throw new Error(`SanitizedString: non-index array property at ${path}.${rawKey}`);
      }
    }

    const out: SanitizedJsonValue[] = [];
    for (let index = 0; index < length; index++) {
      const itemPath = `${path}[${index}]`;
      const descriptor = Object.getOwnPropertyDescriptor(value, String(index));
      if (descriptor === undefined) {
        throw new Error(`SanitizedString: sparse array hole at ${itemPath}`);
      }
      assertEnumerableDataDescriptor(descriptor, itemPath);
      const child: unknown = descriptor.value;
      Object.defineProperty(out, String(index), {
        value: sanitizeJsonValue(child, options, depth + 1, itemPath, ancestors, nodeCounter, true),
        enumerable: true,
        configurable: true,
        writable: true,
      });
    }
    Object.setPrototypeOf(out, null);
    return out;
  });
}

function sanitizeJsonObject(
  value: object,
  options: SanitizedStringOptions,
  depth: number,
  path: string,
  ancestors: WeakSet<object>,
  nodeCounter: SanitizedJsonNodeCounter,
): SanitizedJsonRecord {
  return withJsonAncestor(value, path, ancestors, () => {
    assertNoCallableToJson(value, path);
    const proto = Object.getPrototypeOf(value);
    if (proto !== Object.prototype && proto !== null) {
      throw new Error(`SanitizedString: non-plain object at ${path}`);
    }
    assertNoEnumerableInheritedProperties(value, path);

    // Object.create(null) defeats the `__proto__` setter that lives on
    // Object.prototype. Without this, assigning a sanitized "__proto__" key on
    // a `{}` accumulator would mutate the accumulator's prototype.
    const out: Record<string, SanitizedJsonValue> = Object.create(null) as Record<
      string,
      SanitizedJsonValue
    >;
    const rawKeys = Reflect.ownKeys(value);
    reserveSanitizedJsonChildNodes(rawKeys.length, nodeCounter, (index) => {
      const rawKey = rawKeys[index];
      return `${path}.${typeof rawKey === "symbol" ? String(rawKey) : rawKey}`;
    });
    for (const rawKey of rawKeys) {
      if (typeof rawKey === "symbol") {
        throw new Error(`SanitizedString: symbol keys are not JSON-representable at ${path}`);
      }

      const childPath = `${path}.${rawKey}`;
      const descriptor = Object.getOwnPropertyDescriptor(value, rawKey);
      if (descriptor === undefined) {
        throw new Error(`SanitizedString: missing own property descriptor at ${childPath}`);
      }
      assertEnumerableDataDescriptor(descriptor, childPath);

      const sanitizedKey = sanitizeText(rawKey, options);
      if (FORBIDDEN_KEYS.has(sanitizedKey)) {
        throw new Error(`SanitizedString: forbidden key "${sanitizedKey}"`);
      }
      if (Object.hasOwn(out, sanitizedKey)) {
        throw new Error(`SanitizedString: sanitized key collision on "${sanitizedKey}"`);
      }

      const child: unknown = descriptor.value;
      Object.defineProperty(out, sanitizedKey, {
        value: sanitizeJsonValue(
          child,
          options,
          depth + 1,
          childPath,
          ancestors,
          nodeCounter,
          true,
        ),
        enumerable: true,
        configurable: true,
        writable: true,
      });
    }
    return out;
  });
}

function withJsonAncestor<T>(
  value: object,
  path: string,
  ancestors: WeakSet<object>,
  sanitize: () => T,
): T {
  if (ancestors.has(value)) {
    throw new Error(`SanitizedString: circular reference at ${path} is not JSON-representable`);
  }

  ancestors.add(value);
  try {
    return sanitize();
  } finally {
    ancestors.delete(value);
  }
}

function assertNoCallableToJson(value: object, path: string): void {
  let current: object | null = value;
  while (current !== null) {
    const descriptor = Object.getOwnPropertyDescriptor(current, "toJSON");
    if (descriptor !== undefined) {
      if (isAccessorDescriptor(descriptor)) {
        throw new Error(`SanitizedString: accessor toJSON at ${path}`);
      }
      const toJson: unknown = descriptor.value;
      if (typeof toJson === "function") {
        throw new Error(`SanitizedString: toJSON method at ${path} is not allowed`);
      }
    }
    current = Object.getPrototypeOf(current);
  }
}

function assertNoEnumerableInheritedProperties(value: object, path: string): void {
  let current = Object.getPrototypeOf(value);
  while (current !== null) {
    for (const rawKey of Reflect.ownKeys(current)) {
      const descriptor = Object.getOwnPropertyDescriptor(current, rawKey);
      if (descriptor?.enumerable === true) {
        const key = typeof rawKey === "symbol" ? rawKey.toString() : rawKey;
        throw new Error(`SanitizedString: inherited enumerable property at ${path}.${key}`);
      }
    }
    current = Object.getPrototypeOf(current);
  }
}

function assertEnumerableDataDescriptor(
  descriptor: PropertyDescriptor,
  path: string,
): asserts descriptor is PropertyDescriptor & { readonly value: unknown } {
  if (descriptor.enumerable !== true) {
    throw new Error(`SanitizedString: non-enumerable own property at ${path}`);
  }
  if (isAccessorDescriptor(descriptor)) {
    throw new Error(`SanitizedString: accessor property at ${path}`);
  }
}

function isAccessorDescriptor(descriptor: PropertyDescriptor): boolean {
  return "get" in descriptor || "set" in descriptor;
}

function parseArrayIndexKey(key: string): number | undefined {
  const index = Number(key);
  if (!Number.isInteger(index) || index < 0 || index >= 2 ** 32 - 1) {
    return undefined;
  }
  return String(index) === key ? index : undefined;
}

function sanitizeText(input: string, options: SanitizedStringOptions): string {
  rejectLoneSurrogates(input);
  const normalized = input.normalize("NFKC");
  let out = "";

  for (let i = 0; i < normalized.length; ) {
    const codePoint = normalized.codePointAt(i);
    if (codePoint === undefined) {
      break;
    }
    if (!isDisallowedCodePoint(codePoint, options)) {
      out += String.fromCodePoint(codePoint);
    }
    i += codePoint > 0xffff ? 2 : 1;
  }

  return out;
}

// Pre-compiled to avoid rebuilding on every code point under the allowlist
// policy. The moat rejects two overlapping-but-distinct Unicode sets:
//
//   \p{C}                          — Cc (control), Cf (format), Cn
//                                    (unassigned), Co (private use), Cs
//                                    (surrogate). Non-textual categories.
//   \p{Default_Ignorable_Code_Point} — code points Unicode marks as safe to
//                                    render as nothing: variation selectors
//                                    (U+FE00-0F, U+E0100-EF1EF), the Hangul
//                                    fillers U+115F/U+1160/U+3164, U+034F
//                                    COMBINING GRAPHEME JOINER, U+17B4/B5,
//                                    Mongolian variation selectors, etc.
//
// `\p{C}` alone misses the second set: U+115F is `Lo` (a letter!) and U+FE0F
// is `Mn` (a mark) — neither is `C*`, yet both are invisible and trivially
// weaponized for homograph / hidden-payload attacks. A signed write payload
// must contain neither.
//
// Exported (not via `index.ts`, so not public package API) so the test
// oracle can import the one true pattern instead of hand-copying it — a
// copied literal can silently drift from this one.
export const MOAT_DISALLOWED_RE = /[\p{C}\p{Default_Ignorable_Code_Point}]/u;

function isDisallowedCodePoint(codePoint: number, options: SanitizedStringOptions): boolean {
  if (codePoint <= 0x1f && codePoint !== 0x09 && codePoint !== 0x0a && codePoint !== 0x0d) {
    return true;
  }

  if (
    (codePoint >= 0x202a && codePoint <= 0x202e) ||
    (codePoint >= 0x2066 && codePoint <= 0x2069) ||
    (codePoint >= 0xe0000 && codePoint <= 0xe007f) ||
    codePoint === 0xfeff
  ) {
    return true;
  }

  if (codePoint === 0x200b || codePoint === 0x200c) {
    return true;
  }

  if (codePoint === 0x200d) {
    // ZWJ rules: `allow-zwj` keeps it, every other policy strips it. Under
    // `allowlist` ZWJ is rejected because it's Cf — the broad rule wins.
    return options.policy !== "allow-zwj";
  }

  // Other commonly-weaponized invisible format characters that NFKC does NOT
  // remove: U+180E MONGOLIAN VOWEL SEPARATOR, U+2060 WORD JOINER, and the
  // U+2061..U+2064 INVISIBLE OPERATORS. Per UTS #39 these are
  // Default_Ignorable / Restricted for identifier use; rejecting them at the
  // renderer boundary closes a homograph/spoofing path that the existing ZWSP
  // strip alone leaves open.
  if (codePoint === 0x180e || (codePoint >= 0x2060 && codePoint <= 0x2064)) {
    return true;
  }

  // Allowlist (moat) policy: strip every `C*` AND every
  // Default_Ignorable_Code_Point. Catches what the denylist above misses —
  // soft hyphen U+00AD, U+061C ARABIC LETTER MARK, the Hangul fillers
  // U+115F/U+1160, every variation selector, the full Cn/Co planes, every
  // bidi/format mark Unicode adds in the future. The denylist branches above
  // remain in effect for older policies; this branch only widens the
  // rejection set when the caller has opted into the stricter contract.
  //
  // Tab (U+0009), newline (U+000A), and carriage return (U+000D) are `Cc`
  // and so match `\p{C}`, but they are intentionally-allowed whitespace:
  // every other policy keeps them (see the C0 carve-out at the top of this
  // function). The moat must not silently flatten the line structure of a
  // signed multi-line payload, so they are carved out of the moat too.
  if (
    options.policy === "allowlist" &&
    codePoint !== 0x09 &&
    codePoint !== 0x0a &&
    codePoint !== 0x0d &&
    MOAT_DISALLOWED_RE.test(String.fromCodePoint(codePoint))
  ) {
    return true;
  }

  return false;
}

function rejectLoneSurrogates(value: string): void {
  for (let i = 0; i < value.length; i++) {
    const code = value.charCodeAt(i);
    if (code >= 0xd800 && code <= 0xdbff) {
      if (i + 1 >= value.length) {
        throw new Error("SanitizedString: lone surrogate code unit");
      }
      const next = value.charCodeAt(i + 1);
      if (next < 0xdc00 || next > 0xdfff) {
        throw new Error("SanitizedString: lone surrogate code unit");
      }
      i += 1;
      continue;
    }

    if (code >= 0xdc00 && code <= 0xdfff) {
      throw new Error("SanitizedString: lone surrogate code unit");
    }
  }
}
