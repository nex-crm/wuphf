export type SanitizedStringPolicy = "strip-zero-width" | "allow-zwj";

export interface SanitizedStringOptions {
  readonly policy?: SanitizedStringPolicy | undefined;
}

const MAX_DEPTH = 64;
const FORBIDDEN_KEYS: ReadonlySet<string> = new Set(["__proto__", "constructor", "prototype"]);

export class SanitizedString {
  private constructor(readonly value: string) {
    Object.freeze(this);
  }

  static fromUnknown(input: unknown, options: SanitizedStringOptions = {}): SanitizedString {
    const value = sanitizeText(coerceToString(input, options), options);
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
  // description and `String(fn)` leaks function source. Objects are projected
  // through JSON.stringify; their string fields are sanitized before re-
  // stringifying so NFKC cannot turn content into invalid JSON syntax.
  if (input === null || input === undefined) {
    return "";
  }

  switch (typeof input) {
    case "string":
      return input;
    case "number":
    case "bigint":
    case "boolean":
      return String(input);
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
  const serialized = JSON.stringify(input);
  if (serialized === undefined) {
    throw new Error("SanitizedString: input is not JSON-representable");
  }

  const projected = JSON.parse(serialized) as unknown;
  const sanitizedProjection = sanitizeJsonValue(projected, options, 0);
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
): unknown {
  if (depth > MAX_DEPTH) {
    throw new Error("SanitizedString: max recursion depth exceeded");
  }

  if (typeof value === "string") {
    return sanitizeText(value, options);
  }

  if (value === null || typeof value === "number" || typeof value === "boolean") {
    return value;
  }

  if (Array.isArray(value)) {
    return value.map((item) => sanitizeJsonValue(item, options, depth + 1));
  }

  if (typeof value === "object") {
    // Object.create(null) defeats the `__proto__` setter that lives on
    // Object.prototype. Without this, JSON.parse('{"__proto__": ...}') puts an
    // own property on the parsed value, and assigning it on a `{}` accumulator
    // mutates the accumulator's prototype.
    const out: Record<string, unknown> = Object.create(null) as Record<string, unknown>;
    for (const [rawKey, child] of Object.entries(value as Record<string, unknown>)) {
      const sanitizedKey = sanitizeText(rawKey, options);
      if (FORBIDDEN_KEYS.has(sanitizedKey)) {
        throw new Error(`SanitizedString: forbidden key "${sanitizedKey}"`);
      }
      if (Object.hasOwn(out, sanitizedKey)) {
        throw new Error(`SanitizedString: sanitized key collision on "${sanitizedKey}"`);
      }
      Object.defineProperty(out, sanitizedKey, {
        value: sanitizeJsonValue(child, options, depth + 1),
        enumerable: true,
        configurable: true,
        writable: true,
      });
    }
    return out;
  }

  // Numbers/strings/null are handled above; unknown typeofs (function, symbol,
  // bigint, undefined) shouldn't appear here because JSON.parse can't produce
  // them. Reject defensively rather than letting them fall through.
  throw new Error(`SanitizedString: unrepresentable JSON value of type ${typeof value}`);
}

function sanitizeText(input: string, options: SanitizedStringOptions): string {
  const normalized = input.normalize("NFKC");
  rejectLoneSurrogates(normalized);
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

  return codePoint === 0x200d && options.policy !== "allow-zwj";
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
