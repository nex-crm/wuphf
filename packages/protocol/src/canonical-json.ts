import canonicalize from "canonicalize";

const MAX_DEPTH = 64;
const FORBIDDEN_KEYS = new Set(["__proto__", "constructor", "prototype"]);

export type JsonPrimitive = null | boolean | number | string;
// `readonly` here is a consumer-ergonomic hint, not a runtime constraint —
// `canonicalJSON` does not require frozen inputs (the prototype walk in
// assertJcsValue rejects non-plain objects, but plain mutable objects are
// fine). The intent is to discourage callers from mutating values they've
// promised to canonicalize; the actual freezing happens at the FrozenArgs
// boundary, not here.
export type JsonValue =
  | JsonPrimitive
  | readonly JsonValue[]
  | { readonly [key: string]: JsonValue };

// canonicalJSON is the JCS (RFC 8785) freeze boundary. It rejects every input
// shape that would break hash invariance or silently lose data when handed to
// `canonicalize`: undefined / function / symbol / bigint, non-finite numbers,
// lone-surrogate strings, sparse array holes, non-plain objects (Map/Set/Date/
// custom classes), accessor or non-enumerable own properties, symbol keys.
// Plain objects from `Object.create(null)` are accepted.
export function canonicalJSON(input: unknown): string {
  assertJcsValue(input, "$", 0);
  const serialized = canonicalize(input);
  if (serialized === undefined) {
    // Defensive: assertJcsValue should have already rejected anything that
    // makes canonicalize return undefined.
    throw new Error("canonicalJSON: input is not representable in JSON");
  }
  return serialized;
}

export function assertJcsValue(value: unknown, path = "$", depth = 0): asserts value is JsonValue {
  if (depth > MAX_DEPTH) {
    throw new Error(`canonicalJSON: max recursion depth exceeded at ${path}`);
  }

  if (value === null) return;

  switch (typeof value) {
    case "boolean":
      return;
    case "number":
      if (!Number.isFinite(value)) {
        throw new Error(
          `canonicalJSON: non-finite number at ${path} (NaN/±Infinity not representable in JCS)`,
        );
      }
      return;
    case "string":
      assertNoLoneSurrogate(value, path);
      return;
    case "undefined":
      throw new Error(`canonicalJSON: undefined at ${path} is not representable in JSON`);
    case "function":
      throw new Error(`canonicalJSON: function at ${path} is not representable in JSON`);
    case "symbol":
      throw new Error(`canonicalJSON: symbol at ${path} is not representable in JSON`);
    case "bigint":
      throw new Error(`canonicalJSON: bigint at ${path} is not representable in JSON`);
  }

  if (Array.isArray(value)) {
    if (Object.getOwnPropertySymbols(value).length > 0) {
      throw new Error(`canonicalJSON: symbol keys are not representable at ${path}`);
    }
    const descriptors = Object.getOwnPropertyDescriptors(value);
    for (let i = 0; i < value.length; i++) {
      if (!hasOwn(descriptors, String(i))) {
        throw new Error(`canonicalJSON: sparse array hole at ${path}[${i}]`);
      }
    }
    for (const [key, descriptor] of Object.entries(descriptors)) {
      if (key === "length") {
        continue;
      }
      assertNoLoneSurrogate(key, `${path}.${key}`);
      assertAllowedPropertyKey(key, `${path}.${key}`);
      const index = parseArrayIndexKey(key);
      if (index === undefined) {
        throw new Error(`canonicalJSON: non-array-index own property at ${path}.${key}`);
      }
      if (!descriptor.enumerable) {
        throw new Error(`canonicalJSON: non-enumerable own property at ${path}[${index}]`);
      }
      if ("get" in descriptor || "set" in descriptor) {
        throw new Error(`canonicalJSON: accessor property at ${path}[${index}]`);
      }
      assertJcsValue(descriptor.value, `${path}[${index}]`, depth + 1);
    }
    return;
  }

  if (typeof value === "object") {
    const proto = Object.getPrototypeOf(value);
    if (proto !== Object.prototype && proto !== null) {
      throw new Error(`canonicalJSON: non-plain object at ${path} (got ${describeProto(proto)})`);
    }
    if (Object.getOwnPropertySymbols(value as object).length > 0) {
      throw new Error(`canonicalJSON: symbol keys are not representable at ${path}`);
    }
    const descriptors = Object.getOwnPropertyDescriptors(value as object);
    for (const [key, descriptor] of Object.entries(descriptors)) {
      assertNoLoneSurrogate(key, `${path}.${key}`);
      assertAllowedPropertyKey(key, `${path}.${key}`);
      if (!descriptor.enumerable) {
        throw new Error(`canonicalJSON: non-enumerable own property at ${path}.${key}`);
      }
      if ("get" in descriptor || "set" in descriptor) {
        throw new Error(`canonicalJSON: accessor property at ${path}.${key}`);
      }
      assertJcsValue(descriptor.value, `${path}.${key}`, depth + 1);
    }
    return;
  }

  // Should be unreachable given the typeof branches above.
  throw new Error(`canonicalJSON: ${typeof value} at ${path} is not representable in JSON`);
}

function assertAllowedPropertyKey(key: string, path: string): void {
  if (FORBIDDEN_KEYS.has(key)) {
    throw new Error(`canonicalJSON: forbidden key "${key}" at ${path}`);
  }
}

function assertNoLoneSurrogate(value: string, path: string): void {
  for (let i = 0; i < value.length; i++) {
    const code = value.charCodeAt(i);
    if (code >= 0xd800 && code <= 0xdbff) {
      if (i + 1 >= value.length) {
        throw new Error(`canonicalJSON: lone high surrogate at ${path}`);
      }
      const next = value.charCodeAt(i + 1);
      if (next < 0xdc00 || next > 0xdfff) {
        throw new Error(`canonicalJSON: lone high surrogate at ${path}`);
      }
      i += 1;
      continue;
    }
    if (code >= 0xdc00 && code <= 0xdfff) {
      throw new Error(`canonicalJSON: lone low surrogate at ${path}`);
    }
  }
}

function hasOwn(record: Readonly<Record<string, unknown>>, key: string): boolean {
  return Object.hasOwn(record, key);
}

function parseArrayIndexKey(key: string): number | undefined {
  const index = Number(key);
  if (!Number.isInteger(index) || index < 0 || index >= 2 ** 32 - 1) {
    return undefined;
  }
  return String(index) === key ? index : undefined;
}

function describeProto(proto: object): string {
  const ctor = (proto as { constructor?: { name?: string } }).constructor;
  return typeof ctor?.name === "string" && ctor.name.length > 0 ? ctor.name : "non-plain";
}
