import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import ReviewColumn from './ReviewColumn'
import type { ReviewItem } from '../../api/notebook'

function mkReview(id: string, title: string): ReviewItem {
  return {
    id,
    agent_slug: 'pm',
    entry_slug: 'e',
    entry_title: title,
    proposed_wiki_path: 'x/y',
    excerpt: 'excerpt',
    reviewer_slug: 'ceo',
    state: 'pending',
    submitted_ts: new Date().toISOString(),
    updated_ts: new Date().toISOString(),
    comments: [],
  }
}

describe('<ReviewColumn>', () => {
  it('renders title with count badge', () => {
    render(
      <ReviewColumn
        title="Pending"
        items={[mkReview('r1', 'A'), mkReview('r2', 'B')]}
        onOpenCard={() => {}}
      />,
    )
    expect(screen.getByRole('heading', { name: 'Pending' })).toBeInTheDocument()
    expect(screen.getByText('2')).toBeInTheDocument()
  })

  it('renders empty placeholder when items is empty', () => {
    render(<ReviewColumn title="Approved" items={[]} onOpenCard={() => {}} />)
    expect(screen.getByText('Empty')).toBeInTheDocument()
  })

  it('renders one card per item', () => {
    render(
      <ReviewColumn
        title="Pending"
        items={[mkReview('r1', 'Alpha'), mkReview('r2', 'Beta')]}
        onOpenCard={() => {}}
      />,
    )
    expect(screen.getByText('Alpha')).toBeInTheDocument()
    expect(screen.getByText('Beta')).toBeInTheDocument()
  })
})
