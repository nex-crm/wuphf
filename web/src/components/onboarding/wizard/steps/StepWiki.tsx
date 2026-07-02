/**
 * StepWiki — wizard step 02, "Your company brain."
 *
 * Explains the company brain through the RevOps lens: it holds the operator's
 * CRM rules and playbooks (tiering, deal stages, dedupe policy), and agents
 * read them as first-class context before they touch a record.
 *
 * The stage visual is a rendered clip (web/public/media/onboarding/
 * knowledge-base.gif): a "wuphf · company brain" window where the playbook
 * pages light up as they are captured. The clip is a self-contained product
 * window, so it reads correctly on every onboarding page theme.
 *
 * Informational step plus the one optional input on this page: the
 * "Power the company brain" section, where the user can hand the brain an
 * OpenAI key (the recommended embedder), see the local Ollama alternative, or
 * stay on the no-setup keyword default. Advancing is never gated on it. Copy
 * from ONBOARDING_WIZARD_COPY.wiki and ONBOARDING_EMBEDDING_COPY.
 */

import { EmbeddingChoice } from "../EmbeddingChoice";
import {
  ONBOARDING_WIZARD_COPY,
  type OnboardingWizardStepProps,
} from "../wizardSteps";

const COPY = ONBOARDING_WIZARD_COPY.wiki;

export function StepWiki({ active }: OnboardingWizardStepProps) {
  return (
    <div
      className="office-tour-slide office-tour-slide-wiki"
      data-active={active}
      data-testid="onboarding-step-wiki"
    >
      <div className="office-tour-slide-copy">
        <p className="office-tour-slide-eyebrow">{COPY.eyebrow}</p>
        <h2 className="office-tour-slide-headline office-tour-slide-headline--serif">
          {COPY.headline}
        </h2>
        <p className="office-tour-slide-body">{COPY.body}</p>
        <p className="office-tour-slide-caption">
          Your agents read these rules before they merge an account, route a
          lead, or close a stale opportunity.
        </p>

        <EmbeddingChoice />
      </div>

      <div className="office-tour-slide-stage office-tour-slide-stage--wiki">
        <picture>
          <source
            srcSet="/media/onboarding/knowledge-base-still.png"
            media="(prefers-reduced-motion: reduce)"
          />
          <img
            className="onboarding-wizard-clip"
            src="/media/onboarding/knowledge-base.gif"
            width={800}
            height={680}
            alt="A company brain: CRM hygiene playbook, account tiering, deal stage definitions, lead routing rules, duplicate merge policy, and stale opportunity thresholds, each captured once for every agent to read before acting."
            loading="lazy"
            decoding="async"
          />
        </picture>
      </div>
    </div>
  );
}
