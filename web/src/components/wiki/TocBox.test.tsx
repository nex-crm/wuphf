import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import TocBox from './TocBox'

describe('<TocBox>', () => {
  it('renders nested entries with level classes and numbers', () => {
    // Arrange
    const entries = [
      { level: 1 as const, num: '1', anchor: 'what', title: 'What they want' },
      { level: 2 as const, num: '1.1', anchor: 'stated', title: 'Stated goals' },
      { level: 2 as const, num: '1.2', anchor: 'unstated', title: 'Unstated goals' },
      { level: 1 as const, num: '2', anchor: 'open', title: 'Open issues' },
    ]
    // Act
    render(<TocBox entries={entries} />)
    // Assert
    const l1 = screen.getByRole('link', { name: /What they want/ })
    expect(l1).toHaveClass('wk-lvl-1')
    const l2 = screen.getByRole('link', { name: /Stated goals/ })
    expect(l2).toHaveClass('wk-lvl-2')
    expect(screen.getByText('1.1')).toBeInTheDocument()
  })

  it('hides entries when [hide] is clicked', async () => {
    // Arrange
    const user = userEvent.setup()
    const entries = [
      { level: 1 as const, num: '1', anchor: 'a', title: 'A' },
    ]
    render(<TocBox entries={entries} />)
    expect(screen.getByRole('link', { name: /A/ })).toBeInTheDocument()
    // Act
    await user.click(screen.getByRole('button', { name: /hide/i }))
    // Assert
    expect(screen.queryByRole('link', { name: /A/ })).not.toBeInTheDocument()
  })
})
