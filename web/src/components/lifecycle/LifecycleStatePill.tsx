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

/**
 * True when a task sits assigned to the "auto" sentinel in a pre-run
 * state — i.e. the CEO triage loop is picking the owner (see
 * requestAutoAssignmentLocked on the Go side). The task header renders
 * the staffing pill + copy instead of "parked", which implied the human
 * had to act (the removed approval wall) when nothing was waiting on
 * them. Lives next to the pill (not in TaskDocument) so the staffing
 * regression test can import it without pulling in the chat surface.
 */
export function isAwaitingStaffing(
  doc: { ownerSlug?: string; lifecycleState: LifecycleState } | undefined,
): boolean {
  if (!doc) return false;
  if (doc.ownerSlug?.trim().toLowerCase() !== "auto") return false;
  return (
    doc.lifecycleState === "drafting" ||
    doc.lifecycleState === "intake" ||
    doc.lifecycleState === "ready"
  );
}

/**
 * Pill for an ownerless ("auto") task awaiting CEO staffing. Rendered in
 * place of the lifecycle pill so the page says what is actually happening
 * — the CEO is picking an owner — instead of "parked", which read like the
 * removed approval wall to new users (live smoke run gap #1).
 */
export function StaffingStatePill() {
  return (
    <span
      className="lifecycle-state-pill lifecycle-state-pill--running"
      style={{ background: "var(--accent-bg)", color: "var(--accent)" }}
      data-state="staffing"
      data-activity="running"
      data-testid="staffing-state-pill"
      title="The CEO is picking the owner"
    >
      <span
        className="dot dot--blink"
        style={{ background: DOT_COLOR.running }}
        aria-hidden="true"
      />
      staffing
    </span>
  );
}

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
