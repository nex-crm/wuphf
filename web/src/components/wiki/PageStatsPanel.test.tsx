import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import PageStatsPanel from './PageStatsPanel'

describe('<PageStatsPanel>', () => {
  it('renders all stat rows', () => {
    // Arrange
    const props = {
      revisions: 47,
      contributors: 6,
      wordCount: 2347,
      created: '2026-01-14T00:00:00Z',
      lastEdit: new Date(Date.now() - 3 * 60 * 1000).toISOString(),
      viewed: 128,
    }
    // Act
    render(<PageStatsPanel {...props} />)
    // Assert
    expect(screen.getByText('Page stats')).toBeInTheDocument()
    expect(screen.getByText('47')).toBeInTheDocument()
    expect(screen.getByText('6 agents')).toBeInTheDocument()
    expect(screen.getByText('2,347')).toBeInTheDocument()
    expect(screen.getByText('2026-01-14')).toBeInTheDocument()
    expect(screen.getByText('128 times')).toBeInTheDocument()
  })

  it('omits the Viewed row when undefined', () => {
    render(
      <PageStatsPanel
        revisions={1}
        contributors={1}
        wordCount={100}
        created="2026-01-14T00:00:00Z"
        lastEdit="2026-01-14T00:00:00Z"
      />,
    )
    expect(screen.queryByText('Viewed')).not.toBeInTheDocument()
  })

  it('gracefully handles invalid date strings', () => {
    render(
      <PageStatsPanel
        revisions={0}
        contributors={0}
        wordCount={0}
        created="not-a-date"
        lastEdit="also-not-a-date"
      />,
    )
    expect(screen.getByText('Page stats')).toBeInTheDocument()
  })
})
