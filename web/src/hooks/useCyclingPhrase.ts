import { useEffect, useRef, useState } from "react";

/**
 * Cycles through `phrases` on a fixed interval, returning the current entry.
 * Pass a module-level constant array so its identity is stable across renders.
 * Returns "" when there is nothing to show.
 *
 * The starting index is randomised so two loaders on screen do not march in
 * lock step; the rotation order itself stays deterministic.
 */
export function useCyclingPhrase(
  phrases: readonly string[],
  intervalMs = 2400,
  enabled = true,
): string {
  const [index, setIndex] = useState(() =>
    phrases.length ? Math.floor(Math.random() * phrases.length) : 0,
  );
  // Keep the timer reading the latest length without re-arming on every tick.
  const lengthRef = useRef(phrases.length);
  lengthRef.current = phrases.length;

  useEffect(() => {
    if (!enabled || phrases.length <= 1) return;
    const id = setInterval(() => {
      setIndex((i) => (i + 1) % Math.max(1, lengthRef.current));
    }, intervalMs);
    return () => clearInterval(id);
  }, [enabled, phrases.length, intervalMs]);

  if (!phrases.length) return "";
  return phrases[index % phrases.length];
}
