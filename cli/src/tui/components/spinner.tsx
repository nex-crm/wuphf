import React, { useState, useEffect } from "react";
import { Text } from "ink";
import type { ReactNode } from "react";

// ── Braille spinner frames ──────────────────────────────────────────

const FRAMES = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"] as const;
const DEFAULT_INTERVAL = 80;

// ── Types ───────────────────────────────────────────────────────────

export interface SpinnerProps {
  /** Label displayed after the spinner character */
  label?: string;
  /** Animation interval in ms (default: 80) */
  interval?: number;
  /** Ink Text color for the spinner character */
  color?: string;
  /** When true, the spinner stops animating */
  stopped?: boolean;
}

// ── Hook ────────────────────────────────────────────────────────────

/**
 * Returns the current braille spinner frame, advancing every `interval` ms.
 */
export function useSpinner(interval = DEFAULT_INTERVAL): string {
  const [index, setIndex] = useState(0);

  useEffect(() => {
    const id = setInterval(() => {
      setIndex((prev) => (prev + 1) % FRAMES.length);
    }, interval);
    return () => clearInterval(id);
  }, [interval]);

  return FRAMES[index];
}

// ── Component ───────────────────────────────────────────────────────

export function Spinner({
  label,
  interval = DEFAULT_INTERVAL,
  color = "cyan",
  stopped = false,
}: SpinnerProps): React.JSX.Element {
  const frame = useSpinner(interval);

  if (stopped) {
    return label ? <Text dimColor>{label}</Text> : <Text>{""}</Text>;
  }

  return (
    <Text>
      <Text color={color}>{frame}</Text>
      {label ? <Text>{` ${label}`}</Text> : null}
    </Text>
  );
}

export default Spinner;
