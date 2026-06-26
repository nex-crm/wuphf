/**
 * Small wizard-only display primitives: the arrow / check / return icons, the
 * inline Enter-key hint badge, and the primary-button label wrapper.
 *
 * These are the original onboarding wizard's components (recovered from the
 * pre-CEO-chat wizard) so the visual stepped wizard's CTAs carry the same
 * keyboard-shortcut affordance the product used before: a real keycap-styled
 * ⏎ glyph beside the label, with an optional ⌘/Ctrl modifier badge. The badge
 * styling lives in web/src/styles/kbd.css (`.btn-label`, `.kbd-hint`).
 */

import type { ReactNode } from "react";

/** Trailing → on "advance to the next step" CTAs. */
export function ArrowIcon() {
  return (
    <svg
      aria-hidden="true"
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M5 12h14" />
      <path d="m12 5 7 7-7 7" />
    </svg>
  );
}

/** Checkmark for confirmation CTAs. */
export function CheckIcon() {
  return (
    <svg
      aria-hidden="true"
      width="12"
      height="12"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="3"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <polyline points="20 6 9 17 4 12" />
    </svg>
  );
}

/** The ↵ return glyph, styled into a keycap by `.kbd-hint svg` in kbd.css. */
function ReturnIcon() {
  return (
    <svg
      width="12"
      height="12"
      viewBox="0 0 12 12"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M9 3v3.5a1.5 1.5 0 0 1-1.5 1.5H3" />
      <path d="M5 6L3 8l2 2" />
    </svg>
  );
}

/**
 * Inline Enter-key hint for primary CTAs. Purely decorative — the real Enter
 * handling lives at the wizard level so it works from anywhere on the step, not
 * only when the button has focus. Pass `modifier` (e.g. ⌘/Ctrl) when the step
 * binds modifier+Enter instead of plain Enter (the multiline first-issue step).
 */
export function EnterHint({ modifier }: { modifier?: string } = {}) {
  return (
    <span className="kbd-hint" aria-hidden="true">
      {modifier ? <span className="kbd-hint-mod">{modifier}</span> : null}
      <ReturnIcon />
    </span>
  );
}

/** Label half of a primary CTA that carries an inline keyboard hint. */
export function BtnLabel({ children }: { children: ReactNode }) {
  return <span className="btn-label">{children}</span>;
}
