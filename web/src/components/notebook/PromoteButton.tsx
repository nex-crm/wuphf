import { formatAgentName } from "../../lib/agentName";

/**
 * Primary Promote-to-wiki button + Discard link. On click, transitions to a
 * disabled pending-pill state ("Pending review by CEO") per DESIGN-NOTEBOOK
 * Actions-footer section.
 */

interface PromoteButtonProps {
  reviewerSlug: string;
  pending: boolean;
  onPromote: () => void;
  onDiscard?: () => void;
  disabled?: boolean;
}

export default function PromoteButton({
  reviewerSlug,
  pending,
  onPromote,
  onDiscard,
  disabled,
}: PromoteButtonProps) {
  const reviewerLabel =
    reviewerSlug === "human-only"
      ? "a human reviewer"
      : formatAgentName(reviewerSlug);
  return (
    <div className="nb-actions">
      {pending ? (
        <button
          type="button"
          className="nb-promote-btn is-pending"
          disabled={true}
          aria-label={`Pending review by ${reviewerLabel}`}
        >
          Pending review by {reviewerLabel}
        </button>
      ) : (
        <button
          type="button"
          className="nb-promote-btn"
          onClick={onPromote}
          disabled={disabled}
          aria-label={`Submit this draft for review by ${reviewerLabel}`}
        >
          Promote to wiki →
        </button>
      )}
      {onDiscard && !pending && (
        <button type="button" className="nb-discard-link" onClick={onDiscard}>
          Discard entry
        </button>
      )}
    </div>
  );
}
