/**
 * wizardSteps: single source of truth for the visual onboarding wizard.
 *
 * This module owns the step CONTRACT, the step ORDER, and the per-step COPY.
 * The wizard host (`OnboardingWizard.tsx`) and the five step screens under
 * `./steps/*` (authored by a sibling task) both conform to the ids, the
 * `OnboardingWizardStepProps` shape, and the copy constants declared here, so
 * neither side can drift from the other.
 *
 * The wizard is the visual, stepped onboarding that replaces the thin CEO
 * chat: it educates the user with a persistent mock office and creates their
 * team BEFORE they land in the real office. The office Shell mounts only once
 * the wizard finishes and the broker has seeded the team via
 * POST /onboarding/complete.
 *
 * All copy is WUPHF voice: no em-dashes, no contractions, Oxford comma, dry
 * Office-show humor where it fits. Keep it in one reviewable place so changing
 * a headline means editing this file, not hunting through JSX.
 */

import type { BlueprintOption } from "./types";

/** Ordered step ids. The wizard always runs meet → wiki → team → ship → first-issue. */
export type OnboardingWizardStepId =
  | "meet"
  | "wiki"
  | "team"
  | "ship"
  | "first-issue";

/**
 * Ordered step ids. Index in this array is the step's position and the source
 * of the progress-dot order and the "01 / 05" marker. Do not reorder without
 * updating the copy block below.
 */
export const ONBOARDING_WIZARD_STEP_IDS: OnboardingWizardStepId[] = [
  "meet",
  "wiki",
  "team",
  "ship",
  "first-issue",
];

/**
 * The answers the wizard collects across its steps. This is the wizard's
 * client-side working state; `useOnboardingWizard` persists the load-bearing
 * fields (company name, owner) into the broker's Partial / FormAnswers via
 * POST /onboarding/answer, then forwards blueprint + agents + the first issue
 * to POST /onboarding/complete.
 *
 * - `companyName`   the office / company name. Persisted into Partial so the
 *                   broker can read it back at complete time (seed contract).
 * - `ownerName`     optional founder name, persisted via /onboarding/answer.
 * - `ownerRole`     optional founder role, persisted via /onboarding/answer.
 * - `email`         optional founder email captured on the welcome step.
 *                   Persisted locally as owner_email; attached to the PostHog
 *                   person at finish ONLY when `keepInTouch` is left checked.
 * - `keepInTouch`   consent for the remote email send. Defaults to true; the
 *                   email is still stored locally when it is unchecked.
 * - `blueprintId`   the picked starter roster id, or "" for the scratch path.
 * - `pickedAgents`  the agent slugs kept from the blueprint roster.
 * - `agentName`     the name briefed for the first agent (team step).
 * - `agentInstructions` what that agent does (team step).
 * - `firstIssue`    the text of the first issue, prefilled with the RevOps
 *                   CRM-audit example.
 */
export interface OnboardingAnswers {
  companyName: string;
  ownerName: string;
  ownerRole: string;
  email: string;
  keepInTouch: boolean;
  blueprintId: string;
  pickedAgents: string[];
  /**
   * True when the user explicitly chose "Start from scratch" instead of a
   * pack. The seed treats an empty blueprintId as the scratch path (it
   * synthesizes a founding team), but we track the deliberate choice so the
   * advance gate can let the user proceed with no pack selected.
   */
  startFromScratch: boolean;
  agentName: string;
  agentInstructions: string;
  firstIssue: string;
  /**
   * Product-analytics consent, two independent channels, both default ON.
   * `telemetryConsent` gates anonymous usage events; `recordingConsent` gates
   * session replays that mask typed text. Persisted to config via /onboarding/complete
   * and applied live on finish. See docs/specs/product-analytics.md.
   */
  telemetryConsent: boolean;
  recordingConsent: boolean;
}

/**
 * Props every step screen receives from the wizard host. The host owns the
 * answers state and passes a patch setter so steps update one or more fields
 * without owning the whole object.
 *
 * - `active`     true when this step is the visible one. Steps replay their
 *                entrance animation when `active` flips true (the host
 *                remounts via a stable key, mirroring the tour pattern).
 * - `answers`    the current working answers.
 * - `setAnswers` merge-patch the answers (immutable update in the host).
 * - `blueprints` the blueprint roster options fetched from
 *                GET /onboarding/blueprints. Empty while loading or on error.
 */
export interface OnboardingWizardStepProps {
  active: boolean;
  answers: OnboardingAnswers;
  setAnswers: (patch: Partial<OnboardingAnswers>) => void;
  blueprints: BlueprintOption[];
}

/**
 * The prefilled first-issue example. RevOps framing: WUPHF operates on the
 * user's CRM, it is not a CRM itself. This is the same example the office tour
 * finish handoff used, kept verbatim so the two surfaces stay in lockstep.
 */
export const ONBOARDING_FIRST_ISSUE_EXAMPLE =
  "Audit our CRM for duplicate accounts, deals missing an owner, and opportunities with no activity in 30 days, then propose a cleanup plan";

/**
 * Per-step copy. `eyebrow` is the small-caps kicker above the headline,
 * `headline` is the serif title, `body` is the supporting paragraph. All
 * strings are WUPHF voice. Verbatim source of truth for the step screens.
 */
export const ONBOARDING_WIZARD_COPY: Record<
  OnboardingWizardStepId,
  {
    eyebrow: string;
    headline: string;
    body: string;
  }
> = {
  meet: {
    eyebrow: "WELCOME TO THE OFFICE",
    headline: "Meet WUPHF.",
    body: "WUPHF is an office of AI agents that work on your behalf. They claim work, they ship, and they actually answer your messages. Watch your office assemble itself on the right.",
  },
  wiki: {
    eyebrow: "YOUR KNOWLEDGE BASE",
    headline: "Write the rules once.",
    body: "Your wiki is the team's shared brain. Capture your RevOps rules a single time, account tiering, deal stages, and the dedupe policy, and every agent reads them as first-class context before it touches a record.",
  },
  team: {
    eyebrow: "YOUR STARTING TEAM",
    headline: "Pick a team pack.",
    body: "Each pack is a ready-made RevOps team. Pick one and you are set. Trim who you do not need, or add a custom agent only if you want to.",
  },
  ship: {
    eyebrow: "HOW WORK SHIPS",
    headline: "File it. They ship it.",
    body: "Mention an agent with @, hand off a problem, and the work fans out into tasks across the team while you watch. The ship lands back in a channel you can see.",
  },
  "first-issue": {
    eyebrow: "WRITE YOUR FIRST ISSUE",
    headline: "Give your team something to do.",
    body: "Write the first thing you want your office to handle. We prefilled a CRM cleanup so your team has real work the moment you walk in. Edit it, or write your own.",
  },
};

/**
 * Email-capture copy. The email is optional and never gates advancing: it is a
 * "keep in touch" ask, not a signup wall. The consent line is the verbatim
 * promise we make about how the address is used, so it lives here as the single
 * source of truth. WUPHF voice: no contractions, no em-dashes, Oxford comma.
 */
export const ONBOARDING_EMAIL_COPY = {
  /** Field label on the welcome (meet) step. */
  label: "Your email",
  /** Placeholder for the email input. */
  placeholder: "you@company.com",
  /** Hint under the field. Sets the expectation before anyone types. */
  hint: "Optional. We use it to keep you posted on WUPHF, and nothing else.",
  /**
   * Non-blocking warning shown under the field when the value is non-empty but
   * not a valid address. The field never gates advancing, so this nudges rather
   * than blocks: an invalid email is simply dropped, and this says so.
   */
  invalid:
    "That does not look like an email. Leave it blank, or fix it to stay in touch.",
  /** Consent checkbox label on the final step. Checked by default. */
  consent:
    "Keep me posted on WUPHF. It is source-available and built in the open, and we would love to learn what to build next. No spam, we promise.",
} as const;

/**
 * Analytics-consent copy for the final step. Two honest, independent toggles,
 * both checked by default. The copy states plainly that analytics never
 * collects content and that recordings mask everything you type, so the
 * promise is visible at the point of consent. Single source of truth;
 * mirrored in the README.
 */
export const ONBOARDING_ANALYTICS_CONSENT_COPY = {
  heading: "Help improve WUPHF",
  note: "Both are optional, on by default, and easy to change anytime in Settings. Analytics never collects your content, and recordings mask everything you type.",
  telemetryLabel:
    "Share anonymous product analytics. Counts and shapes of what you do, never the content.",
  recordingLabel:
    "Allow session recordings. We mask everything you type — passwords, keys, form fields — and capture layout, clicks, and navigation to fix rough edges.",
} as const;

/**
 * Semantic-memory copy for the wiki step. The wiki is the shared brain, so this
 * is where we offer the embedder that powers recall by meaning. The backend's
 * EnsureBrain auto-selects in priority order (OpenAI key, then local Ollama,
 * then keyword), so this section recommends the key, presents the alternatives
 * in that same order, and reflects the resulting state. It is always optional:
 * keyword search works with zero setup, so onboarding is never blocked here.
 * WUPHF voice: no contractions, no em-dashes, Oxford comma. Single source of
 * truth; the section component reads only from here.
 */
export const ONBOARDING_EMBEDDING_COPY = {
  heading: "Power semantic memory",
  note: "Semantic memory lets your agents find a rule by meaning, not just by an exact word match. Add an OpenAI key for the best recall, or start on keyword search and upgrade whenever you like.",
  // Primary: the recommended OpenAI key.
  openaiLabel: "OpenAI API key",
  openaiRecommended: "Recommended",
  openaiHint: "Best quality. One key powers chat and memory.",
  openaiPlaceholder: "sk-...",
  openaiSet: "Semantic memory is on, powered by OpenAI embeddings.",
  saveKey: "Save key",
  savingKey: "Saving…",
  saveError:
    "We could not save that key. Check it and try again, or continue on keyword search.",
  // Alternatives, shown only while no key is set, in EnsureBrain priority order.
  alternativesLabel: "No key? You have two other ways:",
  ollamaTitle: "Local embeddings (Ollama)",
  ollamaAvailable: "Free, on-device, and no API key.",
  // ollama_model is interpolated by the section; this is the prose around it.
  ollamaSetupPrefix: "Install Ollama and run ",
  ollamaSetupSuffix: " to turn this on.",
  ollamaModelFallback: "ollama pull nomic-embed-text",
  keywordTitle: "Keyword search",
  keywordHint: "Works now, no setup at all. Upgrade anytime.",
  // The resulting-state pill. The label plus one of the three backend names.
  statusLabel: "Semantic memory:",
  statusOpenAI: "OpenAI",
  statusOllama: "Local (Ollama)",
  statusKeyword: "Keyword",
  // A small action on the Ollama alternative that signals "I want the local
  // path", which surfaces the gbrain install affordance below.
  ollamaChoose: "Use local embeddings",
  // The gbrain install affordance. Shown only when the user wants a semantic
  // path and gbrain is not installed yet. Explicit, one-time consent.
  install: {
    // Consent line. Names exactly what will be installed, on this machine.
    consent:
      "Semantic memory runs on gbrain. Set it up now? This installs gbrain (and Bun, its runtime) on this machine.",
    cta: "Set up semantic memory",
    // While the background installer runs.
    installing: "Setting up gbrain",
    installingHint:
      "This runs in the background. You can keep going, and it will finish on its own.",
    // Shown before the broker emits its first progress line.
    progressPending: "Starting up",
    // The ready state, if the install finishes before gbrain_installed flips.
    installed: "Semantic memory is ready. gbrain is installed.",
    // The error state: the reason (or a generic line), then the keyword
    // fallback, then a retry.
    errorFallback: "We could not set up gbrain just now.",
    keywordFallback: "Using keyword search for now.",
    retry: "Try again",
  },
} as const;

/** UI chrome labels for the wizard host. WUPHF voice, no contractions. */
export const ONBOARDING_WIZARD_LABELS = {
  /** Accessible label for the wizard dialog surface. */
  dialog: "Set up your office",
  /** Back button. */
  back: "Back",
  /** Advance button (non-final steps). */
  next: "Next",
  /** Final-step primary CTA: deposits the user mid-action, not "Done". */
  finish: "Write your first issue",
  /**
   * Subtle escape on the team step that maps to the scratch / skip path. The
   * wizard is still required onboarding (no Esc, no skip-all); this and the
   * first-issue skip below are the only two affordances that advance without
   * the step's normal input.
   */
  teamSkip: "I will set this up later",
  /**
   * First-issue escape: seed the office with no queued issue and land in it to
   * look around first. Maps to the broker's skip_task path.
   */
  firstIssueSkip: "Skip and explore the office first",
  /** Shown while the broker seeds the office after Finish. */
  seeding: "Setting up your office…",
} as const;
