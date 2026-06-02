import type { CSSProperties } from "react";

import {
  type LifecycleState,
  STATE_PILL_TOKENS,
} from "../../lib/types/lifecycle";
import {
  activityDotForLifecycleState,
  ariaLabelForActivityDot,
  type StatusDotKind,
} from "./TaskActivityStream";

interface LifecycleStatePillProps {
  state: LifecycleState;
}

/**
 * Unified state + activity pill.
 *
 * Carries two axes of information in one element:
 *   - Pill background and text are categorical per state (drafting, running,
 *     review, blocked, …) so a board scan reads the lifecycle position at a
 *     glance. Tokens live in STATE_PILL_TOKENS.
 *   - The leading dot encodes the live activity bucket:
 *       green-blink — RUNNING (owner is actively working)
 *       red          — BLOCKED (technical block; human cannot help)
 *       orange       — NEEDS_INPUT (waiting on review / decision)
 *       grey         — IDLE (drafting, ready, terminal)
 *
 * That replaces the previous arrangement where a standalone dot sat next
 * to the pill — same two signals, but two clashing dots.
 */

const UNKNOWN_PILL = {
  bg: "var(--bg-row-active, rgba(255,255,255,0.04))",
  text: "var(--text-tertiary, #888)",
  label: "unknown",
};

// Color mapping for the activity dot. Kept here (not in CSS-only) so the
// pill can render in any surface — including ones outside the
// `.issue-activity-dot` class scope — without leaking on missing styles.
const DOT_COLOR: Record<StatusDotKind, string> = {
  running: "var(--green-500, #16a34a)",
  blocked: "var(--red-500, var(--destructive, #dc2626))",
  "needs-input": "var(--orange-500, #f97316)",
  idle: "var(--text-tertiary, var(--text-secondary))",
};

export function LifecycleStatePill({ state }: LifecycleStatePillProps) {
  const token = STATE_PILL_TOKENS[state] ?? {
    ...UNKNOWN_PILL,
    label: String(state || "unknown"),
  };
  const { bg, text, label } = token;
  const dotKind = activityDotForLifecycleState(state);
  const style: CSSProperties = {
    background: bg,
    color: text,
  };
  const dotStyle: CSSProperties = {
    background: DOT_COLOR[dotKind],
  };
  return (
    <span
      className={`lifecycle-state-pill lifecycle-state-pill--${dotKind}`}
      style={style}
      data-state={state}
      data-activity={dotKind}
      aria-label={`State: ${label}. ${ariaLabelForActivityDot(dotKind)}.`}
    >
      <span
        className={`dot${dotKind === "running" ? " dot--blink" : ""}`}
        style={dotStyle}
        aria-hidden="true"
      />
      {label}
    </span>
  );
}
