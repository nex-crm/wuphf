import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import WikiLink from './WikiLink'

describe('<WikiLink>', () => {
  it('renders a blue dashed wikilink for OK targets', () => {
    // Arrange + Act
    render(<WikiLink slug="people/nazz" display="Nazz" />)
    // Assert
    const link = screen.getByRole('link', { name: 'Nazz' })
    expect(link).toHaveClass('wk-wikilink')
    expect(link).not.toHaveClass('wk-broken')
    expect(link.getAttribute('data-broken')).toBe('false')
    expect(link.getAttribute('href')).toBe('#/wiki/people/nazz')
  })

  it('renders a red broken wikilink with marker when broken=true', () => {
    render(<WikiLink slug="missing" broken />)
    const link = screen.getByRole('link', { name: 'missing' })
    expect(link).toHaveClass('wk-broken')
    expect(link.getAttribute('data-broken')).toBe('true')
  })

  it('falls back to slug when display is omitted', () => {
    render(<WikiLink slug="policies/privacy" />)
    expect(screen.getByRole('link', { name: 'policies/privacy' })).toBeInTheDocument()
  })

  it('invokes onNavigate instead of following the href', async () => {
    // Arrange
    const onNavigate = vi.fn()
    const user = userEvent.setup()
    render(<WikiLink slug="people/nazz" onNavigate={onNavigate} />)
    // Act
    await user.click(screen.getByRole('link'))
    // Assert
    expect(onNavigate).toHaveBeenCalledWith('people/nazz')
  })

  it('navigates via href when onNavigate is not supplied', async () => {
    const user = userEvent.setup()
    render(<WikiLink slug="people/x" />)
    await user.click(screen.getByRole('link'))
    // No onNavigate is passed; the click simply uses the default href.
    expect(screen.getByRole('link').getAttribute('href')).toBe('#/wiki/people/x')
  })
})
