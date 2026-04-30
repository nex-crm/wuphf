import { Kbd } from "../../ui/Kbd";
import { STEP_ORDER } from "./constants";
import type { WizardStep } from "./types";

// Small wizard-only display components: arrow / check icons, the
// Enter-hint badge, and the step progress dots. Extracted from
// Wizard.tsx so step files can import them directly without dragging
// in the full wizard module.

export function ArrowIcon() {
  return (
    <svg
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
export function EnterHint({ modifier }: { modifier?: string } = {}) {
  return (
    <span className="kbd-hint" aria-hidden="true">
      {modifier && (
        <Kbd size="sm" variant="inverse">
          {modifier}
        </Kbd>
      )}
      <Kbd size="sm" variant="inverse">
        ↵
      </Kbd>
    </span>
  );
}

export function ProgressDots({ current }: { current: WizardStep }) {
  return (
    <div className="wizard-progress">
      {STEP_ORDER.map((step) => (
        <div
          key={step}
          className={`wizard-progress-dot ${step === current ? "active" : "inactive"}`}
        />
      ))}
    </div>
  );
}
