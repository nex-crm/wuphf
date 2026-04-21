import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import EntityBriefBar from './EntityBriefBar'
import * as api from '../../api/entity'

type FactCb = (ev: api.FactRecordedEvent) => void
type SynthCb = (ev: api.BriefSynthesizedEvent) => void

function fakeBrief(pending: number): api.BriefSummary {
  return {
    kind: 'people',
    slug: 'sarah-chen',
    title: 'Sarah Chen',
    fact_count: 5,
    last_synthesized_ts: new Date(Date.now() - 60 * 60 * 1000).toISOString(),
    last_synthesized_sha: 'abcdef1',
    pending_delta: pending,
  }
}

describe('<EntityBriefBar>', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    // Default SSE subscribe = no-op unless a test overrides.
    vi.spyOn(api, 'subscribeEntityEvents').mockImplementation(() => () => {})
  })

  it('renders the muted "0 new facts" state when pending is 0', async () => {
    vi.spyOn(api, 'fetchBriefs').mockResolvedValue([fakeBrief(0)])
    render(<EntityBriefBar kind="people" slug="sarah-chen" />)
    await waitFor(() =>
      expect(screen.getByTestId('wk-entity-brief-bar')).toHaveClass('wk-entity-brief-bar--clean'),
    )
    expect(screen.getByTestId('wk-entity-brief-bar').textContent).toMatch(/0 new facts/i)
    expect(screen.queryByRole('button', { name: /refresh brief/i })).toBeNull()
  })

  it('renders the amber pending state with a Refresh button when pending > 0', async () => {
    vi.spyOn(api, 'fetchBriefs').mockResolvedValue([fakeBrief(3)])
    render(<EntityBriefBar kind="people" slug="sarah-chen" />)
    await waitFor(() =>
      expect(screen.getByTestId('wk-entity-brief-bar')).toHaveClass('wk-entity-brief-bar--pending'),
    )
    expect(screen.getByTestId('wk-entity-brief-bar').textContent).toMatch(/3 new facts/i)
    expect(screen.getByRole('button', { name: /refresh brief/i })).toBeInTheDocument()
  })

  it('fires a synthesis request on click and disables the button in-flight', async () => {
    vi.spyOn(api, 'fetchBriefs').mockResolvedValue([fakeBrief(2)])
    const synthSpy = vi.spyOn(api, 'requestBriefSynthesis').mockResolvedValue({
      synthesis_id: 'synth-1',
      queued_at: new Date().toISOString(),
    })
    render(<EntityBriefBar kind="people" slug="sarah-chen" />)
    const btn = (await screen.findByRole('button', {
      name: /refresh brief/i,
    })) as HTMLButtonElement
    await userEvent.click(btn)
    expect(synthSpy).toHaveBeenCalledWith({
      entity_kind: 'people',
      entity_slug: 'sarah-chen',
    })
    // After click, button shows in-flight label + is disabled.
    await waitFor(() => {
      expect(btn).toBeDisabled()
      expect(btn.textContent).toMatch(/synthesizing/i)
    })
  })

  it('clears the in-flight state when an entity:brief_synthesized event arrives', async () => {
    let synthCb: SynthCb = () => {}
    vi.spyOn(api, 'subscribeEntityEvents').mockImplementation(
      (_kind, _slug, _f: FactCb, s: SynthCb) => {
        synthCb = s
        return () => {}
      },
    )
    // First load returns pending=2; post-synth fetch returns pending=0.
    const fetchSpy = vi.spyOn(api, 'fetchBriefs')
    fetchSpy.mockResolvedValueOnce([fakeBrief(2)])
    fetchSpy.mockResolvedValueOnce([fakeBrief(0)])
    vi.spyOn(api, 'requestBriefSynthesis').mockResolvedValue({
      synthesis_id: 'synth-1',
      queued_at: new Date().toISOString(),
    })

    const onSynth = vi.fn()
    render(<EntityBriefBar kind="people" slug="sarah-chen" onSynthesized={onSynth} />)
    const btn = (await screen.findByRole('button', {
      name: /refresh brief/i,
    })) as HTMLButtonElement
    await userEvent.click(btn)
    await waitFor(() => expect(btn).toBeDisabled())

    // Fire the SSE synthesis event.
    await act(async () => {
      synthCb({
        kind: 'people',
        slug: 'sarah-chen',
        commit_sha: 'deadbee',
        fact_count: 5,
        synthesized_ts: new Date().toISOString(),
      })
    })

    await waitFor(() => {
      // Pending=0 → refresh button goes away entirely.
      expect(screen.queryByRole('button', { name: /refresh brief/i })).toBeNull()
      expect(onSynth).toHaveBeenCalled()
    })
  })

  it('increments pending count live when fact_recorded fires', async () => {
    let factCb: FactCb = () => {}
    vi.spyOn(api, 'subscribeEntityEvents').mockImplementation(
      (_kind, _slug, f: FactCb) => {
        factCb = f
        return () => {}
      },
    )
    vi.spyOn(api, 'fetchBriefs').mockResolvedValue([fakeBrief(1)])
    render(<EntityBriefBar kind="people" slug="sarah-chen" />)
    await waitFor(() =>
      expect(screen.getByTestId('wk-entity-brief-bar').textContent).toMatch(/1 new fact/i),
    )

    await act(async () => {
      factCb({
        kind: 'people',
        slug: 'sarah-chen',
        fact_id: 'f-new',
        recorded_by: 'ceo',
        fact_count: 6,
        threshold_crossed: false,
        timestamp: new Date().toISOString(),
      })
    })
    await waitFor(() =>
      expect(screen.getByTestId('wk-entity-brief-bar').textContent).toMatch(/2 new facts/i),
    )
  })
})
