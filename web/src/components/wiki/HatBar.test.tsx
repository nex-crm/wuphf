import { describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import HatBar from './HatBar'

describe('<HatBar>', () => {
  it('marks the active tab and disables the Talk tab by default', () => {
    render(<HatBar active="article" />)
    const talk = screen.getByRole('button', { name: 'Talk' })
    expect(talk).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Article' })).toHaveClass('active')
  })

  it('fires onChange when a non-active, non-disabled tab is clicked', () => {
    const onChange = vi.fn()
    render(<HatBar active="article" onChange={onChange} />)
    fireEvent.click(screen.getByRole('button', { name: 'History' }))
    expect(onChange).toHaveBeenCalledWith('history')
  })

  it('does not fire onChange when a disabled tab is clicked', () => {
    const onChange = vi.fn()
    render(<HatBar active="article" onChange={onChange} />)
    fireEvent.click(screen.getByRole('button', { name: 'Talk' }))
    expect(onChange).not.toHaveBeenCalled()
  })

  it('renders right-rail context when provided', () => {
    render(<HatBar active="article" rightRail={['Cincinnati, OH', 'Mid-market Logistics']} />)
    expect(screen.getByText(/Cincinnati, OH/)).toBeInTheDocument()
    expect(screen.getByText(/Mid-market Logistics/)).toBeInTheDocument()
  })
})
