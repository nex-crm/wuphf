/**
 * OnboardingWizard — full-viewport stepped onboarding host.
 *
 * This is the visual, stepped onboarding that replaces the thin CEO chat. It
 * educates the user with a persistent mock office and creates their team
 * BEFORE the real office mounts. RootRoute renders it after the provider
 * picker (PrePickScreen) and mounts the office Shell only once `onComplete`
 * fires.
 *
 * Layout mirrors the reference product's persistent-mock-window pattern: each
 * step renders a copy column plus a visual stage (a rendered product-window
 * clip or a live mock), the stage carries any inputs the step collects, and a
 * footer carries Back / progress dots / Next-or-Finish. A "01 / 05" marker
 * sits top-left. There is NO skip-all affordance: this is required
 * onboarding, not a dismissible tour, so Esc does nothing here. The one
 * permitted escape is the team step's "I will set this up later", which maps
 * to the scratch path (no blueprint, default first agent) and advances.
 *
 * The host owns only the shell + step navigation + the finish wiring. The five
 * step screens (under ./steps/*) own their own content and visuals and conform
 * to OnboardingWizardStepProps. Step-to-step morphing reuses the office tour's
 * View-Transitions-or-synchronous pattern, gated behind prefers-reduced-motion.
 *
 * Spec: docs/specs/office-onboarding-uplift.md.
 */

import { type ComponentType, useCallback, useEffect } from "react";

import { track } from "../../../lib/analytics";
import { MOD_KEY } from "../../ui/Kbd";
import { BtnLabel, EnterHint } from "./components";
import { StepFirstIssue } from "./steps/StepFirstIssue";
import { StepMeet } from "./steps/StepMeet";
import { StepShip } from "./steps/StepShip";
import { StepTeam } from "./steps/StepTeam";
import { StepWiki } from "./steps/StepWiki";
import { useOnboardingWizard } from "./useOnboardingWizard";
import {
  ONBOARDING_WIZARD_LABELS,
  ONBOARDING_WIZARD_STEP_IDS,
  type OnboardingWizardStepId,
  type OnboardingWizardStepProps,
} from "./wizardSteps";

interface OnboardingWizardProps {
  /** Called once the office is seeded; the caller mounts the office Shell. */
  onComplete: () => void;
}

/**
 * Id → step screen. Each conforms to OnboardingWizardStepProps. The navigation
 * order comes from ONBOARDING_WIZARD_STEP_IDS, not this map.
 */
const STEPS: Record<
  OnboardingWizardStepId,
  ComponentType<OnboardingWizardStepProps>
> = {
  meet: StepMeet,
  wiki: StepWiki,
  team: StepTeam,
  ship: StepShip,
  "first-issue": StepFirstIssue,
};

/**
 * Run `update` inside a View Transition when the API exists and motion is
 * allowed; otherwise call it synchronously. Mirrors OfficeTour.runWithTransition
 * so the wizard's step morph reads identically to the tour's slide morph.
 */
function runWithTransition(update: () => void): void {
  if (typeof document === "undefined") {
    update();
    return;
  }
  const prefersReducedMotion =
    typeof window !== "undefined" &&
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  const doc = document as Document & {
    startViewTransition?: (cb: () => void) => unknown;
  };

  if (prefersReducedMotion || typeof doc.startViewTransition !== "function") {
    update();
    return;
  }
  doc.startViewTransition(update);
}

/** A text-entry target owns its own arrow keys (the caret needs them). */
function isEditableTarget(target: HTMLElement | null): boolean {
  const tag = target?.tagName;
  return (
    tag === "INPUT" || tag === "TEXTAREA" || target?.isContentEditable === true
  );
}

/** The navigation a key press maps to, or null when the key is not ours. */
type WizardKeyAction = "primary" | "next" | "back" | null;

interface WizardKeyContext {
  canAdvance: boolean;
  isLast: boolean;
  canGoBack: boolean;
  primaryEnabled: boolean;
}

/**
 * Enter is the primary "continue" shortcut, except when a button/link is
 * focused (native activation handles it) or inside a textarea without Cmd/Ctrl
 * (so the first-issue field keeps plain Enter for newlines).
 */
function enterAction(
  event: KeyboardEvent,
  tag: string | undefined,
  primaryEnabled: boolean,
): WizardKeyAction {
  if (tag === "BUTTON" || tag === "A") return null;
  if (tag === "TEXTAREA" && !(event.metaKey || event.ctrlKey)) return null;
  return primaryEnabled ? "primary" : null;
}

/**
 * Decide what a key press does, without touching the DOM beyond the event. Kept
 * module-level and flat so the keyboard effect stays trivial. Returns null for
 * keys the wizard does not own (so the caller leaves them to the browser).
 * Arrows navigate steps but are ignored while a text field has focus; Esc is
 * deliberately unmapped, since this is required onboarding, not dismissible.
 */
function decideWizardKey(
  event: KeyboardEvent,
  ctx: WizardKeyContext,
): WizardKeyAction {
  const target = event.target as HTMLElement | null;
  const tag = target?.tagName;

  if (event.key === "Enter") return enterAction(event, tag, ctx.primaryEnabled);
  if (isEditableTarget(target)) return null;
  if (event.key === "ArrowRight") {
    return !ctx.isLast && ctx.canAdvance ? "next" : null;
  }
  if (event.key === "ArrowLeft") {
    return ctx.canGoBack ? "back" : null;
  }
  return null;
}

export function OnboardingWizard({ onComplete }: OnboardingWizardProps) {
  const wizard = useOnboardingWizard(onComplete);
  const {
    index,
    stepId,
    total,
    isLast,
    answers,
    setAnswers,
    blueprints,
    canAdvance,
    next,
    back,
    seeding,
    error,
    finish,
    skip,
  } = wizard;

  // Zero-padded "01 / 05" wayfinding marker, top-left.
  const stepLabel = `${String(index + 1).padStart(2, "0")} / ${String(
    total,
  ).padStart(2, "0")}`;

  // Fire onboarding_started once when the wizard mounts. No-op when dormant.
  useEffect(() => {
    track("onboarding_started");
  }, []);

  const goNext = useCallback(() => {
    track("onboarding_step_completed", { step_id: stepId, step_index: index });
    runWithTransition(next);
  }, [next, stepId, index]);

  const goBack = useCallback(() => {
    runWithTransition(back);
  }, [back]);

  // The team-step escape: take the scratch path (clear any picked blueprint /
  // agent) and advance. This is the only way past the team step without a
  // blueprint or a named agent, and it deliberately does NOT skip the rest of
  // onboarding — the user still writes a first issue.
  const skipTeam = useCallback(() => {
    track("onboarding_step_completed", { step_id: "team", step_index: index });
    setAnswers({ blueprintId: "", pickedAgents: [], agentName: "" });
    runWithTransition(next);
  }, [setAnswers, next, index]);

  // The primary footer button advances on every step except the last, where it
  // becomes the Finish CTA that seeds the office and hands off into a composer.
  const onPrimary = useCallback(() => {
    if (isLast) {
      finish();
    } else if (canAdvance) {
      goNext();
    }
  }, [isLast, canAdvance, finish, goNext]);

  // Computed before the keyboard effect so the effect can gate the Enter
  // shortcut on the same condition the primary button uses.
  const primaryDisabled = seeding || !(isLast || canAdvance);

  // Keyboard: Enter is the primary "continue" shortcut (it mirrors the ⏎ keycap
  // in the primary button), arrows move between steps. Esc is deliberately a
  // no-op — this is required onboarding and cannot be skipped.
  //
  // The handlers never hijack a key that the focused control legitimately owns:
  // arrows are ignored while a text field has focus (the caret needs them), and
  // Enter is ignored when a button/link is focused (native activation handles
  // it) or inside a multiline textarea unless chorded with Cmd/Ctrl (so the
  // first-issue field keeps plain Enter for newlines).
  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      const action = decideWizardKey(event, {
        canAdvance,
        isLast,
        canGoBack: index > 0,
        primaryEnabled: !primaryDisabled,
      });
      if (!action) return;
      event.preventDefault();
      if (action === "primary") onPrimary();
      else if (action === "next") goNext();
      else goBack();
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [isLast, canAdvance, index, primaryDisabled, onPrimary, goNext, goBack]);

  const ActiveStep = STEPS[stepId];
  const primaryLabel = isLast
    ? ONBOARDING_WIZARD_LABELS.finish
    : ONBOARDING_WIZARD_LABELS.next;
  // Precompute the primary button label so the JSX renders a single string,
  // not a ternary the linter reads as a potential leaked render.
  const primaryButtonLabel = seeding
    ? ONBOARDING_WIZARD_LABELS.seeding
    : primaryLabel;
  // The ⏎ keycap only advertises the Enter shortcut when pressing it would
  // actually do something — hidden while seeding or when the step gate blocks.
  const showEnterHint = !primaryDisabled;

  return (
    <section
      className="onboarding-wizard"
      aria-label={ONBOARDING_WIZARD_LABELS.dialog}
      data-step={stepId}
      data-testid="onboarding-wizard"
    >
      <span className="onboarding-wizard-step-marker" aria-hidden="true">
        {stepLabel}
      </span>

      <div className="onboarding-wizard-body">
        {/* key={stepId} remounts the step on change so its entrance animation
            replays; `active` is always true for the mounted step. */}
        <ActiveStep
          key={stepId}
          active={true}
          answers={answers}
          setAnswers={setAnswers}
          blueprints={blueprints}
        />
      </div>

      {error ? (
        <p
          className="onboarding-wizard-error"
          role="alert"
          data-testid="onboarding-wizard-error"
        >
          {error}
        </p>
      ) : null}

      <footer className="onboarding-wizard-footer">
        <div className="onboarding-wizard-footer-side">
          {index > 0 ? (
            <button
              type="button"
              className="btn btn-ghost"
              onClick={goBack}
              disabled={seeding}
              data-testid="onboarding-wizard-back"
            >
              {ONBOARDING_WIZARD_LABELS.back}
            </button>
          ) : (
            <span className="onboarding-wizard-kbd-hint" aria-hidden="true">
              Use <kbd>←</kbd> <kbd>→</kbd> to move
            </span>
          )}
        </div>

        {/* Decorative progress dots, not a tab strip. A single labelled
            role="img" announces "step N of M"; individual dots are hidden. */}
        <div
          className="onboarding-wizard-dots"
          role="img"
          aria-label={`Step ${index + 1} of ${total}`}
        >
          {ONBOARDING_WIZARD_STEP_IDS.map((id, i) => (
            <span
              key={id}
              className={`onboarding-wizard-dot${
                i === index ? " onboarding-wizard-dot-active" : ""
              }`}
              aria-hidden="true"
              data-testid={`onboarding-wizard-dot-${id}`}
            />
          ))}
        </div>

        <div className="onboarding-wizard-footer-side onboarding-wizard-footer-side-end">
          {stepId === "team" ? (
            <button
              type="button"
              className="btn btn-ghost"
              onClick={skipTeam}
              disabled={seeding}
              data-testid="onboarding-wizard-team-skip"
            >
              {ONBOARDING_WIZARD_LABELS.teamSkip}
            </button>
          ) : null}
          {stepId === "first-issue" ? (
            <button
              type="button"
              className="btn btn-ghost"
              onClick={skip}
              disabled={seeding}
              data-testid="onboarding-wizard-first-issue-skip"
            >
              {ONBOARDING_WIZARD_LABELS.firstIssueSkip}
            </button>
          ) : null}
          <button
            type="button"
            className="btn btn-primary onboarding-wizard-primary"
            onClick={onPrimary}
            disabled={primaryDisabled}
            data-testid="onboarding-wizard-primary"
          >
            <BtnLabel>{primaryButtonLabel}</BtnLabel>
            {showEnterHint ? (
              <EnterHint
                modifier={stepId === "first-issue" ? MOD_KEY : undefined}
              />
            ) : null}
          </button>
        </div>
      </footer>
    </section>
  );
}
