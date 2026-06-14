/**
 * useOnboardingWizard — the wizard's state machine and seed orchestrator.
 *
 * Owns:
 *   - The working `answers` object and an immutable patch setter.
 *   - The current step index plus next / back / goTo navigation.
 *   - `canAdvance` gating per step (team needs a blueprint or a named agent;
 *     first-issue needs text).
 *   - The blueprint roster fetched once from GET /onboarding/blueprints.
 *
 * On FINISH it runs the seed contract (verified against
 * internal/onboarding/handlers.go + internal/team/broker_onboarding.go):
 *
 *   1. Persist the load-bearing identity fields via POST /onboarding/answer.
 *      The broker reads `company_name` back out of the persisted Partial /
 *      FormAnswers at complete time (it is NOT in the complete body), so this
 *      MUST happen first. owner_name / owner_role are persisted the same way
 *      when collected.
 *   2. POST /onboarding/complete { task: firstIssue, skip_task: false,
 *      blueprint: blueprintId, agents: pickedAgents }. The broker seeds the
 *      team from that blueprint (or synthesizes one when blueprint is empty),
 *      honors the agents filter, posts the first CEO turn, and flips
 *      onboarded=true. An already_completed response is treated as success.
 *   3. Seed the home composer with the first issue (pendingComposerDraft on the
 *      app store, keyed to HOME_COMPOSER_DRAFT_CHANNEL, which the home
 *      TaskComposer consumes on landing) so the office opens with the issue
 *      ready to send.
 *   4. Call the caller-supplied onComplete() to flip RootRoute into the office.
 *
 * Errors surface on `error` (never silently swallowed); the caller renders
 * them and re-enables Finish so the user can retry.
 */

import { useCallback, useEffect, useMemo, useState } from "react";

import {
  completeOnboarding,
  fetchBlueprints,
  postOnboardingAnswer,
  postOnboardingProgress,
} from "../../../api/onboarding";
import {
  isValidEmail,
  recordOnboardingEmailCaptured,
  setAnalyticsConsent,
  track,
} from "../../../lib/analytics";
import { HOME_COMPOSER_DRAFT_CHANNEL, useAppStore } from "../../../stores/app";
import type { BlueprintOption } from "./types";
import { toBlueprintOption } from "./types";
import {
  ONBOARDING_FIRST_ISSUE_EXAMPLE,
  ONBOARDING_WIZARD_STEP_IDS,
  type OnboardingAnswers,
  type OnboardingWizardStepId,
} from "./wizardSteps";

/**
 * Persist the load-bearing identity fields before completing onboarding.
 *
 * HandleComplete derives the seed company name from the persisted Partial
 * (Partial.Answers["identity"]["company_name"] in internal/onboarding/
 * handlers.go), NOT from the complete body — so the /onboarding/progress
 * step="identity" write is the load-bearing one. We mirror it into FormAnswers
 * via /onboarding/answer so every FormAnswers reader (workspace-registry sync,
 * scratch wiki seed) sees the same name. Owner fields go through
 * /onboarding/answer and also ride the complete body. Each await lets a write
 * failure bubble to the caller's catch rather than silently dropping the value.
 */
async function persistOnboardingIdentity(
  companyName: string,
  ownerName: string,
  ownerRole: string,
  email: string,
): Promise<void> {
  if (companyName) {
    await postOnboardingProgress("identity", { company_name: companyName });
    await postOnboardingAnswer("company_name", companyName);
  }
  if (ownerName) await postOnboardingAnswer("owner_name", ownerName);
  if (ownerRole) await postOnboardingAnswer("owner_role", ownerRole);
  // The email is persisted locally regardless of the keep-in-touch consent —
  // consent only gates the remote send (handled in runFinish), not the local
  // record of who the founder is. Only a well-formed address is stored.
  if (email && isValidEmail(email))
    await postOnboardingAnswer("owner_email", email);
}

/**
 * Attach the welcome-step email to the PostHog person, but only when the user
 * left the keep-in-touch box checked and the address is well-formed. This is
 * the single PII egress point; it is dormant unless a PostHog project key is
 * configured (see lib/analytics). Fire-and-forget, so it never blocks the office.
 */
function maybeRecordOnboardingEmail(email: string, keepInTouch: boolean): void {
  if (keepInTouch && isValidEmail(email)) {
    recordOnboardingEmailCaptured(email);
  }
}

/** Initial answers. The first issue is prefilled with the RevOps example. */
function initialAnswers(): OnboardingAnswers {
  return {
    companyName: "",
    ownerName: "",
    ownerRole: "",
    email: "",
    keepInTouch: true,
    blueprintId: "",
    pickedAgents: [],
    startFromScratch: false,
    agentName: "",
    agentInstructions: "",
    firstIssue: ONBOARDING_FIRST_ISSUE_EXAMPLE,
    telemetryConsent: true,
    recordingConsent: true,
  };
}

export interface UseOnboardingWizardResult {
  /** The current step index (0-based) into ONBOARDING_WIZARD_STEP_IDS. */
  index: number;
  /** The current step id. */
  stepId: OnboardingWizardStepId;
  /** Total number of steps. */
  total: number;
  /** True when on the last step (Finish, not Next). */
  isLast: boolean;
  /** The working answers. */
  answers: OnboardingAnswers;
  /** Merge-patch the answers immutably. */
  setAnswers: (patch: Partial<OnboardingAnswers>) => void;
  /** The blueprint roster options (empty while loading or on error). */
  blueprints: BlueprintOption[];
  /** Whether the user may advance from the current step. */
  canAdvance: boolean;
  /** Advance one step (no-op on the last step; use finish there). */
  next: () => void;
  /** Go back one step (no-op on the first step). */
  back: () => void;
  /** Jump to a specific step index (clamped to range). */
  goTo: (target: number) => void;
  /** True while the FINISH seed sequence is in flight. */
  seeding: boolean;
  /** The last seed error, surfaced to the user. Null when clear. */
  error: string | null;
  /** Run the FINISH seed sequence with the written first issue. */
  finish: () => void;
  /** Seed the office with no first issue and land in it to explore first. */
  skip: () => void;
}

/**
 * @param onComplete called once the office has been seeded successfully, so
 *   the caller (RootRoute) flips into the office Shell.
 */
export function useOnboardingWizard(
  onComplete: () => void,
): UseOnboardingWizardResult {
  const [index, setIndex] = useState(0);
  const [answers, setAnswersState] =
    useState<OnboardingAnswers>(initialAnswers);
  const [blueprints, setBlueprints] = useState<BlueprintOption[]>([]);
  const [seeding, setSeeding] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const total = ONBOARDING_WIZARD_STEP_IDS.length;
  const lastIndex = total - 1;
  const stepId = ONBOARDING_WIZARD_STEP_IDS[index];
  const isLast = index >= lastIndex;

  const setAnswers = useCallback((patch: Partial<OnboardingAnswers>) => {
    setAnswersState((current) => ({ ...current, ...patch }));
  }, []);

  // Fetch the blueprint roster once. A failure is non-fatal: the team step
  // falls back to the scratch path, so we keep blueprints empty and let the
  // user proceed rather than blocking onboarding on a roster fetch.
  useEffect(() => {
    let cancelled = false;
    fetchBlueprints()
      .then((summaries) => {
        if (cancelled) return;
        setBlueprints(summaries.map(toBlueprintOption));
      })
      .catch(() => {
        if (!cancelled) setBlueprints([]);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Per-step advance gate. The team step needs a chosen blueprint OR a named
  // first agent (the "I will set this up later" escape sets neither and uses
  // the scratch path, so that escape is wired in the host, not here). The
  // first-issue step needs non-empty text. Every other step is informational.
  const canAdvance = useMemo(() => {
    switch (stepId) {
      case "team":
        return (
          answers.blueprintId.trim() !== "" ||
          answers.startFromScratch ||
          answers.agentName.trim() !== ""
        );
      case "first-issue":
        return answers.firstIssue.trim() !== "";
      default:
        return true;
    }
  }, [
    stepId,
    answers.blueprintId,
    answers.startFromScratch,
    answers.agentName,
    answers.firstIssue,
  ]);

  const next = useCallback(() => {
    setIndex((current) => Math.min(current + 1, lastIndex));
  }, [lastIndex]);

  const back = useCallback(() => {
    setIndex((current) => Math.max(current - 1, 0));
  }, []);

  const goTo = useCallback(
    (target: number) => {
      setIndex(() => Math.max(0, Math.min(target, lastIndex)));
    },
    [lastIndex],
  );

  // Shared seed sequence for both the primary Finish (with a first issue) and
  // the "skip and explore the office first" path (skipTask = true: complete
  // with no task, so the office opens empty instead of with a queued issue).
  const runFinish = useCallback(
    (skipTask: boolean) => {
      if (seeding) return;
      setSeeding(true);
      setError(null);

      const companyName = answers.companyName.trim();
      const ownerName = answers.ownerName.trim();
      const ownerRole = answers.ownerRole.trim();
      const email = answers.email.trim();
      const firstIssue = answers.firstIssue.trim();
      const blueprintId = answers.blueprintId.trim();
      const telemetryConsent = answers.telemetryConsent;
      const recordingConsent = answers.recordingConsent;

      async function run(): Promise<void> {
        // 1. Persist identity so the office seed reads it back at complete time.
        //    The email rides along here as owner_email (stored locally always).
        await persistOnboardingIdentity(
          companyName,
          ownerName,
          ownerRole,
          email,
        );

        // 2. Seed the team + post the first CEO turn + flip onboarded=true.
        //    blueprint empty => scratch path; agents filters the roster.
        //    owner_name / owner_role are also sent on the complete body so a
        //    fresh broker persists them to config even if the answer writes
        //    above raced; the broker merges, it does not require them.
        const result = await completeOnboarding({
          task: skipTask ? "" : firstIssue,
          skip_task: skipTask,
          blueprint: blueprintId,
          agents: answers.pickedAgents,
          owner_name: ownerName || undefined,
          owner_role: ownerRole || undefined,
          analytics_telemetry_enabled: telemetryConsent,
          analytics_session_recording_enabled: recordingConsent,
        });

        // already_completed is a success case (idempotent re-finish).
        if (result.ok !== true && result.already_completed !== true) {
          throw new Error("Onboarding did not complete");
        }

        // 2a. Apply the consent choice live so an opt-out takes effect this
        //     session and gates the close-out events below (they no-op when
        //     telemetry is turned off — we never send for a user who opted out).
        setAnalyticsConsent({
          telemetry: telemetryConsent,
          recording: recordingConsent,
        });

        // 2b. Funnel close-out + the explicit consent record (both no-ops when
        //     telemetry is off). No content, only counts + the chosen flags.
        track("onboarding_completed", {
          blueprint_id: blueprintId,
          agent_count: answers.pickedAgents.length,
          skipped_first_task: skipTask,
          telemetry_consent: telemetryConsent,
          recording_consent: recordingConsent,
        });
        track("analytics_consent_set", {
          channel: "telemetry",
          enabled: telemetryConsent,
          surface: "onboarding",
        });
        track("analytics_consent_set", {
          channel: "recording",
          enabled: recordingConsent,
          surface: "onboarding",
        });

        // 2c. With consent, register the welcome-step email with the collector.
        //     Runs after complete has succeeded so a lead capture never blocks
        //     landing in the office. See maybeRecordOnboardingEmail.
        maybeRecordOnboardingEmail(email, answers.keepInTouch);

        // 3. Seed the home composer so the office opens with the first issue
        //    ready to send. Keyed to HOME_COMPOSER_DRAFT_CHANNEL, the sentinel
        //    the home TaskComposer consumes on landing — the old code seeded the
        //    CEO DM, which the post-restructure home composer never read, so the
        //    issue was silently dropped. Skipped when the user chose to explore
        //    first: no issue, so open clean.
        if (!skipTask) {
          useAppStore
            .getState()
            .setPendingComposerDraft(HOME_COMPOSER_DRAFT_CHANNEL, firstIssue);
        }

        // 4. Hand control back to the caller to mount the office.
        onComplete();
      }

      run().catch((err: unknown) => {
        const message =
          err instanceof Error ? err.message : "Failed to set up your office";
        setError(message);
        setSeeding(false);
      });
    },
    [
      seeding,
      answers.companyName,
      answers.ownerName,
      answers.ownerRole,
      answers.email,
      answers.keepInTouch,
      answers.firstIssue,
      answers.blueprintId,
      answers.pickedAgents,
      answers.telemetryConsent,
      answers.recordingConsent,
      onComplete,
    ],
  );

  /** Primary Finish: seed the office with the written first issue. */
  const finish = useCallback(() => runFinish(false), [runFinish]);

  /** Skip the first issue and land in the office to explore it first. */
  const skip = useCallback(() => runFinish(true), [runFinish]);

  return {
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
    goTo,
    seeding,
    error,
    finish,
    skip,
  };
}
