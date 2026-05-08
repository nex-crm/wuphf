import { ONBOARDING_COPY } from "../../../lib/constants";
import { BtnLabel, EnterHint } from "./components";
import type { ReadinessCheck } from "./types";

interface ReadyStepProps {
  checks: ReadinessCheck[];
  taskText: string;
  submitting: boolean;
  submitError: string;
  onSkip: () => void;
  onSubmit: () => void;
  onBack: () => void;
}

// ReadyStep is the six-item final review matching the TUI's InitDone
// readinessChecks() view. It's honest: a missing Nex key is not papered
// over, and GBrain+no-OpenAI-key would show a red "missing" row (though
// the Setup step blocks continuing in that case, so users shouldn't get
// here with it).
export function ReadyStep({
  checks,
  taskText,
  submitting,
  submitError,
  onSkip,
  onSubmit,
  onBack,
}: ReadyStepProps) {
  return (
    <div className="wizard-step">
      <div className="wizard-hero">
        <h1 className="wizard-headline wizard-headline-sm">
          {ONBOARDING_COPY.step8_headline}
        </h1>
        <p className="wizard-subhead">{ONBOARDING_COPY.step8_subhead}</p>
      </div>

      <div className="wizard-panel readiness-panel">
        <ul className="readiness-list">
          {checks.map((check) => (
            <li key={check.label} className="readiness-item">
              <span
                className={`readiness-glyph ${check.status}`}
                aria-hidden="true"
              >
                {check.status === "ready"
                  ? "✓"
                  : check.status === "next"
                    ? "—"
                    : "!"}
              </span>
              <div className="readiness-body">
                <div className="readiness-label">{check.label}</div>
                <div className="readiness-detail">{check.detail}</div>
              </div>
            </li>
          ))}
        </ul>
      </div>

      <div className="wizard-panel">
        <p className="wizard-panel-title">Access after setup</p>
        <div style={{ display: "grid", gap: 10, fontSize: 13 }}>
          <div>
            <strong>You:</strong> use Access &amp; Health to check the live
            connection, see the SSH/LAN/Tailscale path, and create, copy,
            refresh, or stop scoped team-member invites.
          </div>
          <div>
            <strong>Team member:</strong> joins through the invite link you
            share.
          </div>
        </div>
      </div>

      {submitError ? (
        <div
          role="alert"
          data-testid="onboarding-submit-error"
          style={{
            fontSize: 13,
            color: "var(--danger-500, #c33)",
            padding: "12px 14px",
            background: "var(--danger-50, #fee)",
            border: "1px solid var(--danger-200, #fcc)",
            borderRadius: 6,
          }}
        >
          <strong>Could not start the office:</strong> {submitError}. Check that
          your CLI runtime is installed and try again, or go back to adjust your
          setup.
        </div>
      ) : null}

      <div className="wizard-nav">
        <button className="btn btn-ghost" onClick={onBack} type="button">
          Back
        </button>
        <div className="wizard-nav-right">
          <button
            className="btn btn-primary"
            data-testid="onboarding-submit-button"
            onClick={taskText.trim().length === 0 ? onSkip : onSubmit}
            disabled={submitting}
            type="button"
          >
            <BtnLabel>
              {submitting
                ? "Starting..."
                : submitError && taskText.trim().length > 0
                  ? "Retry"
                  : ONBOARDING_COPY.step8_cta}
            </BtnLabel>
            {!submitting && taskText.trim().length > 0 && <EnterHint />}
          </button>
        </div>
      </div>
    </div>
  );
}
