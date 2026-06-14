import { type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { NavArrowLeft, NavArrowRight, Xmark } from "iconoir-react";

import {
  type AgentRequest,
  answerRequest,
  cancelRequest,
  type InterviewOption,
  post,
  postMessage,
} from "../../api/client";
import { useRequests } from "../../hooks/useRequests";
import { track } from "../../lib/analytics";
import { parseApprovalContext } from "../../lib/parseApprovalContext";
import {
  requestOptionNeedsText,
  requestOptionTextHint,
} from "../../lib/requestOptions";
import { directChannelSlug, useAppStore } from "../../stores/app";
import { humanEchoForCeoAnswer } from "../onboarding/humanEcho";
import { useOnboardingDMContext } from "../onboarding/OnboardingDMRoute";
import type { CardStage, CeoSuggestion } from "../onboarding/types";
import { showNotice } from "../ui/Toast";
import { ApprovalContextView } from "./ApprovalContextView";
import { ConnectIntegrationCard } from "./ConnectIntegrationCard";
import { StructuredMessageCard } from "./cards/StructuredMessageCard";

/**
 * Inline interview bar shown above the Composer. Mirrors the TUI behavior:
 * - Shows the current pending request (1/N counter for the queue)
 * - Allows cycling through queued requests with prev/next
 * - Renders option buttons; if the picked option requires custom text,
 *   switches to a text input mode using the option's hint as placeholder
 * - Skip / close cancels the unanswered interview
 *
 *
 * Phase 2 (onboarding-into-office spec) extends this with CEO card kinds:
 * - kind="ceo_form_field": label + text input + submit/skip chips
 * - kind="ceo_chip_row": single-select chip row (blueprint pick)
 * - kind="ceo_checklist": multi-select checklist + submit (team trim)
 * - kind="ceo_team_trim": alias for ceo_checklist with team-specific copy
 * - kind="ceo_scan_chip": async scan status display (read-only)
 *
 * CEO cards are rendered by the CeoCardSection sub-component which reads
 * the PendingSuggestion from OnboardingDMContext. They live above the
 * regular request queue section. Sanitization is enforced by:
 *   1. Backend: sanitizeContextValue in broker_onboarding.go (PR #684)
 *   2. Frontend: all card components render strings as text, never innerHTML
 */
/**
 * Pin rank for the interview-bar queue: 0 = blocking/required asks (the
 * office is waiting on this answer), 1 = plain interviews (an agent asked
 * a question), 2 = everything else (notices, FYIs).
 */
export function requestPinRank(request: AgentRequest): number {
  if (request.blocking === true || request.required === true) return 0;
  if ((request.kind ?? "").toLowerCase() === "interview") return 1;
  return 2;
}

/**
 * Normalize a channel slug for request↔surface comparison. Mirrors the
 * broker rule that an empty channel means #general (normalizeChannelSlug in
 * broker_defaults.go), so a channel-less legacy request scopes to #general
 * instead of silently disappearing.
 */
export function interviewChannelSlug(value: string | null | undefined): string {
  const slug = (value ?? "").trim().toLowerCase();
  return slug === "" ? "general" : slug;
}

interface InterviewBarProps {
  /**
   * Channel slug of the chat this bar belongs to. The request queue is
   * scoped to requests that originated in this channel, so an agent's
   * question only appears in the chat it was asked in — not mirrored onto
   * every surface. `null` (a non-chat surface) shows nothing; cross-channel
   * triage lives in the Inbox.
   */
  channelSlug: string | null;
}

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Existing cognitive complexity is baselined for a focused follow-up refactor.
// biome-ignore lint/complexity/noExcessiveLinesPerFunction: Existing function length is baselined for a focused follow-up refactor.
export function InterviewBar({ channelSlug }: InterviewBarProps) {
  const { pending } = useRequests();
  const queryClient = useQueryClient();
  // OnboardingDMContext.phase is non-empty exactly when the broker CEO
  // onboarding state machine is mid-flow. During that window, all
  // legitimate onboarding prompts come through CeoCardSection via
  // pendingSuggestion (deterministic chip / form-field cards backed by
  // onboarded.json). The operational request queue from useRequests is
  // separate office state that can hold stale leftovers (interview /
  // approval requests from prior office turns) — surfacing those during
  // onboarding leaks the office inbox into the wizard. Suppress the
  // entire queue while onboarding is in progress; CeoCardSection still
  // renders below.
  const { phase: onboardingPhase } = useOnboardingDMContext();
  const isOnboarding =
    typeof onboardingPhase === "string" &&
    onboardingPhase !== "" &&
    onboardingPhase !== "complete";

  // The chat this bar is anchored to. `null` means a non-chat surface
  // (wiki, agents, settings, …) where no request should surface inline.
  const activeChannel =
    channelSlug === null ? null : interviewChannelSlug(channelSlug);

  const queue = useMemo(() => {
    if (isOnboarding || activeChannel === null) return [];
    // Only show requests that originated in the chat the human is looking
    // at. The broker queue is office-wide (useRequests fetches every
    // channel for the Inbox), so without this scope an agent's question in
    // one task channel would block the composer on every other surface.
    const scoped = pending.filter(
      (r) => interviewChannelSlug(r.channel) === activeChannel,
    );
    // Pin order: blocking/required asks first, then real interviews, then
    // notices/FYIs; oldest-first within each class. The old pure
    // created_at sort buried a blocking interview behind a pile of stale
    // delivery notices — the live v3 run never surfaced one for 44
    // minutes while the office stalled behind it.
    const sorted = [...scoped].sort((a, b) => {
      const ra = requestPinRank(a);
      const rb = requestPinRank(b);
      if (ra !== rb) return ra - rb;
      const ta = a.created_at ?? "";
      const tb = b.created_at ?? "";
      return ta.localeCompare(tb);
    });
    return sorted;
  }, [pending, isOnboarding, activeChannel]);

  const [cursor, setCursor] = useState(0);
  const [textMode, setTextMode] = useState<{ option: InterviewOption } | null>(
    null,
  );
  const [customText, setCustomText] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [dismissedIds, setDismissedIds] = useState<Set<string>>(new Set());
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const visible = queue.filter((r) => !dismissedIds.has(r.id));
  const safeCursor = Math.min(cursor, Math.max(visible.length - 1, 0));
  const current = visible[safeCursor] ?? null;

  // Reset transient UI state when the active request changes. Keyed on
  // current?.id so cycling between queued requests (or replacing the
  // active one with a fresh broker push) clears textMode / customText
  // before state from one card leaks into the next.
  const currentId = current?.id ?? null;
  useEffect(() => {
    setTextMode(null);
    setCustomText("");
  }, [currentId]);

  // An agent asked the human for input. Record that it was surfaced so we can
  // measure shown→answered latency and abandonment. No request content.
  useEffect(() => {
    if (currentId) track("interview_shown", { surface: "interview_bar" });
  }, [currentId]);

  useEffect(() => {
    if (textMode && textareaRef.current) {
      textareaRef.current.focus();
    }
  }, [textMode]);

  // When there's no agent interview request, fall back to the CEO card
  // section so the deterministic onboarding chip / form-field input still
  // renders during Phase 2. CeoCardSection returns null on its own when
  // there's no pendingSuggestion either, so this is safe in non-onboarding
  // surfaces (the context default is `phase: undefined`).
  if (!current) return <CeoCardSection />;

  const rawOptions = current.options ?? current.choices ?? [];
  const options = [...rawOptions].sort((a, b) => {
    const ar = a.id === current.recommended_id ? 0 : 1;
    const br = b.id === current.recommended_id ? 0 : 1;
    return ar - br;
  });

  // biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Existing cognitive complexity is baselined for a focused follow-up refactor.
  const submit = async (option: InterviewOption, text?: string) => {
    if (submitting) return;
    setSubmitting(true);
    try {
      await answerRequest(current.id, option.id, text);
      await queryClient.invalidateQueries({ queryKey: ["requests"] });
      setTextMode(null);
      setCustomText("");
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : "Failed to answer";
      showNotice(message, "error");
    } finally {
      setSubmitting(false);
    }
  };

  const handleOption = (option: InterviewOption) => {
    if (requestOptionNeedsText(current, option)) {
      setTextMode({ option });
      setCustomText("");
      return;
    }
    submit(option);
  };

  const handleDismiss = async () => {
    if (submitting) return;
    setSubmitting(true);
    setTextMode(null);
    try {
      await cancelRequest(current.id);
      setDismissedIds((prev) => {
        const next = new Set(prev);
        next.add(current.id);
        return next;
      });
      await queryClient.invalidateQueries({ queryKey: ["requests"] });
      await queryClient.invalidateQueries({ queryKey: ["requests-badge"] });
      showNotice("Request canceled.", "info");
    } catch (err: unknown) {
      const message =
        err instanceof Error ? err.message : "Failed to cancel request";
      showNotice(message, "error");
    } finally {
      setSubmitting(false);
    }
  };

  const handleNext = () =>
    setCursor((i) => Math.min(i + 1, visible.length - 1));
  const handlePrev = () => setCursor((i) => Math.max(i - 1, 0));

  // kind="notice" rows are non-blocking FYIs (delivery acknowledgements,
  // "waiting on you" hints) — NOT questions. Labeling them INTERVIEW with
  // "@x asks" made the bar cry wolf (ICP eval N8: 10+ notices drowned 3
  // real interviews). Notices get a neutral NOTICE badge and drop "asks".
  const isNotice = current.kind === "notice";
  // `connect` requests render the OAuth-driving ConnectIntegrationCard instead
  // of generic option buttons — otherwise "Connect" just answers the request
  // and nothing opens.
  const isConnect = current.kind === "connect";

  return (
    <>
      <CeoCardSection />
      <section className="interview-bar" aria-label="Pending agent request">
        <div className="interview-bar-head">
          <span
            className={`badge ${isNotice ? "badge-neutral" : "badge-yellow"}`}
          >
            {current.blocking ? "BLOCKING" : isNotice ? "NOTICE" : "INTERVIEW"}
          </span>
          {current.kind === "approval" ? (
            <span className="badge badge-orange">EXTERNAL ACTION</span>
          ) : null}
          <span className="interview-bar-from">
            {isNotice
              ? `from @${current.from || "agent"}`
              : `@${current.from || "agent"} asks`}
          </span>
          {current.channel ? (
            <span className="interview-bar-channel">in #{current.channel}</span>
          ) : null}
          <span className="interview-bar-counter">
            {safeCursor + 1}/{visible.length}
          </span>
          <div className="interview-bar-cycle">
            <button
              type="button"
              className="interview-bar-icon-btn"
              onClick={handlePrev}
              disabled={safeCursor === 0}
              aria-label="Previous request"
              title="Previous"
            >
              <NavArrowLeft width={16} height={16} />
            </button>
            <button
              type="button"
              className="interview-bar-icon-btn"
              onClick={handleNext}
              disabled={safeCursor >= visible.length - 1}
              aria-label="Next request"
              title="Next"
            >
              <NavArrowRight width={16} height={16} />
            </button>
          </div>
          <button
            type="button"
            className="interview-bar-close"
            onClick={handleDismiss}
            disabled={submitting}
            aria-label="Dismiss request"
            title="Dismiss"
          >
            <Xmark width={20} height={20} />
          </button>
        </div>

        {isConnect ? (
          <div className="interview-bar-body interview-bar-connect">
            <ConnectIntegrationCard
              request={current}
              submitting={submitting}
              onSkip={() => submit({ id: "skip", label: "Skip" })}
              onDismiss={handleDismiss}
            />
          </div>
        ) : (
          <>
            <div className="interview-bar-body">
              {current.title && current.title !== "Request" ? (
                <div className="interview-bar-title">{current.title}</div>
              ) : null}
              <div className="interview-bar-question">
                {(current.question || "")
                  .replace(/\*\*/g, "")
                  .replace(/^\s*\d+\.\s*/, "")}
              </div>
              {(() => {
                if (current.kind === "approval") {
                  const parsed = parseApprovalContext(current.context);
                  if (parsed) return <ApprovalContextView parsed={parsed} />;
                }
                return current.context ? (
                  <div className="interview-bar-context">{current.context}</div>
                ) : null;
              })()}
            </div>

            {textMode ? (
              <div className="interview-bar-text">
                <textarea
                  ref={textareaRef}
                  className="interview-bar-textarea"
                  placeholder={requestOptionTextHint(current, textMode.option)}
                  value={customText}
                  onChange={(e) => setCustomText(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Escape") {
                      e.preventDefault();
                      setTextMode(null);
                    }
                    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                      e.preventDefault();
                      if (customText.trim())
                        submit(textMode.option, customText.trim());
                    }
                  }}
                  rows={3}
                />
                <div className="interview-bar-text-actions">
                  <button
                    type="button"
                    className="btn btn-ghost btn-sm"
                    onClick={() => setTextMode(null)}
                    disabled={submitting}
                  >
                    Back
                  </button>
                  <button
                    type="button"
                    className="btn btn-primary btn-sm"
                    onClick={() => submit(textMode.option, customText.trim())}
                    disabled={submitting || !customText.trim()}
                  >
                    {submitting
                      ? "Sending..."
                      : `Send as ${textMode.option.label}`}
                  </button>
                </div>
              </div>
            ) : options.length > 0 ? (
              <div
                className={`interview-bar-actions${options.length <= 2 ? " interview-bar-actions-inline" : ""}`}
              >
                {options.map((opt, i) => (
                  <button
                    key={opt.id}
                    type="button"
                    className={`btn btn-sm ${opt.id === current.recommended_id ? "btn-primary" : "btn-ghost"}`}
                    onClick={() => handleOption(opt)}
                    disabled={submitting}
                    title={opt.description}
                  >
                    <span className="interview-bar-opt-num">{i + 1}</span>
                    <span className="interview-bar-opt-label">{opt.label}</span>
                    {requestOptionNeedsText(current, opt) ? (
                      <span className="interview-bar-text-hint"> · type</span>
                    ) : null}
                  </button>
                ))}
              </div>
            ) : (
              <div className="interview-bar-empty">No options provided.</div>
            )}
          </>
        )}
      </section>
    </>
  );
}

// ── CEO card section (Phase 2 onboarding) ─────────────────────────────────

/**
 * CeoCardSection renders the current PendingSuggestion from OnboardingDMContext
 * as an interactive CEO card above the regular interview queue.
 *
 * This is a separate exported component so it can be rendered inside the CEO
 * DM's InterviewBar slot, and tested independently.
 *
 * The card is only visible when:
 *   1. The OnboardingDMRoute provides a non-null pendingSuggestion
 *   2. The card has not yet been committed (stage !== "committed")
 *
 * POST /onboarding/answer wire shape: { field: string, value: unknown }
 */
export function CeoCardSection() {
  const { phase, pendingSuggestion } = useOnboardingDMContext();
  const queryClient = useQueryClient();
  const setOnboardingComplete = useAppStore((s) => s.setOnboardingComplete);
  const [stage, setStage] = useState<CardStage>("pending");
  const [committedValue, setCommittedValue] = useState<
    string | string[] | undefined
  >(undefined);

  // Reset stage when suggestion changes (new question arrived).
  const suggestionId = pendingSuggestion?.id ?? null;
  useEffect(() => {
    setStage("pending");
    setCommittedValue(undefined);
  }, [suggestionId]);

  if (!pendingSuggestion || stage === "committed") return null;

  const submitAnswer = async (field: string, value: unknown) => {
    if (stage === "submitting") return;
    setStage("submitting");
    try {
      if (shouldPersistOnboardingAnswer(field)) {
        await post("/onboarding/answer", { field, value });
      }
      // Mirror the human's committed answer into the CEO DM as a chat
      // bubble. The /messages endpoint persists it the same as any typed
      // message so a tab reload still shows the transcript. See #978.
      const echo = humanEchoForCeoAnswer(pendingSuggestion, field, value);
      if (echo !== null) {
        // Detached: don't await the echo work. The echo is best-effort UI
        // sugar (mirror the human's answer back in chat); awaiting it
        // would let a slow/hung /messages call freeze the wizard in
        // "submitting" even though /onboarding/answer has already
        // committed the real state. CodeRabbit on PR #988.
        void postMessage(echo, directChannelSlug("ceo"))
          .then(() =>
            queryClient.invalidateQueries({
              queryKey: ["messages", directChannelSlug("ceo")],
            }),
          )
          .catch((echoErr: unknown) => {
            // The next CEO message will still arrive; the user just loses
            // the visible mirror of their own answer for that turn.
            console.warn("onboarding: failed to echo human answer", echoErr);
          });
      }
      await advanceOnboardingAfterAnswer(field, value, phase);
      // Refresh onboarding state so the next suggestion appears.
      await queryClient.invalidateQueries({ queryKey: ["onboarding-state"] });
      setCommittedValue(committedOnboardingValue(value));
      setStage("committed");
      if (completesOnboarding(field)) {
        setOnboardingComplete(true);
      }
    } catch (err: unknown) {
      const message =
        err instanceof Error ? err.message : "Failed to send answer";
      showNotice(message, "error");
      setStage("pending");
    }
  };

  const handleSkip = async (field: string) => {
    await submitAnswer(field, "");
  };

  // Key the inner card on the suggestion id so React forcibly unmounts the
  // previous card when the broker swaps it. The useEffect above resets the
  // local stage when the id changes, but keying is defense in depth: it
  // guarantees no stale child state (focus, in-flight submit handler closure,
  // sub-component refs) leaks from one suggestion into the next. The
  // failure-mode this guards against is the scan-chip → blueprint-pick swap
  // after a scan failure (see useBrokerEvents.ts onboarding-state invalidate).
  return (
    <section
      className="ceo-card-section"
      aria-label="CEO question"
      data-testid="ceo-card-section"
      data-kind={pendingSuggestion.kind}
      data-suggestion-id={pendingSuggestion.id}
    >
      {renderCeoCard(
        pendingSuggestion,
        stage,
        committedValue,
        submitAnswer,
        handleSkip,
        setStage,
      )}
    </section>
  );
}

/**
 * Returns true if the field's answer should be persisted via /onboarding/answer.
 * `bridge_choice` is the terminal action (Start Issue vs. Look Around) and
 * advances the phase machine directly, so it is not stored as form state.
 */
function shouldPersistOnboardingAnswer(field: string) {
  return field !== "bridge_choice";
}

function isScratchBlueprintID(value: unknown) {
  if (typeof value !== "string") return true;
  return ["", "__blank_slate__", "from-scratch", "blank-slate"].includes(
    value.trim(),
  );
}

/**
 * Narrows an unknown CEO card value to the union the answer endpoint accepts.
 * Strings pass through; arrays are coerced to string[]; everything else
 * becomes undefined so the caller can elide the field from the request body.
 */
function committedOnboardingValue(
  value: unknown,
): string | string[] | undefined {
  if (typeof value === "string") return value;
  if (Array.isArray(value)) return value as string[];
  return undefined;
}

/** Returns true when answering this field terminates the onboarding loop. */
function completesOnboarding(field: string) {
  return field === "bridge_choice";
}

/**
 * Drives the deterministic phase machine after a CEO card answer commits.
 * Each `case` maps the just-answered field to the next phase transition the
 * client should request. The broker validates the transition before emitting
 * the next CEO card.
 */
async function advanceOnboardingAfterAnswer(
  field: string,
  value: unknown,
  phase: string | undefined,
) {
  switch (field) {
    case "company_name":
      await post("/onboarding/transition", { phase: "identity" });
      return;
    case "description":
      await post("/onboarding/transition", { phase: "website" });
      return;
    case "blueprint_id":
      // Only a real blueprint id routes to "team". Empty string is the
      // current scratch wire value; the named values are legacy/cached-client
      // sentinels that the backend also normalizes to scratch.
      if (isScratchBlueprintID(value)) {
        await post("/onboarding/transition", { phase: "seed" });
        await post("/onboarding/transition", { phase: "bridge" });
      } else {
        await post("/onboarding/transition", { phase: "team" });
      }
      return;
    case "picked_agents":
      await post("/onboarding/transition", { phase: "seed" });
      await post("/onboarding/transition", { phase: "bridge" });
      return;
    case "bridge_choice":
      await post("/onboarding/transition", { phase: "complete" });
      return;
    case "task_prompt":
      await post("/onboarding/transition", {
        phase: "approve",
      });
      return;
    case "website_url":
      // Same strict trimmed-string guard as blueprint_id — whitespace-only
      // values must NOT route to "scan" (the scanner would fetch nothing).
      await post("/onboarding/transition", {
        phase:
          typeof value === "string" && value.trim() !== ""
            ? "scan"
            : "blueprint",
      });
      return;
    default:
      if (phase === "scan" && field === "scan_complete") {
        await post("/onboarding/transition", { phase: "blueprint" });
      }
  }
}

function renderCeoCard(
  suggestion: CeoSuggestion,
  stage: CardStage,
  committedValue: string | string[] | undefined,
  onSubmit: (field: string, value: unknown) => Promise<void>,
  onSkip: (field: string) => Promise<void>,
  onStageChange?: (next: CardStage) => void,
): ReactNode {
  // Key on the suggestion id so React unmounts and remounts the card when the
  // broker swaps the pending suggestion (e.g. ceo_scan_chip → ceo_chip_row
  // after a scan failure). Without the key React would reconcile the same
  // StructuredMessageCard instance across kinds and any child-level state
  // (focus, controlled inputs, sub-component refs) could leak from one
  // suggestion into the next.
  return (
    <StructuredMessageCard
      key={suggestion.id}
      suggestion={suggestion}
      stage={stage}
      committedValue={committedValue}
      onSubmit={(field, value) => void onSubmit(field, value)}
      onSkip={(field) => void onSkip(field)}
      onStageChange={onStageChange}
    />
  );
}
