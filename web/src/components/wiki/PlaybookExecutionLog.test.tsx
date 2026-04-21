import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import PlaybookExecutionLog from './PlaybookExecutionLog'
import * as api from '../../api/playbook'

describe('<PlaybookExecutionLog>', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.spyOn(api, 'subscribePlaybookEvents').mockImplementation(() => () => {})
    vi.spyOn(api, 'subscribePlaybookSynthesizedEvents').mockImplementation(
      () => () => {},
    )
    vi.spyOn(api, 'fetchSynthesisStatus').mockResolvedValue(null)
  })

  it('renders the empty state when no executions exist', async () => {
    vi.spyOn(api, 'fetchPlaybookExecutions').mockResolvedValue([])
    render(<PlaybookExecutionLog slug="churn-prevention" />)
    // The panel starts collapsed — click to expand.
    const toggle = await screen.findByRole('button', { name: /execution log/i })
    fireEvent.click(toggle)
    await waitFor(() =>
      expect(screen.getByText(/no executions recorded yet/i)).toBeInTheDocument(),
    )
  })

  it('renders execution entries newest first with outcome pill', async () => {
    vi.spyOn(api, 'fetchPlaybookExecutions').mockResolvedValue([
      {
        id: 'e1',
        slug: 'churn-prevention',
        outcome: 'partial',
        summary: 'Blocked on legal review.',
        notes: 'Owner paged.',
        recorded_by: 'cmo',
        created_at: '2026-04-20T12:00:00Z',
      },
      {
        id: 'e2',
        slug: 'churn-prevention',
        outcome: 'success',
        summary: 'Saved the account.',
        recorded_by: 'cmo',
        created_at: '2026-04-19T09:00:00Z',
      },
    ])
    render(<PlaybookExecutionLog slug="churn-prevention" />)
    fireEvent.click(await screen.findByRole('button', { name: /execution log/i }))
    await screen.findByText('Blocked on legal review.')
    expect(screen.getByText('Saved the account.')).toBeInTheDocument()
    expect(screen.getByText('Owner paged.')).toBeInTheDocument()
    // Outcome pills use uppercase labels.
    expect(screen.getByText('partial')).toBeInTheDocument()
    expect(screen.getByText('success')).toBeInTheDocument()
  })

  it('shows the last-synthesis badge when status is available', async () => {
    vi.spyOn(api, 'fetchPlaybookExecutions').mockResolvedValue([])
    const ts = new Date(Date.now() - 2 * 60 * 60 * 1000).toISOString() // 2h ago
    vi.spyOn(api, 'fetchSynthesisStatus').mockResolvedValue({
      slug: 'churn-prevention',
      source_path: 'team/playbooks/churn-prevention.md',
      execution_count: 7,
      last_synthesized_ts: ts,
      last_synthesized_sha: 'abc1234',
      executions_since_last_synthesis: 2,
      threshold: 3,
    })
    render(<PlaybookExecutionLog slug="churn-prevention" />)
    fireEvent.click(await screen.findByRole('button', { name: /execution log/i }))
    await waitFor(() => {
      expect(screen.getByTestId('wk-playbook-synthesis')).toBeInTheDocument()
    })
    // "2h ago · 7 executions"
    expect(screen.getByText(/2h ago · 7 executions/i)).toBeInTheDocument()
    expect(
      screen.getByText(/2 new executions since last synthesis/i),
    ).toBeInTheDocument()
  })

  it('posts to synthesizeNow when Re-synthesize is clicked', async () => {
    vi.spyOn(api, 'fetchPlaybookExecutions').mockResolvedValue([])
    const spy = vi
      .spyOn(api, 'synthesizeNow')
      .mockResolvedValue({ synthesis_id: 1, queued_at: '2026-04-21T10:00:00Z' })
    render(<PlaybookExecutionLog slug="churn-prevention" />)
    fireEvent.click(await screen.findByRole('button', { name: /execution log/i }))
    const button = await screen.findByTestId('wk-playbook-synthesis-button')
    fireEvent.click(button)
    await waitFor(() => {
      expect(spy).toHaveBeenCalledWith('churn-prevention')
    })
    // The button transitions to the pending label immediately.
    await waitFor(() =>
      expect(screen.getByTestId('wk-playbook-synthesis-button')).toHaveTextContent(
        /synthesizing/i,
      ),
    )
  })

  it('exposes the count next to the heading even when collapsed', async () => {
    vi.spyOn(api, 'fetchPlaybookExecutions').mockResolvedValue([
      {
        id: 'e1',
        slug: 'churn-prevention',
        outcome: 'aborted',
        summary: 'Account churned anyway.',
        recorded_by: 'cmo',
        created_at: '2026-04-18T00:00:00Z',
      },
    ])
    render(<PlaybookExecutionLog slug="churn-prevention" />)
    await waitFor(() =>
      expect(screen.getByText('(1)')).toBeInTheDocument(),
    )
  })
})
