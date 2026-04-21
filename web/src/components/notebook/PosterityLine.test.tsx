import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import PosterityLine from './PosterityLine'

describe('<PosterityLine>', () => {
  it('names the author, reviewer, and file path', () => {
    render(
      <PosterityLine
        authorSlug="pm"
        reviewerSlug="ceo"
        filePath="~/.wuphf/wiki/agents/pm/notebook/2026-04-20-acme.md"
      />,
    )
    expect(screen.getByText('PM')).toBeInTheDocument()
    expect(screen.getByText('CEO')).toBeInTheDocument()
    expect(
      screen.getByText('~/.wuphf/wiki/agents/pm/notebook/2026-04-20-acme.md'),
    ).toBeInTheDocument()
  })

  it('says "a human" when reviewer is human-only', () => {
    render(
      <PosterityLine authorSlug="pm" reviewerSlug="human-only" filePath="f.md" />,
    )
    expect(screen.getByText('a human')).toBeInTheDocument()
  })
})
