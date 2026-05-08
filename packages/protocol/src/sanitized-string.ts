export type SanitizedStringPolicy = "strip-zero-width" | "allow-zwj";

export interface SanitizedStringOptions {
  readonly policy?: SanitizedStringPolicy | undefined;
}

export class SanitizedString {
  private constructor(readonly value: string) {}

  static fromUnknown(input: unknown, options: SanitizedStringOptions = {}): SanitizedString {
    const value = sanitizeText(coerceToString(input, options), options);
    const sanitized = new SanitizedString(value);
    Object.freeze(sanitized);
    return sanitized;
  }

  get length(): number {
    return this.value.length;
  }

  toString(): string {
    return this.value;
  }
}

function coerceToString(input: unknown, options: SanitizedStringOptions): string {
  // Coercion is explicit at the renderer boundary: strings are used as-is;
  // numbers, bigints, booleans, symbols, and functions use JS String();
  // null/undefined become empty text so absent optional fields do not render as
  // "null" or "undefined"; objects are first projected through JSON.stringify.
  // JSON object content is sanitized before the final stringify so NFKC cannot
  // turn string content into invalid JSON syntax.
  if (input === null || input === undefined) {
    return "";
  }

  switch (typeof input) {
    case "string":
      return input;
    case "number":
    case "bigint":
    case "boolean":
    case "symbol":
    case "function":
      return String(input);
    case "object":
      return stringifySanitizedJson(input, options);
  }

  return String(input);
}

function stringifySanitizedJson(input: object, options: SanitizedStringOptions): string {
  const serialized = JSON.stringify(input);
  if (serialized === undefined) {
    return "";
  }

  const projected = JSON.parse(serialized) as unknown;
  const sanitizedProjection = sanitizeJsonValue(projected, options);
  const sanitizedSerialized = JSON.stringify(sanitizedProjection);
  return sanitizedSerialized ?? "";
}

function sanitizeJsonValue(value: unknown, options: SanitizedStringOptions): unknown {
  if (typeof value === "string") {
    return sanitizeText(value, options);
  }

  if (value === null || typeof value === "number" || typeof value === "boolean") {
    return value;
  }

  if (Array.isArray(value)) {
    return value.map((item) => sanitizeJsonValue(item, options));
  }

  if (typeof value === "object") {
    const out: Record<string, unknown> = {};
    for (const [key, child] of Object.entries(value as Record<string, unknown>)) {
      out[sanitizeText(key, options)] = sanitizeJsonValue(child, options);
    }
    return out;
  }

  return value;
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
