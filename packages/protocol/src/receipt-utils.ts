// Shared internal helpers used by hand-rolled validators and codecs. Not part
// of the public API surface.

import type { ReceiptValidationError } from "./receipt-types.ts";

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function hasOwn(record: Readonly<Record<string, unknown>>, key: string): boolean {
  return Object.hasOwn(record, key);
}

export function recordValue(record: Readonly<Record<string, unknown>>, key: string): unknown {
  const descriptor = Object.getOwnPropertyDescriptor(record, key);
  return descriptor !== undefined && "value" in descriptor ? descriptor.value : undefined;
}

export function addError(errors: ReceiptValidationError[], path: string, message: string): void {
  errors.push({ path, message });
}

export function pointer(base: string, segment: string): string {
  // RFC 6901 escaping: ~ → ~0, / → ~1. Order matters.
  const escaped = segment.replace(/~/g, "~0").replace(/\//g, "~1");
  return `${base}/${escaped}`;
}

export function omitUndefined<T extends Record<string, unknown>>(input: T): T {
  const out: Partial<T> = {};
  for (const [key, value] of Object.entries(input) as [keyof T, T[keyof T]][]) {
    if (value !== undefined) {
      out[key] = value;
    }
  }
  return out as T;
}

export function formatValidationErrors(errors: readonly ReceiptValidationError[]): string {
  return errors.map((error) => `${error.path}: ${error.message}`).join("; ");
}

export function requireRecord(value: unknown, path: string): Readonly<Record<string, unknown>> {
  if (!isRecord(value)) {
    throw new Error(`${path}: must be an object`);
  }
  return value;
}

export function assertKnownKeys(
  record: Readonly<Record<string, unknown>>,
  basePath: string,
  allowed: ReadonlySet<string>,
): void {
  for (const key of Object.keys(record)) {
    if (!allowed.has(key)) {
      throw new Error(`${pointer(basePath, key)}: is not allowed`);
    }
  }
}
