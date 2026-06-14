import { useState } from "react";

import type { ReviewComment, ReviewState } from "../../api/notebook";
import { formatAgentName } from "../../lib/agentName";
import { formatRelativeTime } from "../../lib/format";
import { PixelAvatar } from "../ui/PixelAvatar";

/**
 * Review thread surface beneath the entry body. The broker owns the write
 * path; this component renders current comments and optional review actions.
 *
 * Request changes is a two-step gesture: the broker REQUIRES a rationale
 * (POST /review/{id}/request-changes returns 400 "rationale is required"
 * without one), so the button reveals a textarea + confirm instead of firing
 * an empty payload. The live v3 ICP eval clicked the old one-shot button
 * three times and got three silent 400s ([17:43–17:45], shot 12).
 */

interface InlineReviewThreadProps {
  reviewerSlug: string;
  state: ReviewState | null;
  comments: ReviewComment[];
  onApprove?: () => void;
  /** Called with the reviewer's non-empty rationale. */
  onRequestChanges?: (rationale: string) => void;
}

export default function InlineReviewThread({
  reviewerSlug,
  state,
  comments,
  onApprove,
  onRequestChanges,
}: InlineReviewThreadProps) {
  const [requesting, setRequesting] = useState(false);
  const [rationale, setRationale] = useState("");
  const [rationaleError, setRationaleError] = useState<string | null>(null);

  if (
    !state ||
    state === "archived" ||
    state === "rejected" ||
    state === "expired"
  )
    return null;

  const reviewerLabel =
    reviewerSlug === "human-only"
      ? "Human reviewer"
      : formatAgentName(reviewerSlug);
  const hasActions = Boolean(onApprove || onRequestChanges);

  function submitRequestChanges() {
    const trimmed = rationale.trim();
    if (!trimmed) {
      setRationaleError("Say what needs to change — the author needs it.");
      return;
    }
    setRationaleError(null);
    setRequesting(false);
    setRationale("");
    onRequestChanges?.(trimmed);
  }

  return (
    <section
      className="nb-review"
      aria-label="Reviewer comments"
      data-testid="nb-inline-review"
    >
      <h3>Review — {reviewerLabel}</h3>
      {comments.length === 0 ? (
        <p className="nb-review-empty">
          No comments yet. {reviewerLabel} has been notified.
        </p>
      ) : (
        comments.map((c) => (
          <div key={c.id} className="nb-review-comment">
            <PixelAvatar slug={c.author_slug} size={22} />
            <div>
              <div>
                <span className="nb-comment-author">
                  {formatAgentName(c.author_slug)}
                </span>
                <span className="nb-comment-ts">
                  {formatRelativeTime(c.ts)}
                </span>
              </div>
              <p className="nb-comment-body">{c.body_md}</p>
            </div>
          </div>
        ))
      )}
      {hasActions && !requesting ? (
        <div className="nb-review-drawer-actions">
          {onApprove ? (
            <button
              type="button"
              className="nb-review-drawer-approve"
              onClick={onApprove}
            >
              Approve
            </button>
          ) : null}
          {onRequestChanges ? (
            <button
              type="button"
              className="nb-review-drawer-reject"
              onClick={() => setRequesting(true)}
            >
              Request changes
            </button>
          ) : null}
        </div>
      ) : null}
      {requesting ? (
        <div
          className="nb-review-request-changes"
          data-testid="nb-review-request-changes"
        >
          <label
            className="nb-review-request-changes-label"
            htmlFor="nb-review-rationale"
          >
            What needs to change? (required)
          </label>
          <textarea
            id="nb-review-rationale"
            className="nb-review-request-changes-input"
            value={rationale}
            rows={3}
            placeholder="e.g. Merge with the existing Corti brief — don't create a duplicate."
            onChange={(e) => {
              setRationale(e.target.value);
              if (rationaleError) setRationaleError(null);
            }}
            data-testid="nb-review-rationale-input"
          />
          {rationaleError ? (
            <p className="nb-error" role="alert">
              {rationaleError}
            </p>
          ) : null}
          <div className="nb-review-drawer-actions">
            <button
              type="button"
              className="nb-review-drawer-reject"
              onClick={submitRequestChanges}
              data-testid="nb-review-rationale-submit"
            >
              Request changes
            </button>
            <button
              type="button"
              className="nb-review-drawer-cancel"
              onClick={() => {
                setRequesting(false);
                setRationale("");
                setRationaleError(null);
              }}
            >
              Cancel
            </button>
          </div>
        </div>
      ) : null}
    </section>
  );
}
