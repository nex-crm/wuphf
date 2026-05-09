import { assertWithinBudget, MAX_FROZEN_ARGS_BYTES } from "./budgets.ts";
import * as canonicalJson from "./canonical-json.ts";
import { type Sha256Hex, sha256Hex } from "./sha256.ts";

const MAX_FROZEN_ARGS_PREFLIGHT_DEPTH = 64;

// FrozenArgs is the RFC 8785 (JCS) freeze boundary for tool-call arguments.
// `freeze` rejects any input that would break JCS hash invariance — see
// canonical-json.ts for the full rejection list. The hash is derived from the
// canonical JSON string; `equals` compares hashes only because the constructor
// is private and `freeze` enforces `hash === sha256Hex(canonicalJson)`.
export class FrozenArgs {
  private constructor(
    readonly canonicalJson: string,
    readonly hash: Sha256Hex,
  ) {
    Object.freeze(this);
  }

  static freeze(input: unknown): FrozenArgs {
    assertFrozenArgsInputBudget(input);
    const canonical = canonicalJson.canonicalJSON(input);
    assertFrozenArgsCanonicalBudget(canonical);
    const hash = sha256Hex(canonical);
    return new FrozenArgs(canonical, hash);
  }

  static fromCanonical(canonicalJsonString: string): FrozenArgs {
    assertFrozenArgsCanonicalBudget(canonicalJsonString);
    let parsed: unknown;
    try {
      parsed = JSON.parse(canonicalJsonString);
    } catch (err) {
      throw new Error(
        `FrozenArgs.fromCanonical: input is not valid JSON${
          err instanceof Error ? ` (${err.message})` : ""
        }`,
      );
    }
    const reCanonical = canonicalJson.canonicalJSON(parsed);
    if (reCanonical !== canonicalJsonString) {
      throw new Error(
        "FrozenArgs.fromCanonical: input is not canonical-form (re-canonicalization differed)",
      );
    }
    const hash = sha256Hex(canonicalJsonString);
    return new FrozenArgs(canonicalJsonString, hash);
  }

  equals(other: FrozenArgs): boolean {
    return this.hash === other.hash;
  }
}

function assertFrozenArgsInputBudget(input: unknown): void {
  // Use the canonical budget as the input preflight budget so this batch does
  // not introduce a second public resource constant outside budgets.ts.
  const inputBytes = jsonByteLengthUpTo(input, MAX_FROZEN_ARGS_BYTES, "$", 0, new Set<object>());
  assertWithinBudget(inputBytes, MAX_FROZEN_ARGS_BYTES, "FrozenArgs input bytes");
}

function assertFrozenArgsCanonicalBudget(canonical: string): void {
  assertWithinBudget(
    utf8ByteLengthUpTo(canonical, MAX_FROZEN_ARGS_BYTES),
    MAX_FROZEN_ARGS_BYTES,
    "FrozenArgs canonicalJson bytes",
  );
}

function jsonByteLengthUpTo(
  value: unknown,
  budget: number,
  path: string,
  depth: number,
  ancestors: Set<object>,
): number {
  if (depth > MAX_FROZEN_ARGS_PREFLIGHT_DEPTH) {
    throw new Error(`canonicalJSON: max recursion depth exceeded at ${path}`);
  }

  if (value === null) return "null".length;

  switch (typeof value) {
    case "boolean":
      return value ? "true".length : "false".length;
    case "number":
      if (!Number.isFinite(value)) {
        throw new Error(
          `canonicalJSON: non-finite number at ${path} (NaN/+/-Infinity not representable in JCS)`,
        );
      }
      return JSON.stringify(value).length;
    case "string":
      return jsonStringByteLengthUpTo(value, budget, path);
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
    return jsonArrayByteLengthUpTo(value, budget, path, depth, ancestors);
  }

  return jsonObjectByteLengthUpTo(value, budget, path, depth, ancestors);
}

function jsonArrayByteLengthUpTo(
  value: readonly unknown[],
  budget: number,
  path: string,
  depth: number,
  ancestors: Set<object>,
): number {
  if (Object.getOwnPropertySymbols(value).length > 0) {
    throw new Error(`canonicalJSON: symbol keys are not representable at ${path}`);
  }
  if (ancestors.has(value)) {
    throw new Error(`canonicalJSON: circular reference at ${path}`);
  }

  ancestors.add(value);
  try {
    const descriptors = Object.getOwnPropertyDescriptors(value);
    let bytes = 2;
    for (let i = 0; i < value.length; i++) {
      const key = String(i);
      if (!Object.hasOwn(descriptors, key)) {
        throw new Error(`canonicalJSON: sparse array hole at ${path}[${i}]`);
      }
    }
    for (const [key, descriptor] of Object.entries(descriptors)) {
      if (key === "length") continue;
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
      if (index > 0) {
        bytes = addJsonBytes(bytes, 1, budget);
      }
      bytes = addJsonBytes(
        bytes,
        jsonByteLengthUpTo(
          descriptor.value,
          budget - Math.min(bytes, budget),
          `${path}[${index}]`,
          depth + 1,
          ancestors,
        ),
        budget,
      );
      if (bytes > budget) return budget + 1;
    }
    return bytes;
  } finally {
    ancestors.delete(value);
  }
}

function jsonObjectByteLengthUpTo(
  value: object,
  budget: number,
  path: string,
  depth: number,
  ancestors: Set<object>,
): number {
  const proto = Object.getPrototypeOf(value);
  if (proto !== Object.prototype && proto !== null) {
    throw new Error(`canonicalJSON: non-plain object at ${path} (got ${describeProto(proto)})`);
  }
  if (Object.getOwnPropertySymbols(value).length > 0) {
    throw new Error(`canonicalJSON: symbol keys are not representable at ${path}`);
  }
  if (ancestors.has(value)) {
    throw new Error(`canonicalJSON: circular reference at ${path}`);
  }

  ancestors.add(value);
  try {
    const descriptors = Object.getOwnPropertyDescriptors(value);
    let bytes = 2;
    let fieldCount = 0;
    for (const [key, descriptor] of Object.entries(descriptors)) {
      if (!descriptor.enumerable) {
        throw new Error(`canonicalJSON: non-enumerable own property at ${path}.${key}`);
      }
      if ("get" in descriptor || "set" in descriptor) {
        throw new Error(`canonicalJSON: accessor property at ${path}.${key}`);
      }
      if (fieldCount > 0) {
        bytes = addJsonBytes(bytes, 1, budget);
      }
      bytes = addJsonBytes(bytes, jsonStringByteLengthUpTo(key, budget, path, false), budget);
      bytes = addJsonBytes(bytes, 1, budget);
      bytes = addJsonBytes(
        bytes,
        jsonByteLengthUpTo(
          descriptor.value,
          budget - Math.min(bytes, budget),
          `${path}.${key}`,
          depth + 1,
          ancestors,
        ),
        budget,
      );
      fieldCount += 1;
      if (bytes > budget) return budget + 1;
    }
    return bytes;
  } finally {
    ancestors.delete(value);
  }
}

function jsonStringByteLengthUpTo(
  value: string,
  budget: number,
  path: string,
  rejectLoneSurrogates = true,
): number {
  let bytes = 2;
  for (let i = 0; i < value.length; i++) {
    const code = value.charCodeAt(i);
    if (code === 0x22 || code === 0x5c) {
      bytes += 2;
    } else if (code === 0x08 || code === 0x09 || code === 0x0a || code === 0x0c || code === 0x0d) {
      bytes += 2;
    } else if (code <= 0x1f) {
      bytes += 6;
    } else if (code >= 0xd800 && code <= 0xdbff) {
      if (i + 1 >= value.length) {
        if (!rejectLoneSurrogates) {
          bytes += 6;
          continue;
        }
        throw new Error(`canonicalJSON: lone high surrogate at ${path}`);
      }
      const next = value.charCodeAt(i + 1);
      if (next < 0xdc00 || next > 0xdfff) {
        if (!rejectLoneSurrogates) {
          bytes += 6;
          continue;
        }
        throw new Error(`canonicalJSON: lone high surrogate at ${path}`);
      }
      bytes += 4;
      i += 1;
    } else if (code >= 0xdc00 && code <= 0xdfff) {
      if (!rejectLoneSurrogates) {
        bytes += 6;
        continue;
      }
      throw new Error(`canonicalJSON: lone low surrogate at ${path}`);
    } else if (code <= 0x7f) {
      bytes += 1;
    } else if (code <= 0x7ff) {
      bytes += 2;
    } else {
      bytes += 3;
    }

    if (bytes > budget) return budget + 1;
  }
  return bytes;
}

function utf8ByteLengthUpTo(value: string, budget: number): number {
  let bytes = 0;
  for (let i = 0; i < value.length; i++) {
    const code = value.charCodeAt(i);
    if (code <= 0x7f) {
      bytes += 1;
    } else if (code <= 0x7ff) {
      bytes += 2;
    } else if (code >= 0xd800 && code <= 0xdbff && i + 1 < value.length) {
      const next = value.charCodeAt(i + 1);
      if (next >= 0xdc00 && next <= 0xdfff) {
        bytes += 4;
        i += 1;
      } else {
        bytes += 3;
      }
    } else {
      bytes += 3;
    }

    if (bytes > budget) return budget + 1;
  }
  return bytes;
}

function addJsonBytes(total: number, amount: number, budget: number): number {
  const next = total + amount;
  return next > budget ? budget + 1 : next;
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
