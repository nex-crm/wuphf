import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, act } from '@testing-library/react'
import FactsOnFile from './FactsOnFile'
import * as api from '../../api/entity'

type FactCb = (ev: api.FactRecordedEvent) => void

describe('<FactsOnFile>', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.spyOn(api, 'subscribeEntityEvents').mockImplementation(() => () => {})
  })

  it('renders the empty state when no facts are recorded', async () => {
    vi.spyOn(api, 'fetchFacts').mockResolvedValue([])
    render(<FactsOnFile kind="people" slug="sarah-chen" />)
    await waitFor(() =>
      expect(screen.getByText(/0 facts recorded yet/i)).toBeInTheDocument(),
    )
    expect(screen.getByRole('heading', { name: /facts on file/i })).toBeInTheDocument()
  })

  it('renders a fact list with author names and timestamps', async () => {
    const facts: api.Fact[] = [
      {
        id: 'f1',
        kind: 'people',
        slug: 'sarah-chen',
        text: 'Prefers async updates over meetings.',
        recorded_by: 'pm',
        created_at: '2026-04-14T00:00:00Z',
      },
      {
        id: 'f2',
        kind: 'people',
        slug: 'sarah-chen',
        text: 'Champion inside Customer X.',
        recorded_by: 'ceo',
        source_path: 'team/companies/customer-x.md',
        created_at: '2026-04-15T00:00:00Z',
      },
    ]
    vi.spyOn(api, 'fetchFacts').mockResolvedValue(facts)
    render(<FactsOnFile kind="people" slug="sarah-chen" />)
    await screen.findByText('Prefers async updates over meetings.')
    expect(screen.getByText('Champion inside Customer X.')).toBeInTheDocument()
    // Source wikilink rendered for team/ paths.
    const source = screen.getByText(/companies\/customer-x/)
    expect(source.closest('a')).toHaveAttribute('data-wikilink', 'true')
    // Shortened ISO date.
    expect(screen.getByText('2026-04-14')).toBeInTheDocument()
  })

  it('does not render a source wikilink for non-wiki source paths', async () => {
    const facts: api.Fact[] = [
      {
        id: 'f1',
        kind: 'people',
        slug: 'sarah-chen',
        text: 'Observed in a Slack DM.',
        recorded_by: 'pm',
        source_path: 'messages/dm/123',
        created_at: '2026-04-14T00:00:00Z',
      },
    ]
    vi.spyOn(api, 'fetchFacts').mockResolvedValue(facts)
    render(<FactsOnFile kind="people" slug="sarah-chen" />)
    await screen.findByText('Observed in a Slack DM.')
    expect(screen.queryByText(/messages\/dm/)).toBeNull()
  })

  it('renders typed fields (type, confidence, triplet, validity, supersedes) when present', async () => {
    const facts: api.Fact[] = [
      {
        id: 'f_typed',
        kind: 'people',
        slug: 'sarah-jones',
        text: 'Sarah was promoted to VP of Sales at Acme Corp on 2026-04-10.',
        recorded_by: 'archivist',
        created_at: '2026-04-22T13:01:00Z',
        type: 'status',
        confidence: 0.92,
        triplet: {
          subject: 'sarah-jones',
          predicate: 'role_at',
          object: 'company:acme-corp',
        },
        valid_from: '2026-04-10T00:00:00Z',
        valid_until: null,
        supersedes: ['prior_role_head_of_marketing'],
        reinforced_at: '2026-04-20T09:15:00Z',
      },
    ]
    vi.spyOn(api, 'fetchFacts').mockResolvedValue(facts)
    render(<FactsOnFile kind="people" slug="sarah-jones" />)
    await screen.findByText(/promoted to VP of Sales/)
    // Type enum rendered.
    expect(screen.getByText('status')).toBeInTheDocument()
    // Confidence shown to 2dp.
    expect(screen.getByText('0.92')).toBeInTheDocument()
    // Triplet predicate rendered as code.
    expect(screen.getByText('role_at')).toBeInTheDocument()
    // Validity formatted.
    expect(screen.getByText(/valid from 2026-04-10/)).toBeInTheDocument()
    // Reinforced hint rendered.
    expect(screen.getByText(/reinforced 2026-04-20/)).toBeInTheDocument()
    // Supersedes hint rendered.
    expect(screen.getByText(/supersedes 1 prior/)).toBeInTheDocument()
  })

  it('falls back to legacy rendering when typed fields are absent', async () => {
    const facts: api.Fact[] = [
      {
        id: 'f_legacy',
        kind: 'people',
        slug: 'michael-chen',
        text: 'Legacy v1.2 fact without typed fields.',
        recorded_by: 'pm',
        created_at: '2026-04-14T00:00:00Z',
      },
    ]
    vi.spyOn(api, 'fetchFacts').mockResolvedValue(facts)
    render(<FactsOnFile kind="people" slug="michael-chen" />)
    await screen.findByText('Legacy v1.2 fact without typed fields.')
    // No type badge, no confidence number, no validity, no reinforcement.
    expect(screen.queryByText('status')).toBeNull()
    expect(screen.queryByText(/valid /)).toBeNull()
    expect(screen.queryByText(/reinforced /)).toBeNull()
    expect(screen.queryByText(/supersedes /)).toBeNull()
  })

  it('prepends a new fact when an entity:fact_recorded event arrives', async () => {
    let factCb: FactCb = () => {}
    vi.spyOn(api, 'subscribeEntityEvents').mockImplementation(
      (_k, _s, cb: FactCb) => {
        factCb = cb
        return () => {}
      },
    )
    const fetchSpy = vi.spyOn(api, 'fetchFacts')
    fetchSpy.mockResolvedValueOnce([
      {
        id: 'f1',
        kind: 'people',
        slug: 'sarah-chen',
        text: 'First fact.',
        recorded_by: 'pm',
        created_at: '2026-04-14T00:00:00Z',
      },
    ])
    // Refetch after SSE event — returns with the new fact at the top.
    fetchSpy.mockResolvedValueOnce([
      {
        id: 'f2',
        kind: 'people',
        slug: 'sarah-chen',
        text: 'Fresh fact from SSE.',
        recorded_by: 'ceo',
        created_at: '2026-04-15T00:00:00Z',
      },
      {
        id: 'f1',
        kind: 'people',
        slug: 'sarah-chen',
        text: 'First fact.',
        recorded_by: 'pm',
        created_at: '2026-04-14T00:00:00Z',
      },
    ])

    render(<FactsOnFile kind="people" slug="sarah-chen" />)
    await screen.findByText('First fact.')

    await act(async () => {
      factCb({
        kind: 'people',
        slug: 'sarah-chen',
        fact_id: 'f2',
        recorded_by: 'ceo',
        fact_count: 2,
        threshold_crossed: false,
        timestamp: '2026-04-15T00:00:00Z',
      })
    })

    await waitFor(() =>
      expect(screen.getByText('Fresh fact from SSE.')).toBeInTheDocument(),
    )
  })
})
