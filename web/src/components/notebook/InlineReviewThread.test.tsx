import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import InlineReviewThread from './InlineReviewThread'
import type { ReviewComment } from '../../api/notebook'

const COMMENTS: ReviewComment[] = [
  { id: 'c1', author_slug: 'pm', body_md: 'Submitting for review.', ts: new Date().toISOString() },
  { id: 'c2', author_slug: 'ceo', body_md: 'One nit about scope.', ts: new Date().toISOString() },
]

describe('<InlineReviewThread>', () => {
  it('returns nothing when there is no active review', () => {
    const { container } = render(
      <InlineReviewThread reviewerSlug="ceo" state={null} comments={[]} />,
    )
    expect(container.firstChild).toBeNull()
  })

  it('renders comments with author names and bodies', () => {
    render(
      <InlineReviewThread reviewerSlug="ceo" state="in-review" comments={COMMENTS} />,
    )
    expect(screen.getByRole('heading', { name: 'Review — CEO' })).toBeInTheDocument()
    expect(screen.getByText('PM')).toBeInTheDocument()
    expect(screen.getByText('CEO')).toBeInTheDocument()
    expect(screen.getByText('Submitting for review.')).toBeInTheDocument()
    expect(screen.getByText('One nit about scope.')).toBeInTheDocument()
  })

  it('announces "Human reviewer" when reviewer is human-only', () => {
    render(<InlineReviewThread reviewerSlug="human-only" state="pending" comments={[]} />)
    expect(screen.getByRole('heading', { name: 'Review — Human reviewer' })).toBeInTheDocument()
  })

  it('shows an empty-state line when there are no comments', () => {
    render(<InlineReviewThread reviewerSlug="ceo" state="pending" comments={[]} />)
    expect(screen.getByText(/No comments yet/)).toBeInTheDocument()
  })

  it('renders approve + request-changes buttons when handlers supplied, and wires them', async () => {
    const onApprove = vi.fn()
    const onRequestChanges = vi.fn()
    render(
      <InlineReviewThread
        reviewerSlug="ceo"
        state="in-review"
        comments={COMMENTS}
        onApprove={onApprove}
        onRequestChanges={onRequestChanges}
      />,
    )
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Approve' }))
    expect(onApprove).toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Request changes' }))
    expect(onRequestChanges).toHaveBeenCalled()
  })
})
