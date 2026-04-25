import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import ResolveContradictionModal from './ResolveContradictionModal'
import * as wikiApi from '../../api/wiki'
import * as toast from '../ui/Toast'

const FINDING: wikiApi.LintFinding = {
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

describe('<ResolveContradictionModal>', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('shows a spinner on the clicked button while the request is in flight', async () => {
    // Never-resolving promise so the pending state is observable. We
    // intentionally never settle it: the spinner-visible state *is* the
    // assertion, and settling it after the test body returns would schedule
    // state updates outside an awaited act() cycle (CodeRabbit #259). Test
    // cleanup unmounts the component, which discards the pending promise.
    vi.spyOn(wikiApi, 'resolveContradiction').mockImplementation(
      () => new Promise(() => { /* never resolves */ }),
    )

    render(
      <ResolveContradictionModal
        finding={FINDING}
        findingIdx={0}
        reportDate="2026-04-22"
        onClose={vi.fn()}
        onResolved={vi.fn()}
      />,
    )

    const pickA = screen.getByTestId('wk-resolve-pick-a')
    const pickB = screen.getByTestId('wk-resolve-pick-b')
    fireEvent.click(pickA)

    await waitFor(() => {
      expect(pickA.getAttribute('aria-busy')).toBe('true')
    })
    // Matches CitedAnswer's convention: omit aria-busy when not busy rather
    // than write aria-busy="false". Keeps the DOM quiet for assistive tech.
    expect(pickB.hasAttribute('aria-busy')).toBe(false)
    // Spinner renders inside the clicked button only — siblings stay quiet.
    expect(pickA.querySelector('.wk-spinner')).toBeTruthy()
    expect(pickB.querySelector('.wk-spinner')).toBeNull()
    expect(pickA.textContent).toContain('Resolving')
    // All three pick buttons are disabled; cancel is also disabled while
    // the request is in flight so the user can't drop the modal mid-write.
    expect(pickA).toBeDisabled()
    expect(pickB).toBeDisabled()
    expect(screen.getByTestId('wk-resolve-pick-both')).toBeDisabled()
    expect(screen.getByTestId('wk-resolve-cancel')).toBeDisabled()
  })

  it('surfaces errors inline (no toast) and re-enables the buttons', async () => {
    vi.spyOn(wikiApi, 'resolveContradiction').mockRejectedValue(
      new Error('broker offline'),
    )
    const showNoticeSpy = vi.spyOn(toast, 'showNotice')
    const onResolved = vi.fn()

    render(
      <ResolveContradictionModal
        finding={FINDING}
        findingIdx={0}
        reportDate="2026-04-22"
        onClose={vi.fn()}
        onResolved={onResolved}
      />,
    )

    fireEvent.click(screen.getByTestId('wk-resolve-pick-a'))

    // Error string renders inline via the shared error banner, not a toast.
    const banner = await screen.findByRole('alert')
    expect(banner.textContent).toContain('broker offline')
    expect(onResolved).not.toHaveBeenCalled()
    expect(showNoticeSpy).not.toHaveBeenCalled()
    // Buttons re-enabled after the error so the user can retry.
    expect(screen.getByTestId('wk-resolve-pick-a')).not.toBeDisabled()
  })

  it('on success fires a short-sha success toast and calls onResolved', async () => {
    vi.spyOn(wikiApi, 'resolveContradiction').mockResolvedValue({
      commit_sha: 'abcdef1234567890',
      message: 'resolved',
    })
    const showNoticeSpy = vi.spyOn(toast, 'showNotice')
    const onResolved = vi.fn()

    render(
      <ResolveContradictionModal
        finding={FINDING}
        findingIdx={0}
        reportDate="2026-04-22"
        onClose={vi.fn()}
        onResolved={onResolved}
      />,
    )

    fireEvent.click(screen.getByTestId('wk-resolve-pick-both'))

    await waitFor(() => expect(onResolved).toHaveBeenCalled())
    expect(showNoticeSpy).toHaveBeenCalledWith('Resolved. Commit abcdef1.', 'success')
  })

  it('falls back to a plain "Resolved." toast if the broker omits the sha', async () => {
    vi.spyOn(wikiApi, 'resolveContradiction').mockResolvedValue({
      commit_sha: '',
      message: 'resolved',
    })
    const showNoticeSpy = vi.spyOn(toast, 'showNotice')

    render(
      <ResolveContradictionModal
        finding={FINDING}
        findingIdx={0}
        reportDate="2026-04-22"
        onClose={vi.fn()}
        onResolved={vi.fn()}
      />,
    )

    fireEvent.click(screen.getByTestId('wk-resolve-pick-a'))

    await waitFor(() => expect(showNoticeSpy).toHaveBeenCalled())
    expect(showNoticeSpy).toHaveBeenCalledWith('Resolved.', 'success')
  })
})
