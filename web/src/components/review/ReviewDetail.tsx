import { useEffect } from "react";

import type { ReviewItem } from "../../api/notebook";
import InlineReviewThread from "../notebook/InlineReviewThread";

/**
 * Right-side drawer that opens when a review card is clicked. No modal
 * backdrop by default (spec: "no modals"), but we render a low-opacity
 * backdrop to handle click-away dismissal; Esc also closes.
 */

interface ReviewDetailProps {
  review: ReviewItem;
  onClose: () => void;
  onApprove?: (id: string) => void;
  /** Request changes requires a rationale — the broker 400s without one. */
  onRequestChanges?: (id: string, rationale: string) => void;
  /** Last failed action's error, rendered inside the drawer so a broken
   *  approve/request-changes is never silent (ICP-eval v3 [18:14:39]). */
  actionError?: string | null;
}

export default function ReviewDetail({
  review,
  onClose,
  onApprove,
  onRequestChanges,
  actionError,
}: ReviewDetailProps) {
  useEffect(() => {
    function handleKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    window.addEventListener("keydown", handleKey);
    return () => window.removeEventListener("keydown", handleKey);
  }, [onClose]);

  return (
    <>
      <div
        className="nb-review-drawer-backdrop"
        onClick={onClose}
        aria-hidden="true"
      />
      <aside
        className="nb-review-drawer"
        role="dialog"
        aria-label={`Review: ${review.entry_title}`}
        data-testid="nb-review-drawer"
      >
        <button
          type="button"
          className="nb-review-drawer-close"
          onClick={onClose}
          aria-label="Close review detail"
        >
          ×
        </button>
        <h2>{review.entry_title}</h2>
        <div className="nb-review-drawer-path">
          Proposed path: {review.proposed_wiki_path}
        </div>

        {actionError ? (
          <p
            className="nb-error"
            role="alert"
            data-testid="nb-review-action-error"
          >
            Could not update this review: {actionError}
          </p>
        ) : null}

        <InlineReviewThread
          reviewerSlug={review.reviewer_slug}
          state={review.state}
          comments={review.comments}
          onApprove={onApprove ? () => onApprove(review.id) : undefined}
          onRequestChanges={
            onRequestChanges
              ? (rationale) => onRequestChanges(review.id, rationale)
              : undefined
          }
        />
      </aside>
    </>
  );
}
