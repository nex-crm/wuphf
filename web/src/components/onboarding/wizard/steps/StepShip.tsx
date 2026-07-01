/**
 * StepShip — wizard step 04, "How work ships."
 *
 * Visualizes the issue → tasks → ship loop with the RevOps framing: a mock
 * @revops issue types into a composer, fans out into a task per agent, and
 * lands back in a visible channel.
 *
 * The stage visual is a rendered Remotion clip (web/public/media/onboarding/
 * work-ships.gif): a "wuphf · #revops" window where the issue is filed and the
 * tasks fan out. The clip is a self-contained product window, so it reads
 * correctly on every onboarding page theme.
 *
 * Informational step (no inputs, no advance gate). Copy from
 * ONBOARDING_WIZARD_COPY.ship.
 */

import {
  ONBOARDING_WIZARD_COPY,
  type OnboardingWizardStepProps,
} from "../wizardSteps";

const COPY = ONBOARDING_WIZARD_COPY.ship;

export function StepShip({ active }: OnboardingWizardStepProps) {
  return (
    <div
      className="office-tour-slide office-tour-slide-issues"
      data-active={active}
      data-testid="onboarding-step-ship"
    >
      <div className="office-tour-slide-copy">
        <p className="office-tour-slide-eyebrow">{COPY.eyebrow}</p>
        <h2 className="office-tour-slide-headline office-tour-slide-headline--serif">
          {COPY.headline}
        </h2>
        <p className="office-tour-slide-body">{COPY.body}</p>
        <p className="office-tour-slide-caption">
          You write one line. The team cuts it into tasks, picks them up in
          parallel, and lands the result back in a channel you can watch.
        </p>
      </div>

      <div className="office-tour-slide-stage office-tour-slide-stage--issues">
        <picture>
          <source
            srcSet="/media/onboarding/work-ships-still.png"
            media="(prefers-reduced-motion: reduce)"
          />
          <img
            className="onboarding-wizard-clip"
            src="/media/onboarding/work-ships.gif"
            width={1000}
            height={760}
            alt="An issue filed in the #general channel: a human asks for a CRM audit, @crm-auditor spins up and runs the workflow step by step (duplicate scan, owner gaps, 30-day stale), and the finished cleanup plan lands back in #general."
            loading="lazy"
            decoding="async"
          />
        </picture>
      </div>
    </div>
  );
}
