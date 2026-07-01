/**
 * StepFirstIssue — the final wizard step, "Hand off your first workflow."
 *
 * Collects the first workflow text, prefilled with the RevOps CRM-audit
 * example (ONBOARDING_FIRST_ISSUE_EXAMPLE, seeded into answers.firstIssue by
 * the wizard hook). Finish seeds the (empty) office and drops the user into
 * #general with this text already in the composer. There is no CEO and no
 * team: the workflow waits for the first agent the user spins up.
 *
 * The right rail shows where the handoff is headed: a small mock #general
 * row. A "reset to the example" link restores the prefill if the user clears
 * or edits it and wants it back.
 *
 * The wizard's advance gate requires non-empty text, so the Finish button is
 * disabled while the field is blank. Copy from
 * ONBOARDING_WIZARD_COPY.first-issue.
 *
 * As the final step, this is also where the "keep me in touch" consent box
 * lives. It is only shown when an email was captured on the welcome step, is
 * checked by default, and gates whether that address is attached to the PostHog
 * person at Finish (the email is still stored locally either way). See
 * useOnboardingWizard + lib/analytics.
 */

import { useCallback } from "react";

import {
  ONBOARDING_ANALYTICS_CONSENT_COPY,
  ONBOARDING_EMAIL_COPY,
  ONBOARDING_FIRST_ISSUE_EXAMPLE,
  ONBOARDING_WIZARD_COPY,
  type OnboardingWizardStepProps,
} from "../wizardSteps";

const COPY = ONBOARDING_WIZARD_COPY["first-issue"];

export function StepFirstIssue({
  active,
  answers,
  setAnswers,
}: OnboardingWizardStepProps) {
  const resetToExample = useCallback(() => {
    setAnswers({ firstIssue: ONBOARDING_FIRST_ISSUE_EXAMPLE });
  }, [setAnswers]);

  const isExample =
    answers.firstIssue.trim() === ONBOARDING_FIRST_ISSUE_EXAMPLE.trim();

  return (
    <div
      className="office-tour-slide office-tour-slide-issues"
      data-active={active}
      data-testid="onboarding-step-first-issue"
    >
      <div className="office-tour-slide-copy">
        <p className="office-tour-slide-eyebrow">{COPY.eyebrow}</p>
        <h2 className="office-tour-slide-headline office-tour-slide-headline--serif">
          {COPY.headline}
        </h2>
        <p className="office-tour-slide-body">{COPY.body}</p>

        <div className="onboarding-team-field onboarding-first-issue-field">
          <div className="onboarding-first-issue-label-row">
            <label
              className="onboarding-team-label"
              htmlFor="onboarding-first-issue"
            >
              Your first workflow
            </label>
            {isExample ? null : (
              <button
                type="button"
                className="onboarding-first-issue-reset"
                onClick={resetToExample}
                data-testid="onboarding-first-issue-reset"
              >
                Use the example
              </button>
            )}
          </div>
          <textarea
            id="onboarding-first-issue"
            className="onboarding-team-textarea onboarding-first-issue-textarea"
            value={answers.firstIssue}
            placeholder="Audit our CRM for duplicate accounts and stale deals, then propose a cleanup plan."
            onChange={(event) => setAnswers({ firstIssue: event.target.value })}
            data-testid="onboarding-first-issue"
          />
          <p className="onboarding-first-issue-hint">
            It waits in #general. The first agent you spin up picks it up and
            runs it end to end.
          </p>
        </div>

        {answers.email.trim() ? (
          <label className="onboarding-keep-in-touch">
            <input
              type="checkbox"
              className="onboarding-keep-in-touch-box"
              checked={answers.keepInTouch}
              onChange={(event) =>
                setAnswers({ keepInTouch: event.target.checked })
              }
              data-testid="onboarding-keep-in-touch"
            />
            <span className="onboarding-keep-in-touch-copy">
              {ONBOARDING_EMAIL_COPY.consent}
            </span>
          </label>
        ) : null}

        <div
          className="onboarding-analytics-consent"
          data-testid="onboarding-analytics-consent"
        >
          <p className="onboarding-analytics-consent-heading">
            {ONBOARDING_ANALYTICS_CONSENT_COPY.heading}
          </p>
          <label className="onboarding-keep-in-touch">
            <input
              type="checkbox"
              className="onboarding-keep-in-touch-box"
              checked={answers.telemetryConsent}
              onChange={(event) =>
                setAnswers({ telemetryConsent: event.target.checked })
              }
              data-testid="onboarding-consent-telemetry"
            />
            <span className="onboarding-keep-in-touch-copy">
              {ONBOARDING_ANALYTICS_CONSENT_COPY.telemetryLabel}
            </span>
          </label>
          <label className="onboarding-keep-in-touch">
            <input
              type="checkbox"
              className="onboarding-keep-in-touch-box"
              checked={answers.recordingConsent}
              onChange={(event) =>
                setAnswers({ recordingConsent: event.target.checked })
              }
              data-testid="onboarding-consent-recording"
            />
            <span className="onboarding-keep-in-touch-copy">
              {ONBOARDING_ANALYTICS_CONSENT_COPY.recordingLabel}
            </span>
          </label>
          <p className="onboarding-analytics-consent-note">
            {ONBOARDING_ANALYTICS_CONSENT_COPY.note}
          </p>
        </div>
      </div>

      <div className="office-tour-slide-stage office-tour-slide-stage--first-issue">
        <div className="onboarding-handoff-card" aria-hidden="true">
          <div className="onboarding-handoff-head">
            <span className="onboarding-handoff-who">
              <span className="onboarding-handoff-name">#general</span>
            </span>
            <span className="onboarding-handoff-route">Workflow handoff</span>
          </div>
          <div className="onboarding-handoff-bubble">
            {answers.firstIssue.trim() || "Write the first thing you want run."}
          </div>
          <div className="onboarding-handoff-foot">
            <span className="onboarding-handoff-send">Send</span>
            <span className="onboarding-handoff-note">
              Lands in your composer, ready to send.
            </span>
          </div>
        </div>
      </div>
    </div>
  );
}
