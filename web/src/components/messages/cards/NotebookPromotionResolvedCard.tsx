/**
 * NotebookPromotionResolvedCard — chat card emitted by the broker in
 * #general when a reviewer resolves a promotion (approve OR request
 * changes). One card kind, payload.decision disambiguates so the FE
 * picks the right eyebrow, icon, and accent at render time.
 *
 * Click → navigates to /reviews. Same destination for both decisions
 * because the Reviews app is the surface where follow-up happens.
 */

import { router } from "../../../lib/router";

export type NotebookPromotionDecision = "approved" | "changes_requested";

export interface NotebookPromotionResolvedPayload {
  promotion_id?: string;
  source_path?: string;
  target_path?: string;
  reviewer?: string;
  decision?: NotebookPromotionDecision;
  rationale?: string;
  submitter?: string;
}

function isStringField(value: unknown): value is string {
  return typeof value === "string" && value.length > 0;
}

function parseDecision(
  raw: unknown,
): NotebookPromotionDecision | undefined {
  if (raw === "approved" || raw === "changes_requested") {
    return raw;
  }
  return undefined;
}

export function parseNotebookPromotionResolvedPayload(
  raw: unknown,
): NotebookPromotionResolvedPayload {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
    return {};
  }
  const r = raw as Record<string, unknown>;
  const out: NotebookPromotionResolvedPayload = {};
  if (isStringField(r.promotion_id)) out.promotion_id = r.promotion_id;
  if (isStringField(r.source_path)) out.source_path = r.source_path;
  if (isStringField(r.target_path)) out.target_path = r.target_path;
  if (isStringField(r.reviewer)) out.reviewer = r.reviewer;
  const decision = parseDecision(r.decision);
  if (decision) out.decision = decision;
  if (isStringField(r.rationale)) out.rationale = r.rationale;
  if (isStringField(r.submitter)) out.submitter = r.submitter;
  return out;
}

interface DecisionPresentation {
  eyebrow: string;
  icon: string;
  accent: "done" | "warn";
  cta: string;
}

function presentationFor(
  decision: NotebookPromotionDecision | undefined,
  reviewer: string | undefined,
): DecisionPresentation {
  const reviewerTag = reviewer ? `@${reviewer}` : "Reviewer";
  if (decision === "changes_requested") {
    return {
      eyebrow: `Changes requested by ${reviewerTag}`,
      icon: "✏️",
      accent: "warn",
      cta: "Open review →",
    };
  }
  if (decision === "approved") {
    return {
      eyebrow: `Approved by ${reviewerTag}`,
      icon: "✅",
      accent: "done",
      cta: "Open review →",
    };
  }
  // Unknown / missing decision: never claim "approved" since that would
  // misrepresent the review outcome. Fall back to a neutral state.
  return {
    eyebrow: `Promotion resolved by ${reviewerTag}`,
    icon: "ℹ️",
    accent: "warn",
    cta: "Open review →",
  };
}

export interface NotebookPromotionResolvedCardProps {
  payload: NotebookPromotionResolvedPayload;
}

export function NotebookPromotionResolvedCard({
  payload,
}: NotebookPromotionResolvedCardProps) {
  const promotionId = payload.promotion_id ?? "";
  const sourcePath = payload.source_path ?? "";
  const targetPath = payload.target_path ?? "";
  const decision = payload.decision;
  const presentation = presentationFor(decision, payload.reviewer);
  const title = targetPath || "Promotion resolved";

  function openReviews() {
    void router.navigate({ to: "/reviews" });
  }

  return (
    <button
      type="button"
      className={`issue-lifecycle-card issue-lifecycle-card--${presentation.accent}`}
      onClick={openReviews}
      data-testid="notebook-promotion-resolved-card"
      data-promotion-id={promotionId}
      data-decision={decision ?? ""}
      aria-label={`Open reviews for promotion ${promotionId}`}
    >
      <span className="issue-lifecycle-card-icon" aria-hidden="true">
        {presentation.icon}
      </span>
      <span className="issue-lifecycle-card-body">
        <span className="issue-lifecycle-card-eyebrow">
          {presentation.eyebrow}
        </span>
        <span className="issue-lifecycle-card-title">{title}</span>
        {sourcePath && targetPath ? (
          <span className="issue-lifecycle-card-meta">
            {sourcePath} → {targetPath}
          </span>
        ) : null}
      </span>
      <span className="issue-lifecycle-card-cta" aria-hidden="true">
        {presentation.cta}
      </span>
    </button>
  );
}
