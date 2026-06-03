/**
 * StepFirstIssue — wizard step 05, "Write your first issue."
 *
 * Collects the first issue text, prefilled with the RevOps CRM-audit example
 * (ONBOARDING_FIRST_ISSUE_EXAMPLE, seeded into answers.firstIssue by the
 * wizard hook). Finish seeds the office and drops the user into #general /
 * the CEO DM with this text already in the composer.
 *
 * The right rail shows where the issue is headed: a small mock CEO DM row so
 * the user sees the handoff before it happens. A "reset to the example" link
 * restores the prefill if the user clears or edits it and wants it back.
 *
 * The wizard's advance gate requires non-empty text, so the Finish button is
 * disabled while the field is blank. Copy from
 * ONBOARDING_WIZARD_COPY.first-issue.
 */

import { useCallback } from "react";

import { PixelAvatar } from "../../../ui/PixelAvatar";
import {
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
              Your first issue
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
            Your team picks this up the moment you walk into the office.
          </p>
        </div>
      </div>

      <div className="office-tour-slide-stage office-tour-slide-stage--first-issue">
        <div className="onboarding-handoff-card" aria-hidden="true">
          <div className="onboarding-handoff-head">
            <span className="onboarding-handoff-avatar">
              <PixelAvatar slug="ceo" size={24} />
            </span>
            <span className="onboarding-handoff-who">
              <span className="onboarding-handoff-name">CEO</span>
              <span className="onboarding-handoff-handle">@ceo</span>
            </span>
            <span className="onboarding-handoff-route">Direct message</span>
          </div>
          <div className="onboarding-handoff-bubble">
            {answers.firstIssue.trim() ||
              "Write the first thing you want your office to handle."}
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
