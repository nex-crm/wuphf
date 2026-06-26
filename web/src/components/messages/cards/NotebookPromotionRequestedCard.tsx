/**
 * NotebookPromotionRequestedCard — chat card emitted by the broker in
 * #general when an agent submits a notebook→wiki promotion request.
 *
 * Click → opens the notebook item itself (the entry view carries the
 * in-place Approve / Request-changes bar), derived from `source_path`
 * ("agents/<slug>/notebook/<entry>.md"). Falls back to /reviews when the
 * path is missing or unparseable. The promotion id is surfaced as a data
 * attribute so future iterations can deep-link the specific review without
 * changing the wire shape.
 */

import { router } from "../../../lib/router";

/** Extracts {agentSlug, entrySlug} from "agents/<slug>/notebook/<entry>.md". */
export function notebookEntryFromSourcePath(
  sourcePath: string,
): { agentSlug: string; entrySlug: string } | null {
  const match = /^agents\/([^/]+)\/notebook\/([^/]+)\.md$/.exec(
    sourcePath.trim(),
  );
  if (!match) return null;
  const [, agentSlug, entrySlug] = match;
  if (!(agentSlug && entrySlug)) return null;
  return { agentSlug, entrySlug };
}

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
    const entry = notebookEntryFromSourcePath(sourcePath);
    if (entry) {
      void router.navigate({
        to: "/notebooks/$agentSlug/$entrySlug",
        params: { agentSlug: entry.agentSlug, entrySlug: entry.entrySlug },
      });
      return;
    }
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
