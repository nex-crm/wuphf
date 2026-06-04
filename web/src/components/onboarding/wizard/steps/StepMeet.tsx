/**
 * StepMeet — wizard step 01, "Meet WUPHF."
 *
 * Sets the metaphor: WUPHF is an office, and a team of agents lives in it. The
 * visual is a mock office coming online, so the first thing the user sees is
 * their (mock) office coming to life.
 *
 * This is also where the office / company name and an optional founder email
 * are collected. The wizard hook persists the name into Partial at finish so
 * the broker reads it back at complete time (the seed contract). Naming the
 * office is not gated: a user can advance without naming it, in which case the
 * broker uses its default. Example chips fill the field with one click to lower
 * friction.
 *
 * The email is a "keep in touch" ask, never a wall: it does not gate advancing.
 * As the welcome step, this is also where the anonymous capture funnel starts
 * (a viewed event on mount, a started event on the first keystroke). The
 * address itself is only attached to the PostHog person later, at finish, and
 * only with consent (see useOnboardingWizard + lib/analytics). Both events are
 * dormant unless a PostHog project key is configured.
 *
 * The stage visual is a rendered Remotion clip (web/public/media/onboarding/
 * meet-office.gif): a "wuphf · office" window where the founding team comes
 * online one agent at a time, each with a presence dot. It is a self-contained
 * product window, so it reads correctly on every onboarding page theme.
 *
 * Reuses the office-tour split + copy primitives so the wizard reads as one
 * continuous arc with the visuals it shares. Copy is pulled from
 * ONBOARDING_WIZARD_COPY.meet (single source of truth).
 */

import { useCallback, useEffect, useRef } from "react";

import {
  recordOnboardingEmailStarted,
  recordOnboardingEmailViewed,
} from "../../../../lib/analytics";
import {
  ONBOARDING_EMAIL_COPY,
  ONBOARDING_WIZARD_COPY,
  type OnboardingWizardStepProps,
} from "../wizardSteps";

const COPY = ONBOARDING_WIZARD_COPY.meet;

/**
 * One-click example office names. Picking one fills the field; the user can
 * still type over it. These are illustrative starter names, not headline copy,
 * so they live with the step rather than in the shared copy module.
 */
const OFFICE_NAME_EXAMPLES = ["Acme RevOps", "Northwind Sales", "Dunder HQ"];

export function StepMeet({
  active,
  answers,
  setAnswers,
}: OnboardingWizardStepProps) {
  const setOfficeName = useCallback(
    (value: string) => {
      setAnswers({ companyName: value });
    },
    [setAnswers],
  );

  // Fire the anonymous "saw the email step" event once, when the welcome step
  // first mounts. The host remounts steps on change, so this runs exactly once
  // per visit to this step. No PII: distinct id + source only.
  useEffect(() => {
    recordOnboardingEmailViewed();
  }, []);

  // The start event fires once, on the first non-empty keystroke in the email
  // field. It signals intent (the user began typing) without sending the
  // partial address. The ref keeps it to a single emit per visit.
  const startedRef = useRef(false);
  const setEmail = useCallback(
    (value: string) => {
      setAnswers({ email: value });
      if (!startedRef.current && value.trim().length > 0) {
        startedRef.current = true;
        recordOnboardingEmailStarted();
      }
    },
    [setAnswers],
  );

  return (
    <div
      className="office-tour-slide office-tour-slide-intro"
      data-active={active}
      data-testid="onboarding-step-meet"
    >
      <div className="office-tour-slide-copy">
        <p className="office-tour-slide-eyebrow">{COPY.eyebrow}</p>
        <h2 className="office-tour-slide-headline office-tour-slide-headline--serif">
          {COPY.headline}
        </h2>
        <p className="office-tour-slide-body">{COPY.body}</p>

        <div className="onboarding-team-field onboarding-meet-name-field">
          <label
            className="onboarding-team-label"
            htmlFor="onboarding-office-name"
          >
            Name your office
          </label>
          <input
            id="onboarding-office-name"
            className="onboarding-team-input"
            type="text"
            value={answers.companyName}
            placeholder="Acme RevOps"
            autoComplete="organization"
            onChange={(event) => setOfficeName(event.target.value)}
            data-testid="onboarding-office-name"
          />
          <div className="onboarding-meet-chips">
            {OFFICE_NAME_EXAMPLES.map((example) => (
              <button
                key={example}
                type="button"
                className="onboarding-meet-chip"
                onClick={() => setOfficeName(example)}
                aria-label={`Use ${example} as the office name`}
                data-testid={`onboarding-office-name-chip-${example}`}
              >
                {example}
              </button>
            ))}
          </div>
          <p className="onboarding-meet-name-hint">
            You can rename it later. We will use a sensible default if you leave
            it blank.
          </p>
        </div>

        <div className="onboarding-team-field onboarding-meet-email-field">
          <label
            className="onboarding-team-label"
            htmlFor="onboarding-owner-email"
          >
            {ONBOARDING_EMAIL_COPY.label}
          </label>
          <input
            id="onboarding-owner-email"
            className="onboarding-team-input"
            type="email"
            value={answers.email}
            placeholder={ONBOARDING_EMAIL_COPY.placeholder}
            autoComplete="email"
            inputMode="email"
            onChange={(event) => setEmail(event.target.value)}
            data-testid="onboarding-owner-email"
          />
          <p className="onboarding-meet-name-hint">
            {ONBOARDING_EMAIL_COPY.hint}
          </p>
        </div>
      </div>

      <div className="office-tour-slide-stage office-tour-slide-stage--intro">
        <picture>
          <source
            srcSet="/media/onboarding/meet-office-still.png"
            media="(prefers-reduced-motion: reduce)"
          />
          <img
            className="onboarding-wizard-clip"
            src="/media/onboarding/meet-office.gif"
            width={868}
            height={620}
            alt="A WUPHF office coming online: the CEO who runs the office, @revops who keeps the CRM clean, and @analyst who watches the funnel, each turning to an online presence as they arrive."
            loading="lazy"
            decoding="async"
          />
        </picture>
      </div>
    </div>
  );
}
