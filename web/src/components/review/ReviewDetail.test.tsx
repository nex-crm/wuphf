import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import ReviewDetail from './ReviewDetail'
import type { ReviewItem } from '../../api/notebook'

const REVIEW: ReviewItem = {
  id: 'r1',
  agent_slug: 'pm',
  entry_slug: 'e',
  entry_title: 'Sample review',
  proposed_wiki_path: 'customers/acme',
  excerpt: 'excerpt',
  reviewer_slug: 'ceo',
  state: 'in-review',
  submitted_ts: new Date().toISOString(),
  updated_ts: new Date().toISOString(),
  comments: [
    { id: 'c1', author_slug: 'pm', body_md: 'Sub.', ts: new Date().toISOString() },
  ],
}

describe('<ReviewDetail>', () => {
  it('renders title, path, and comment thread', () => {
    render(<ReviewDetail review={REVIEW} onClose={() => {}} />)
    expect(screen.getByRole('heading', { name: 'Sample review' })).toBeInTheDocument()
    expect(screen.getByText(/Proposed path: customers\/acme/)).toBeInTheDocument()
    expect(screen.getByText('Sub.')).toBeInTheDocument()
  })

  it('calls onClose when the close button is clicked', async () => {
    const onClose = vi.fn()
    render(<ReviewDetail review={REVIEW} onClose={onClose} />)
    await userEvent.setup().click(screen.getByLabelText('Close review detail'))
    expect(onClose).toHaveBeenCalled()
  })

  it('calls onClose on Escape key', () => {
    const onClose = vi.fn()
    render(<ReviewDetail review={REVIEW} onClose={onClose} />)
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(onClose).toHaveBeenCalled()
  })

  it('wires approve and request-changes handlers when supplied', async () => {
    const onApprove = vi.fn()
    const onRequestChanges = vi.fn()
    render(
      <ReviewDetail
        review={REVIEW}
        onClose={() => {}}
        onApprove={onApprove}
        onRequestChanges={onRequestChanges}
      />,
    )
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Approve' }))
    expect(onApprove).toHaveBeenCalledWith('r1')
    await user.click(screen.getByRole('button', { name: 'Request changes' }))
    expect(onRequestChanges).toHaveBeenCalledWith('r1')
  })
})
