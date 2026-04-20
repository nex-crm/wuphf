import { PixelAvatar } from '../ui/PixelAvatar'
import { formatAgentName } from '../../lib/agentName'
import { formatRelativeTime } from '../../lib/format'
import type { ReviewItem } from '../../api/notebook'

/** One promotion card in the `/reviews` Kanban. */

interface ReviewCardProps {
  review: ReviewItem
  active?: boolean
  onOpen: (id: string) => void
}

export default function ReviewCard({ review, active, onOpen }: ReviewCardProps) {
  return (
    <button
      type="button"
      className={`nb-review-card${active ? ' is-active' : ''}`}
      onClick={() => onOpen(review.id)}
      aria-label={`Open review for ${review.entry_title}`}
      data-testid="nb-review-card"
    >
      <div className="nb-review-card-title">{review.entry_title}</div>
      <div className="nb-review-card-excerpt">{review.excerpt}</div>
      <div className="nb-review-card-meta">
        <span className="nb-review-card-avatars" aria-hidden="true">
          <PixelAvatar slug={review.agent_slug} size={14} />
          <span>→</span>
          <PixelAvatar slug={review.reviewer_slug} size={14} />
        </span>
        <span className="nb-review-card-path">{review.proposed_wiki_path}</span>
        <span style={{ marginLeft: 'auto' }}>
          {formatAgentName(review.agent_slug)} · {formatRelativeTime(review.updated_ts)}
        </span>
      </div>
    </button>
  )
}
