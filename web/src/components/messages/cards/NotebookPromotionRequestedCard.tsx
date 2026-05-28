/**
 * NotebookPromotionRequestedCard — chat card emitted by the broker in
 * #general when an agent submits a notebook→wiki promotion request.
 *
 * Click → navigates to /reviews (the Reviews app). The promotion id is
 * surfaced as a data attribute so future iterations can deep-link to the
 * specific review without changing the wire shape.
 */

import { router } from "../../../lib/router";

export interface NotebookPromotionRequestedPayload {
  promotion_id?: string;
  source_path?: string;
  target_path?: string;
  submitter?: string;
}

function isStringField(value: unknown): value is string {
  return typeof value === "string" && value.length > 0;
}

export function parseNotebookPromotionRequestedPayload(
  raw: unknown,
): NotebookPromotionRequestedPayload {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
    return {};
  }
  const r = raw as Record<string, unknown>;
  const out: NotebookPromotionRequestedPayload = {};
  if (isStringField(r.promotion_id)) out.promotion_id = r.promotion_id;
  if (isStringField(r.source_path)) out.source_path = r.source_path;
  if (isStringField(r.target_path)) out.target_path = r.target_path;
  if (isStringField(r.submitter)) out.submitter = r.submitter;
  return out;
}

export interface NotebookPromotionRequestedCardProps {
  payload: NotebookPromotionRequestedPayload;
}

export function NotebookPromotionRequestedCard({
  payload,
}: NotebookPromotionRequestedCardProps) {
  const promotionId = payload.promotion_id ?? "";
  const sourcePath = payload.source_path ?? "";
  const targetPath = payload.target_path ?? "";
  const submitter = payload.submitter;
  const title = targetPath || "Promotion requested";

  function openReviews() {
    void router.navigate({ to: "/reviews" });
  }

  return (
    <button
      type="button"
      className="issue-lifecycle-card issue-lifecycle-card--review"
      onClick={openReviews}
      data-testid="notebook-promotion-requested-card"
      data-promotion-id={promotionId}
      aria-label={`Open reviews for promotion ${promotionId}`}
    >
      <span className="issue-lifecycle-card-icon" aria-hidden="true">
        📥
      </span>
      <span className="issue-lifecycle-card-body">
        <span className="issue-lifecycle-card-eyebrow">
          Promotion requested
          {submitter ? (
            <span className="issue-lifecycle-card-id"> · by @{submitter}</span>
          ) : null}
        </span>
        <span className="issue-lifecycle-card-title">{title}</span>
        {sourcePath && targetPath ? (
          <span className="issue-lifecycle-card-meta">
            {sourcePath} → {targetPath}
          </span>
        ) : null}
      </span>
      <span className="issue-lifecycle-card-cta" aria-hidden="true">
        Review →
      </span>
    </button>
  );
}
