import type { CSSProperties } from "react";

import {
  type LifecycleState,
  STATE_PILL_TOKENS,
} from "../../lib/types/lifecycle";

interface LifecycleStatePillProps {
  state: LifecycleState;
}

/**
 * Inline state pill (decision / running / blocked / merged / etc.).
 * Reads tokens from the central STATE_PILL_TOKENS map so a pill on the
 * Inbox row and one on the Decision Packet meta row stay byte-identical.
 *
 * The label text is the only signal users with color-vision differences
 * see. Both color and label render together — color is never the
 * sole signal.
 */
export function LifecycleStatePill({ state }: LifecycleStatePillProps) {
  const { bg, text, label } = STATE_PILL_TOKENS[state];
  const style: CSSProperties = {
    background: bg,
    color: text,
  };
  return (
    <span
      className="lifecycle-state-pill"
      style={style}
      data-state={state}
      aria-label={`State: ${label}`}
    >
      <span className="dot" aria-hidden="true" />
      {label}
    </span>
  );
}
