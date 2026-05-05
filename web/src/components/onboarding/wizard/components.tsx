import { STEP_ORDER } from "./constants";
import type { WizardStep } from "./types";

// Small wizard-only display components: arrow / check icons, the
// Enter-hint badge, and the step progress dots. Extracted from
// Wizard.tsx so step files can import them directly without dragging
// in the full wizard module.

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

/**
 * Inline Enter-key hint for primary CTAs. Purely decorative — the real
 * Enter handling lives at the Wizard level so it works from anywhere on
 * the step, not just when the button has focus. Pass `modifier` (e.g.
 * ⌘/Ctrl) when the step binds ⌘+Enter instead of plain Enter.
 */
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

export function EnterHint({ modifier }: { modifier?: string } = {}) {
  return (
    <span className="kbd-hint" aria-hidden="true">
      {modifier ? <span className="kbd-hint-mod">{modifier}</span> : null}
      <ReturnIcon />
    </span>
  );
}

export function BtnLabel({ children }: { children: React.ReactNode }) {
  return <span className="btn-label">{children}</span>;
}

export function ProgressDots({
  current,
  steps = STEP_ORDER,
}: {
  current: WizardStep;
  steps?: readonly WizardStep[];
}) {
  return (
    <div className="wizard-progress">
      {steps.map((step) => (
        <div
          key={step}
          className={`wizard-progress-dot ${step === current ? "active" : "inactive"}`}
        />
      ))}
    </div>
  );
}
