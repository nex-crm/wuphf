import { type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { NavArrowLeft, NavArrowRight, Xmark } from "iconoir-react";

import {
  type AgentRequest,
  answerRequest,
  cancelRequest,
  getSkillsList,
  type InterviewOption,
  patchSkill,
  post,
  postMessage,
  type Skill,
  type SkillSimilarRef,
} from "../../api/client";
import { useRequests } from "../../hooks/useRequests";
import { parseApprovalContext } from "../../lib/parseApprovalContext";
import {
  requestOptionNeedsText,
  requestOptionTextHint,
} from "../../lib/requestOptions";
import { directChannelSlug, useAppStore } from "../../stores/app";
import { SkillCompareView } from "../apps/SkillCompareView";
import { humanEchoForCeoAnswer } from "../onboarding/humanEcho";
import { useOnboardingDMContext } from "../onboarding/OnboardingDMRoute";
import type { CardStage, CeoSuggestion } from "../onboarding/types";
import { SidePanel } from "../ui/SidePanel";
import { showNotice } from "../ui/Toast";
import { ApprovalContextView } from "./ApprovalContextView";
import { StructuredMessageCard } from "./cards/StructuredMessageCard";

/**
 * Inline interview bar shown above the Composer. Mirrors the TUI behavior:
 * - Shows the current pending request (1/N counter for the queue)
 * - Allows cycling through queued requests with prev/next
 * - Renders option buttons; if the picked option requires custom text,
 *   switches to a text input mode using the option's hint as placeholder
 * - Skip / close cancels the unanswered interview
 *
 * PR 7 task #14 extends this with the enhance-existing flow:
 * - kind="enhance_skill_proposal" replaces the standard option row with a
 *   side-by-side preview + three buttons (Enhance / Approve anyway / Reject).
 * - kind="skill_proposal" with metadata.similar_to_existing renders a
 *   warning banner with a [Compare] action above the standard options.
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
// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Existing cognitive complexity is baselined for a focused follow-up refactor.
// biome-ignore lint/complexity/noExcessiveLinesPerFunction: Existing function length is baselined for a focused follow-up refactor.
export function InterviewBar() {
  const { pending } = useRequests();
  const queryClient = useQueryClient();

  const queue = useMemo(() => {
    const sorted = [...pending].sort((a, b) => {
      const ta = a.created_at ?? "";
      const tb = b.created_at ?? "";
      return ta.localeCompare(tb);
    });
    return sorted;
  }, [pending]);

  const [cursor, setCursor] = useState(0);
  const [textMode, setTextMode] = useState<{ option: InterviewOption } | null>(
    null,
  );
  const [customText, setCustomText] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [dismissedIds, setDismissedIds] = useState<Set<string>>(new Set());
  const [compareOpen, setCompareOpen] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const visible = queue.filter((r) => !dismissedIds.has(r.id));
  const safeCursor = Math.min(cursor, Math.max(visible.length - 1, 0));
  const current = visible[safeCursor] ?? null;

  // The Skills catalog feeds the compare view's "existing" pane and
  // supplies the existing-skill body for the patchSkill call. Cached at
  // ["skills", "all"] so SkillsApp shares the same query.
  const enhanceContext = readEnhanceContext(current);
  const needsCatalog =
    enhanceContext.kind === "enhance_skill_proposal" ||
    enhanceContext.kind === "skill_proposal_ambiguous";
  const { data: catalog } = useQuery({
    queryKey: ["skills", "all"],
    queryFn: () => getSkillsList("all"),
    staleTime: 30_000,
    enabled: needsCatalog,
  });
  const existingSkill = useMemo(() => {
    if (!enhanceContext.existingSlug) return undefined;
    return catalog?.skills.find((s) =>
      skillSlugMatches(s.name, enhanceContext.existingSlug),
    );
  }, [catalog, enhanceContext.existingSlug]);

  // Reset transient UI state when the active request changes. Keyed on
  // current?.id so cycling between queued requests (or replacing the
  // active one with a fresh broker push) clears textMode / customText /
  // compareOpen before state from one card leaks into the next.
  const currentId = current?.id ?? null;
  useEffect(() => {
    setTextMode(null);
    setCustomText("");
    setCompareOpen(false);
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
      // Enhance flow: when the user accepts the gate's recommendation we
      // must apply the candidate body to the existing skill BEFORE we
      // close the interview. Architect's task #15 deliberately leaves the
      // patch to the UI so the human can preview the diff before commit.
      if (
        option.id === "enhance" &&
        current.kind === "enhance_skill_proposal"
      ) {
        const slug = enhanceContext.existingSlug;
        if (!slug) {
          throw new Error("Missing existing slug on enhance interview");
        }
        if (!existingSkill?.content) {
          throw new Error(
            "Existing skill body not loaded yet — try again in a moment",
          );
        }
        const candidate =
          (current.enhance_candidate as Skill | undefined) ??
          enhanceContext.candidate;
        const newBody = candidate?.content ?? "";
        if (!newBody) {
          throw new Error("Candidate body is empty");
        }
        await patchSkill(slug, {
          old_string: existingSkill.content,
          new_string: newBody,
          replace_all: false,
        });
      }

      await answerRequest(current.id, option.id, text);
      await queryClient.invalidateQueries({ queryKey: ["requests"] });
      await queryClient.invalidateQueries({ queryKey: ["skills"] });
      setTextMode(null);
      setCustomText("");

      // Surface a tailored toast for enhance-flow decisions; legacy
      // skill_proposal answers stay quiet (the queue empty-state is the
      // signal).
      if (current.kind === "enhance_skill_proposal") {
        if (option.id === "enhance" && enhanceContext.existingSlug) {
          showNotice(
            `Updated ${enhanceContext.existingSlug}. Source article still tracked.`,
            "success",
          );
        } else if (option.id === "approve_anyway") {
          showNotice(
            "Created as a new skill despite the similarity warning.",
            "success",
          );
        } else if (option.id === "reject") {
          const target = enhanceContext.existingSlug
            ? ` (duplicate of ${enhanceContext.existingSlug})`
            : "";
          showNotice(`Dropped the candidate${target}.`, "info");
        }
      }
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

  const isEnhance = enhanceContext.kind === "enhance_skill_proposal";
  const ambiguousRef =
    enhanceContext.kind === "skill_proposal_ambiguous"
      ? enhanceContext.similarRef
      : undefined;
  const candidateForCompare =
    (current.enhance_candidate as Skill | undefined) ??
    enhanceContext.candidate ??
    fallbackCandidateFromRequest(current);

  return (
    <>
      <CeoCardSection />
      <section className="interview-bar" aria-label="Pending agent request">
        <div className="interview-bar-head">
          <span className="badge badge-yellow">
            {current.blocking ? "BLOCKING" : "INTERVIEW"}
          </span>
          {current.kind === "approval" ? (
            <span className="badge badge-orange">EXTERNAL ACTION</span>
          ) : null}
          <span className="interview-bar-from">
            @{current.from || "agent"} asks
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

          {ambiguousRef ? (
            <SimilarBanner
              slug={ambiguousRef.slug}
              score={ambiguousRef.score}
              onCompare={() => setCompareOpen(true)}
            />
          ) : null}

          {isEnhance ? (
            <EnhancePreview
              existing={existingSkill}
              candidate={candidateForCompare}
              score={enhanceContext.similarRef?.score}
              method={enhanceContext.similarRef?.method}
              onOpenFull={() => setCompareOpen(true)}
            />
          ) : null}
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
                {submitting ? "Sending..." : `Send as ${textMode.option.label}`}
              </button>
            </div>
          </div>
        ) : isEnhance ? (
          <EnhanceActions
            options={options}
            submitting={submitting}
            onPick={handleOption}
          />
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

        <SidePanel
          open={compareOpen}
          onClose={() => setCompareOpen(false)}
          title="Compare skills"
          subtitle={
            enhanceContext.existingSlug
              ? `existing: @${enhanceContext.existingSlug}`
              : undefined
          }
        >
          {candidateForCompare ? (
            <SkillCompareView
              existing={existingSkill}
              candidate={candidateForCompare}
              score={enhanceContext.similarRef?.score}
              method={enhanceContext.similarRef?.method}
            />
          ) : (
            <p style={{ color: "var(--text-tertiary)", fontSize: 13 }}>
              Couldn't load candidate data.
            </p>
          )}
        </SidePanel>
      </section>
    </>
  );
}

interface EnhanceContext {
  kind: "enhance_skill_proposal" | "skill_proposal_ambiguous" | "other";
  existingSlug?: string;
  similarRef?: SkillSimilarRef;
  candidate?: Skill;
}

/**
 * Read the structured metadata the broker stamped on the interview
 * (PR 7 task #15) and classify the interview into one of three buckets:
 *
 * - "enhance_skill_proposal" — full enhance UX (3 buttons + side-by-side).
 * - "skill_proposal_ambiguous" — standard 2-button row, plus banner.
 * - "other" — rendered as a normal interview.
 *
 * Returns flat scalars so the calling component doesn't have to walk
 * `unknown` shapes inline.
 */
function readEnhanceContext(req: AgentRequest | null): EnhanceContext {
  if (!req) return { kind: "other" };
  const meta = req.metadata;
  if (req.kind === "enhance_skill_proposal") {
    const slug =
      typeof meta?.enhances_slug === "string" ? meta.enhances_slug : undefined;
    return {
      kind: "enhance_skill_proposal",
      existingSlug: slug,
      similarRef: readSimilarRef(meta?.similar_to_existing),
      candidate: req.enhance_candidate as Skill | undefined,
    };
  }
  if (req.kind === "skill_proposal") {
    const ref = readSimilarRef(meta?.similar_to_existing);
    if (ref) {
      return {
        kind: "skill_proposal_ambiguous",
        existingSlug: ref.slug,
        similarRef: ref,
      };
    }
  }
  return { kind: "other" };
}

function readSimilarRef(value: unknown): SkillSimilarRef | undefined {
  if (!value || typeof value !== "object") return undefined;
  const v = value as Record<string, unknown>;
  if (typeof v.slug !== "string" || typeof v.score !== "number") {
    return undefined;
  }
  return {
    slug: v.slug,
    score: v.score,
    method: typeof v.method === "string" ? v.method : undefined,
  };
}

function skillSlugMatches(name: string | undefined, slug: string | undefined) {
  if (!(name && slug)) return false;
  const normalize = (s: string) =>
    s.trim().toLowerCase().replace(/\s+/g, "-").replace(/_/g, "-");
  return normalize(name) === normalize(slug);
}

/**
 * Synthesize a Skill-shaped object from the request's reply_to + question
 * when the broker doesn't ship `enhance_candidate` (fallback path; the
 * task #15 contract supplies it directly, but defensive shapes guard
 * against transitional broker builds in dev environments).
 */
function fallbackCandidateFromRequest(
  req: AgentRequest | null,
): Skill | undefined {
  if (!req) return undefined;
  const name = req.reply_to;
  if (!name) return undefined;
  return {
    name,
    description: req.question?.split("\n\n")[1] || "",
    content: req.context || "",
  };
}

interface SimilarBannerProps {
  slug: string;
  score: number;
  onCompare: () => void;
}

function SimilarBanner({ slug, score, onCompare }: SimilarBannerProps) {
  return (
    <div
      role="note"
      style={{
        display: "flex",
        alignItems: "center",
        gap: 8,
        flexWrap: "wrap",
        marginTop: 8,
        padding: "8px 12px",
        background: "var(--yellow-bg, #fff7d6)",
        border: "1px solid var(--yellow, #d6a700)",
        borderRadius: 6,
        fontSize: 12,
        color: "var(--text)",
      }}
    >
      <span>
        Similar to <strong>{slug}</strong> (score: {score.toFixed(2)}). Worth
        merging?
      </span>
      <button
        type="button"
        className="btn-text"
        onClick={onCompare}
        style={{ padding: "2px 6px" }}
      >
        Compare
      </button>
    </div>
  );
}

interface EnhancePreviewProps {
  existing: Skill | undefined;
  candidate: Skill | undefined;
  score?: number;
  method?: string;
  onOpenFull: () => void;
}

function EnhancePreview({
  existing,
  candidate,
  score,
  method,
  onOpenFull,
}: EnhancePreviewProps) {
  if (!candidate) return null;
  return (
    <div style={{ marginTop: 10 }}>
      <SkillCompareView
        existing={existing}
        candidate={candidate}
        score={score}
        method={method}
      />
      <div style={{ marginTop: 6, textAlign: "right" }}>
        <button
          type="button"
          className="btn-text"
          onClick={onOpenFull}
          style={{ padding: "2px 6px", fontSize: 12 }}
        >
          Open full comparison →
        </button>
      </div>
    </div>
  );
}

interface EnhanceActionsProps {
  options: InterviewOption[];
  submitting: boolean;
  onPick: (option: InterviewOption) => void;
}

/**
 * Three-button action row for enhance_skill_proposal interviews. Maps the
 * server-registered option IDs (enhance / approve_anyway / reject) to the
 * user-facing layout: primary | secondary | text-destructive.
 */
function EnhanceActions({ options, submitting, onPick }: EnhanceActionsProps) {
  const enhance = options.find((o) => o.id === "enhance");
  const approve = options.find((o) => o.id === "approve_anyway");
  const reject = options.find((o) => o.id === "reject");

  return (
    <div
      className="interview-bar-actions"
      style={{
        display: "flex",
        alignItems: "center",
        gap: 8,
        flexWrap: "wrap",
      }}
    >
      {enhance ? (
        <button
          type="button"
          className="btn btn-primary btn-sm"
          onClick={() => onPick(enhance)}
          disabled={submitting}
          title={enhance.description}
          aria-label={enhance.label}
        >
          {submitting ? "Working..." : enhance.label}
        </button>
      ) : null}
      {approve ? (
        <button
          type="button"
          className="btn btn-ghost btn-sm"
          onClick={() => onPick(approve)}
          disabled={submitting}
          title={approve.description}
          aria-label={approve.label}
        >
          {approve.label}
        </button>
      ) : null}
      {reject ? (
        <button
          type="button"
          className="btn-text btn-text--danger"
          onClick={() => onPick(reject)}
          disabled={submitting}
          title={reject.description}
          aria-label={reject.label}
        >
          {reject.label}
        </button>
      ) : null}
    </div>
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
