import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, act } from '@testing-library/react'
import EditLogFooter from './EditLogFooter'
import * as api from '../../api/wiki'

describe('<EditLogFooter>', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('renders initial entries with the newest marked live', () => {
    const initial: api.WikiEditLogEntry[] = [
      { who: 'CEO', action: 'edited', article_path: 'people/customer-x', article_title: 'Customer X', timestamp: new Date().toISOString(), commit_sha: 'a' },
      { who: 'PM', action: 'updated', article_path: 'playbooks/churn', article_title: 'Churn', timestamp: new Date().toISOString(), commit_sha: 'b' },
    ]
    render(<EditLogFooter initialEntries={initial} />)
    const live = screen.getByTestId('wk-live-entry')
    expect(live).toHaveClass('wk-live')
    expect(screen.getByText('Customer X')).toBeInTheDocument()
    expect(screen.getByText('Churn')).toBeInTheDocument()
  })

  it('invokes onNavigate on entry click instead of navigating', () => {
    const onNavigate = vi.fn()
    vi.spyOn(api, 'subscribeEditLog').mockImplementation(() => () => {})
    const initial: api.WikiEditLogEntry[] = [
      { who: 'CEO', action: 'edited', article_path: 'people/x', article_title: 'X', timestamp: new Date().toISOString(), commit_sha: 'a' },
    ]
    render(<EditLogFooter initialEntries={initial} onNavigate={onNavigate} />)
    const link = screen.getByText('X')
    link.click()
    expect(onNavigate).toHaveBeenCalledWith('people/x')
  })

  it('prepends new entries from the subscription', async () => {
    // Arrange: capture handler and push to it during the test.
    type Handler = (e: api.WikiEditLogEntry) => void
    let handler: Handler | null = null
    vi.spyOn(api, 'subscribeEditLog').mockImplementation((h: Handler) => {
      handler = h
      return () => {}
    })
    const initial: api.WikiEditLogEntry[] = [
      { who: 'PM', action: 'updated', article_path: 'playbooks/churn', article_title: 'Churn', timestamp: new Date().toISOString(), commit_sha: 'b' },
    ]
    // Act
    render(<EditLogFooter initialEntries={initial} />)
    act(() => {
      handler?.({
        who: 'Designer',
        action: 'created',
        article_path: 'brand/voice',
        article_title: 'Brand Voice',
        timestamp: new Date().toISOString(),
        commit_sha: 'c',
      })
    })
    // Assert
    const live = screen.getByTestId('wk-live-entry')
    expect(live).toHaveTextContent('Brand Voice')
    expect(screen.getByText('Churn')).toBeInTheDocument()
  })
})
