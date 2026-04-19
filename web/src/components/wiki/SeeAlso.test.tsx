import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import SeeAlso from './SeeAlso'

describe('<SeeAlso>', () => {
  it('renders each item as a wikilink', () => {
    // Arrange
    const items = [
      { slug: 'people/sarah', display: 'Sarah Chen' },
      { slug: 'missing', display: 'Missing link', broken: true },
    ]
    // Act
    render(<SeeAlso items={items} />)
    // Assert
    expect(screen.getByRole('heading', { name: 'See also' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Sarah Chen' })).toBeInTheDocument()
    const broken = screen.getByRole('link', { name: 'Missing link' })
    expect(broken).toHaveClass('wk-broken')
  })

  it('renders nothing when empty', () => {
    const { container } = render(<SeeAlso items={[]} />)
    expect(container.firstChild).toBeNull()
  })
})
