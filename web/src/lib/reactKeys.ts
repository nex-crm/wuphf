export interface KeyedOccurrence<T> {
  key: string;
  value: T;
  index: number;
}

/**
 * Builds React keys from a semantic base while preserving duplicate
 * occurrences. Use this when an index-only key was removed but the remaining
 * data is not guaranteed to be globally unique.
 */
export function keyedByOccurrence<T>(
  values: readonly T[],
  baseKey: (value: T, index: number) => string,
): KeyedOccurrence<T>[] {
  const seen = new Map<string, number>();
  const emitted = new Set<string>();
  return values.map((value, index) => {
    const base = baseKey(value, index) || "item";
    const occurrence = seen.get(base) ?? 0;
    seen.set(base, occurrence + 1);
    let key = occurrence === 0 ? base : `${base}#${occurrence}`;
    let collision = occurrence + 1;
    while (emitted.has(key)) {
      key = `${base}#${collision}`;
      collision += 1;
    }
    emitted.add(key);
    return {
      key,
      value,
      index,
    };
  });
}
