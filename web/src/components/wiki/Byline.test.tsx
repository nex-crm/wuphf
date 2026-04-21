import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import Byline from './Byline'

describe('<Byline>', () => {
  it('renders avatar, author, and timestamp pulse', () => {
    // Arrange
    const props = {
      authorSlug: 'ceo',
      authorName: 'CEO',
      lastEditedTs: new Date(Date.now() - 3 * 60 * 1000).toISOString(),
      startedDate: '2026-01-14',
      startedBy: 'PM',
      revisions: 47,
    }
    // Act
    render(<Byline {...props} />)
    // Assert
    expect(screen.getByText('CEO')).toBeInTheDocument()
    expect(screen.getByText('2026-01-14')).toBeInTheDocument()
    expect(screen.getByText('47 revisions')).toBeInTheDocument()
    expect(screen.getByTestId('wk-ts')).toBeInTheDocument()
  })

  it('renders without optional fields', () => {
    render(
      <Byline
        authorSlug="pm"
        authorName="PM"
        lastEditedTs={new Date().toISOString()}
      />,
    )
    expect(screen.getByText('PM')).toBeInTheDocument()
  })

  it('falls back to the raw timestamp when formatting fails', () => {
    render(
      <Byline
        authorSlug="pm"
        authorName="PM"
        lastEditedTs="not-a-date"
      />,
    )
    // The component does not throw.
    expect(screen.getByText('PM')).toBeInTheDocument()
  })

  it('renders a distinct "Human" pill when the last editor is the human', () => {
    render(
      <Byline
        authorSlug="human"
        authorName="Human"
        lastEditedTs={new Date().toISOString()}
      />,
    )
    const pill = screen.getByTestId('wk-human-byline')
    expect(pill).toBeInTheDocument()
    expect(pill).toHaveTextContent('Human')
    expect(pill.className).toMatch(/wk-human-pill/)
  })

  it('renders started without startedBy', () => {
    render(
      <Byline
        authorSlug="pm"
        authorName="PM"
        lastEditedTs={new Date().toISOString()}
        startedDate="2026-01-14"
        revisions={0}
      />,
    )
    expect(screen.getByText('2026-01-14')).toBeInTheDocument()
    expect(screen.queryByText(/revisions/)).not.toBeInTheDocument()
  })
})
