import { describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import ToolsNav from './ToolsNav'

describe('<ToolsNav>', () => {
  it('renders the default Tools list', () => {
    render(<ToolsNav />)
    expect(screen.getByText('Tools')).toBeInTheDocument()
    expect(screen.getByText('Recent changes')).toBeInTheDocument()
    expect(screen.getByText('Random article')).toBeInTheDocument()
  })

  it('calls per-item onClick when provided', () => {
    const onClick = vi.fn()
    render(<ToolsNav items={[{ label: 'Custom tool', onClick }]} />)
    fireEvent.click(screen.getByText('Custom tool'))
    expect(onClick).toHaveBeenCalledTimes(1)
  })
})
