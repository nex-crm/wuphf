import canonicalize from "canonicalize";

export function canonicalJSON(input: unknown): string {
  if (input === undefined) {
    throw new Error("canonicalJSON: undefined is not representable in JSON");
  }
  rejectNonFiniteNumbers(input);
  const serialized = canonicalize(input);
  if (serialized === undefined) {
    throw new Error("canonicalJSON: input contains undefined value");
  }
  return serialized;
}

function rejectNonFiniteNumbers(value: unknown, path = "$"): void {
  if (typeof value === "number") {
    if (!Number.isFinite(value)) {
      throw new Error(
        `canonicalJSON: non-finite number at ${path} (NaN/±Infinity not representable in JCS)`,
      );
    }
    return;
  }
  if (Array.isArray(value)) {
    for (let i = 0; i < value.length; i++) {
      rejectNonFiniteNumbers(value[i], `${path}[${i}]`);
    }
    return;
  }
  if (value !== null && typeof value === "object") {
    for (const [k, v] of Object.entries(value)) {
      rejectNonFiniteNumbers(v, `${path}.${k}`);
    }
  }
}
