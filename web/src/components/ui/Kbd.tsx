import type { ReactNode } from "react";

import { keyedByOccurrence } from "../../lib/reactKeys";

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

// Mac/cmd-style modifier glyphs render visually small in most UI fonts vs
// alphanumerics — they're cleaner as a slightly larger inline glyph so a
// chord like "⌘1" reads with balanced weight on both sides.
const GLYPH_PATTERN = /(⌘|⌥|⌃|⇧|⏎|⌫|⌦|⎋|↑|↓|←|→|⇪|⇥|⏏|⏯)/g;

function decorateGlyphs(node: ReactNode): ReactNode {
  if (typeof node !== "string") return node;
  const parts = node.split(GLYPH_PATTERN);
  if (parts.length === 1) return node;
  return parts.map((part, i) =>
    GLYPH_PATTERN.test(part) ? (
      // biome-ignore lint/suspicious/noArrayIndexKey: stable position in split output.
      <span key={i} className="kbd-glyph">
        {part}
      </span>
    ) : (
      part
    ),
  );
}

export function Kbd({
  children,
  size = "md",
  variant = "default",
  className = "",
}: KbdProps) {
  const cls =
    `kbd kbd-${size} ${variant === "inverse" ? "kbd-inverse" : ""} ${className}`.trim();
  return <kbd className={cls}>{decorateGlyphs(children)}</kbd>;
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
  return (
    <span className={`kbd-sequence ${className}`.trim()}>
      {keyedByOccurrence(chords, (chord) => chord.join("+")).map(
        ({ key: chordKey, value: chord, index: i }) => (
          <span key={chordKey} className="kbd-chord">
            {i > 0 && (
              <span className="kbd-then" aria-hidden="true">
                then
              </span>
            )}
            {keyedByOccurrence(chord, (k) => k).map(
              ({ key: keyKey, value: k }) => (
                <Kbd key={keyKey} size={size} variant={variant}>
                  {k}
                </Kbd>
              ),
            )}
          </span>
        ),
      )}
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
