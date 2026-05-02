import type { ReactNode } from "react";

/**
 * Shared keyboard-key badge. Renders real <kbd> semantics so assistive
 * tech announces the key, and uses a single visual treatment across the
 * app — sidebar hints, wizard CTAs, help modal, status bar.
 */
interface KbdProps {
  children: ReactNode;
  size?: "sm" | "md";
  variant?: "default" | "inverse";
  className?: string;
}

export function Kbd({
  children,
  size = "md",
  variant = "default",
  className = "",
}: KbdProps) {
  const cls =
    `kbd kbd-${size} ${variant === "inverse" ? "kbd-inverse" : ""} ${className}`.trim();
  return <kbd className={cls}>{children}</kbd>;
}

/**
 * One or more keys rendered as a sequence, with a thin "then" separator
 * between chord segments. Pass keys as an array of arrays when needed
 * (e.g. `[['g'], ['g']]` for gg). For simple combos use a single array
 * (e.g. `['⌘', 'K']`).
 */
interface KbdSequenceProps {
  keys: string[] | string[][];
  size?: "sm" | "md";
  variant?: "default" | "inverse";
  className?: string;
}

export function KbdSequence({
  keys,
  size = "md",
  variant = "default",
  className = "",
}: KbdSequenceProps) {
  const chords: string[][] = Array.isArray(keys[0])
    ? (keys as string[][])
    : [keys as string[]];
  const chordKeys = new Map<string, number>();
  const keyKeys = new Map<string, number>();
  const uniqueKey = (seen: Map<string, number>, value: string) => {
    const occurrence = seen.get(value) ?? 0;
    seen.set(value, occurrence + 1);
    return occurrence === 0 ? value : `${value}-${occurrence}`;
  };
  return (
    <span className={`kbd-sequence ${className}`.trim()}>
      {chords.map((chord, i) => (
        <span key={uniqueKey(chordKeys, chord.join("+"))} className="kbd-chord">
          {i > 0 && (
            <span className="kbd-then" aria-hidden="true">
              then
            </span>
          )}
          {chord.map((k) => (
            <Kbd key={uniqueKey(keyKeys, k)} size={size} variant={variant}>
              {k}
            </Kbd>
          ))}
        </span>
      ))}
    </span>
  );
}

/**
 * Platform-aware modifier label. macOS users see the glyph; everyone else
 * sees "Ctrl". We only detect once at module load; swapping OS mid-session
 * is not a real case.
 */
const isMac =
  typeof navigator !== "undefined" &&
  /Mac|iPod|iPhone|iPad/.test(navigator.platform);

export const MOD_KEY = isMac ? "⌘" : "Ctrl";
