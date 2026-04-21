import ReviewCard from './ReviewCard'
import type { ReviewItem } from '../../api/notebook'

/** A Kanban column with a Caveat header and list of cards. */

interface ReviewColumnProps {
  title: string
  items: ReviewItem[]
  activeId?: string | null
  onOpenCard: (id: string) => void
}

export default function ReviewColumn({ title, items, activeId, onOpenCard }: ReviewColumnProps) {
  return (
    <section
      className="nb-review-col"
      aria-label={`${title} reviews`}
      data-testid={`nb-review-col-${title.toLowerCase().replace(/\s+/g, '-')}`}
    >
      <div className="nb-review-col-head">
        <h3>{title}</h3>
        <span className="nb-review-col-count">{items.length}</span>
      </div>
      {items.length === 0 ? (
        <p className="nb-review-col-empty">Empty</p>
      ) : (
        items.map((r) => (
          <ReviewCard key={r.id} review={r} active={activeId === r.id} onOpen={onOpenCard} />
        ))
      )}
    </section>
  )
}
