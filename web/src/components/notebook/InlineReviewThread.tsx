import { PixelAvatar } from '../ui/PixelAvatar'
import { formatAgentName } from '../../lib/agentName'
import { formatRelativeTime } from '../../lib/format'
import type { ReviewComment, ReviewState } from '../../api/notebook'

/**
 * Review thread surface beneath the entry body. Lane C owns the write
 * path; this component is read-only in v1.1 but keeps space for Lane C
 * to wire approve/request-changes/reply controls.
 */

interface InlineReviewThreadProps {
  reviewerSlug: string
  state: ReviewState | null
  comments: ReviewComment[]
  onApprove?: () => void
  onRequestChanges?: () => void
}

export default function InlineReviewThread({
  reviewerSlug,
  state,
  comments,
  onApprove,
  onRequestChanges,
}: InlineReviewThreadProps) {
  if (!state || state === 'archived') return null

  const reviewerLabel = reviewerSlug === 'human-only'
    ? 'Human reviewer'
    : formatAgentName(reviewerSlug)

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
                <span className="nb-comment-author">{formatAgentName(c.author_slug)}</span>
                <span className="nb-comment-ts">{formatRelativeTime(c.ts)}</span>
              </div>
              <p className="nb-comment-body">{c.body_md}</p>
            </div>
          </div>
        ))
      )}
      {(onApprove || onRequestChanges) && (
        <div className="nb-review-drawer-actions">
          {onApprove && (
            <button
              type="button"
              className="nb-review-drawer-approve"
              onClick={onApprove}
            >
              Approve
            </button>
          )}
          {onRequestChanges && (
            <button
              type="button"
              className="nb-review-drawer-reject"
              onClick={onRequestChanges}
            >
              Request changes
            </button>
          )}
        </div>
      )}
    </section>
  )
}
