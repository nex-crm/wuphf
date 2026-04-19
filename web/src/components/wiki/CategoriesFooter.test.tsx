import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import CategoriesFooter from './CategoriesFooter'

describe('<CategoriesFooter>', () => {
  it('renders a chip per tag', () => {
    render(<CategoriesFooter tags={['Mid-market', 'Logistics', 'Q1 2026']} />)
    expect(screen.getByText('Categories:')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Mid-market' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Logistics' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Q1 2026' })).toBeInTheDocument()
  })

  it('fires onSelect instead of navigating', async () => {
    // Arrange
    const onSelect = vi.fn()
    const user = userEvent.setup()
    render(<CategoriesFooter tags={['Logistics']} onSelect={onSelect} />)
    // Act
    await user.click(screen.getByRole('link', { name: 'Logistics' }))
    // Assert
    expect(onSelect).toHaveBeenCalledWith('Logistics')
  })

  it('renders nothing when empty', () => {
    const { container } = render(<CategoriesFooter tags={[]} />)
    expect(container.firstChild).toBeNull()
  })
})
