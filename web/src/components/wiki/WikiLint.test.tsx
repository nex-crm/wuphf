import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import WikiLint from './WikiLint'
import * as wikiApi from '../../api/wiki'

const EMPTY_REPORT: wikiApi.LintReport = { date: '2026-04-22', findings: [] }

const CRITICAL_FINDING: wikiApi.LintFinding = {
  severity: 'critical',
  type: 'contradictions',
  entity_slug: 'sarah-chen',
  fact_ids: ['f1', 'f2'],
  summary: 'role_at predicate has two conflicting values.',
  resolve_actions: [
    'Fact A (id: f1): Sarah is Head of Marketing.',
    'Fact B (id: f2): Sarah is VP of Sales.',
    'Both',
  ],
}

const REPORT_WITH_CRITICAL: wikiApi.LintReport = {
  date: '2026-04-22',
  findings: [CRITICAL_FINDING],
}

describe('<WikiLint>', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('renders empty state when report has 0 findings', async () => {
    vi.spyOn(wikiApi, 'runLint').mockResolvedValue(EMPTY_REPORT)
    render(<WikiLint onNavigate={vi.fn()} />)
    await waitFor(() =>
      expect(screen.getByTestId('wk-lint-empty')).toBeInTheDocument(),
    )
    expect(screen.getByText(/all clear/i)).toBeInTheDocument()
  })

  it('critical finding row has aria-label="Needs attention finding"', async () => {
    vi.spyOn(wikiApi, 'runLint').mockResolvedValue(REPORT_WITH_CRITICAL)
    render(<WikiLint onNavigate={vi.fn()} />)
    // Wait for the findings table to appear.
    const severityBadge = await screen.findByLabelText('Needs attention finding')
    expect(severityBadge).toBeInTheDocument()
    expect(severityBadge.textContent).toBe('Needs attention')
  })

  it('clicking Resolve opens modal, picking A calls resolveContradiction with winner=A', async () => {
    vi.spyOn(wikiApi, 'runLint').mockResolvedValue(REPORT_WITH_CRITICAL)
    const resolveSpy = vi
      .spyOn(wikiApi, 'resolveContradiction')
      .mockResolvedValue({ commit_sha: 'abc1234', message: 'resolved' })
    // Re-run after resolve returns a clean report.
    vi.spyOn(wikiApi, 'runLint').mockResolvedValueOnce(REPORT_WITH_CRITICAL).mockResolvedValue(EMPTY_REPORT)

    render(<WikiLint onNavigate={vi.fn()} />)

    // Wait for the table row with the Resolve button.
    const resolveBtn = await screen.findByRole('button', { name: /resolve/i })
    fireEvent.click(resolveBtn)

    // Modal appears.
    expect(screen.getByTestId('wk-resolve-modal')).toBeInTheDocument()

    // Pick Fact A.
    const pickA = screen.getByTestId('wk-resolve-pick-a')
    fireEvent.click(pickA)

    await waitFor(() =>
      expect(resolveSpy).toHaveBeenCalledWith({
        report_date: '2026-04-22',
        finding_idx: 0,
        winner: 'A',
      }),
    )
  })

  it('modal Escape key closes without submitting', async () => {
    vi.spyOn(wikiApi, 'runLint').mockResolvedValue(REPORT_WITH_CRITICAL)
    const resolveSpy = vi.spyOn(wikiApi, 'resolveContradiction').mockResolvedValue({
      commit_sha: 'abc1234',
      message: 'resolved',
    })

    render(<WikiLint onNavigate={vi.fn()} />)

    // Open the modal.
    const resolveBtn = await screen.findByRole('button', { name: /resolve/i })
    fireEvent.click(resolveBtn)
    expect(screen.getByTestId('wk-resolve-modal')).toBeInTheDocument()

    // Press Escape.
    fireEvent.keyDown(window, { key: 'Escape' })

    // Modal gone, resolveContradiction never called.
    await waitFor(() =>
      expect(screen.queryByTestId('wk-resolve-modal')).not.toBeInTheDocument(),
    )
    expect(resolveSpy).not.toHaveBeenCalled()
  })
})
