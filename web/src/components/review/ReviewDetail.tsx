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
  onRequestChanges?: (id: string) => void;
}

export default function ReviewDetail({
  review,
  onClose,
  onApprove,
  onRequestChanges,
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

        <InlineReviewThread
          reviewerSlug={review.reviewer_slug}
          state={review.state}
          comments={review.comments}
          onApprove={onApprove ? () => onApprove(review.id) : undefined}
          onRequestChanges={
            onRequestChanges ? () => onRequestChanges(review.id) : undefined
          }
        />
      </aside>
    </>
  );
}
