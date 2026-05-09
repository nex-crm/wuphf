export type SanitizedStringPolicy = "strip-zero-width" | "allow-zwj";

export interface SanitizedStringOptions {
  readonly policy?: SanitizedStringPolicy | undefined;
}

const MAX_DEPTH = 64;
const FORBIDDEN_KEYS: ReadonlySet<string> = new Set(["__proto__", "constructor", "prototype"]);

type SanitizedJsonPrimitive = null | boolean | number | string;
type SanitizedJsonValue = SanitizedJsonPrimitive | SanitizedJsonValue[] | SanitizedJsonRecord;
interface SanitizedJsonRecord {
  readonly [key: string]: SanitizedJsonValue;
}

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
  const sanitizedProjection = sanitizeJsonValue(input, options, 0, "$", new WeakSet<object>());
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
): SanitizedJsonValue {
  if (depth > MAX_DEPTH) {
    throw new Error("SanitizedString: max recursion depth exceeded");
  }

  if (value === null) {
    return null;
  }

  switch (typeof value) {
    case "string":
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
    return sanitizeJsonArray(value, options, depth, path, ancestors);
  }

  return sanitizeJsonObject(value, options, depth, path, ancestors);
}

function sanitizeJsonArray(
  value: readonly unknown[],
  options: SanitizedStringOptions,
  depth: number,
  path: string,
  ancestors: WeakSet<object>,
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
        value: sanitizeJsonValue(child, options, depth + 1, itemPath, ancestors),
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
    for (const rawKey of Reflect.ownKeys(value)) {
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
        value: sanitizeJsonValue(child, options, depth + 1, childPath, ancestors),
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

  if (codePoint === 0x200d) {
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
