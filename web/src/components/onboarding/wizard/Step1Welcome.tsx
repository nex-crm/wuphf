import { useState } from "react";

import { ONBOARDING_COPY } from "../../../lib/constants";
import { BtnLabel, EnterHint } from "./components";

interface WelcomeStepProps {
  onNext: () => void;
  // Resume affordance: when a previous draft is detected, offer a way
  // to wipe it before starting again. Optional so existing call sites
  // (and tests that don't care about resume) keep working.
  hasSavedDraft?: boolean;
  onResetDraft?: () => void;
}

export function WelcomeStep({
  onNext,
  hasSavedDraft = false,
  onResetDraft,
}: WelcomeStepProps) {
  const [confirmingReset, setConfirmingReset] = useState(false);
  return (
    <div className="wizard-step">
      <div className="wizard-hero">
        <h1 className="wizard-headline">{ONBOARDING_COPY.step1_headline}</h1>
        <p className="wizard-subhead" style={{ whiteSpace: "pre-line" }}>
          {ONBOARDING_COPY.step1_subhead}
        </p>
      </div>
      <div style={{ display: "flex", justifyContent: "center" }}>
        <button className="btn btn-primary" onClick={onNext} type="button">
          <BtnLabel>{ONBOARDING_COPY.step1_cta}</BtnLabel>
          <EnterHint />
        </button>
      </div>
      {hasSavedDraft && onResetDraft ? (
        <div
          style={{
            display: "flex",
            justifyContent: "center",
            marginTop: 16,
          }}
          data-testid="welcome-reset-row"
        >
          {confirmingReset ? (
            <span style={{ display: "inline-flex", gap: 12 }}>
              <span style={{ color: "var(--text-tertiary)", fontSize: 13 }}>
                Discard your saved progress?
              </span>
              <button
                type="button"
                className="link-btn"
                onClick={() => {
                  onResetDraft();
                  setConfirmingReset(false);
                }}
                data-testid="welcome-reset-confirm"
              >
                Yes, start over
              </button>
              <button
                type="button"
                className="link-btn"
                onClick={() => setConfirmingReset(false)}
                data-testid="welcome-reset-cancel"
              >
                Cancel
              </button>
            </span>
          ) : (
            <button
              type="button"
              className="link-btn"
              onClick={() => setConfirmingReset(true)}
              data-testid="welcome-reset-trigger"
            >
              Reset setup
            </button>
          )}
        </div>
      ) : null}
    </div>
  );
}
