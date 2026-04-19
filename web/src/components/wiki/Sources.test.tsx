import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import Sources from './Sources'

describe('<Sources>', () => {
  it('renders numbered commit references', () => {
    // Arrange
    const items = [
      {
        commitSha: '3f9a21b',
        authorSlug: 'pm',
        authorName: 'PM',
        msg: 'Initial brief',
        date: '2026-01-16T00:00:00Z',
      },
      {
        commitSha: '7c2e8810000',
        authorSlug: 'ceo',
        authorName: 'CEO',
        msg: 'Pilot update',
        date: '2026-01-17T00:00:00Z',
      },
    ]
    // Act
    render(<Sources items={items} />)
    // Assert
    expect(screen.getByRole('heading', { name: 'Sources' })).toBeInTheDocument()
    expect(screen.getByText('Initial brief')).toBeInTheDocument()
    expect(screen.getByText('Pilot update')).toBeInTheDocument()
    // Commit SHAs rendered as first 7 chars
    expect(screen.getByText('3f9a21b')).toBeInTheDocument()
    expect(screen.getByText('7c2e881')).toBeInTheDocument()
  })

  it('renders nothing when empty', () => {
    const { container } = render(<Sources items={[]} />)
    expect(container.firstChild).toBeNull()
  })

  it('renders gracefully when dates are malformed', () => {
    render(
      <Sources
        items={[
          { commitSha: 'a1b2c3d', authorSlug: 'pm', msg: 'X', date: 'bad-date' },
        ]}
      />,
    )
    expect(screen.getByText('X')).toBeInTheDocument()
  })
})
