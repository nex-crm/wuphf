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

describe('isSuperseded rendering (Fix C5)', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.spyOn(api, 'subscribeEntityEvents').mockImplementation(() => () => {})
  })

  it('renders data-superseded=true on a fact whose valid_until is set', async () => {
    const facts: api.Fact[] = [
      {
        id: 'f-superseded',
        kind: 'people',
        slug: 'sarah-chen',
        text: 'Old title: Head of Marketing.',
        recorded_by: 'archivist',
        created_at: '2026-03-01T00:00:00Z',
        valid_until: '2026-04-10',
      },
    ]
    vi.spyOn(api, 'fetchFacts').mockResolvedValue(facts)
    render(<FactsOnFile kind="people" slug="sarah-chen" />)
    await screen.findByText('Old title: Head of Marketing.')
    const li = screen.getByText('Old title: Head of Marketing.').closest('li')
    expect(li).toHaveAttribute('data-superseded', 'true')
  })

  it('does NOT render data-superseded on a fact that supersedes others but has no valid_until', async () => {
    const facts: api.Fact[] = [
      {
        id: 'f-newer',
        kind: 'people',
        slug: 'sarah-chen',
        text: 'Promoted to VP of Sales.',
        recorded_by: 'archivist',
        created_at: '2026-04-10T00:00:00Z',
        supersedes: ['prior-role-fact-abc'],
        valid_until: null,
      },
    ]
    vi.spyOn(api, 'fetchFacts').mockResolvedValue(facts)
    render(<FactsOnFile kind="people" slug="sarah-chen" />)
    await screen.findByText('Promoted to VP of Sales.')
    const li = screen.getByText('Promoted to VP of Sales.').closest('li')
    expect(li).not.toHaveAttribute('data-superseded')
  })

  it('does NOT render data-superseded on a fact with neither valid_until nor supersedes', async () => {
    const facts: api.Fact[] = [
      {
        id: 'f-plain',
        kind: 'people',
        slug: 'sarah-chen',
        text: 'Prefers async updates.',
        recorded_by: 'pm',
        created_at: '2026-04-01T00:00:00Z',
      },
    ]
    vi.spyOn(api, 'fetchFacts').mockResolvedValue(facts)
    render(<FactsOnFile kind="people" slug="sarah-chen" />)
    await screen.findByText('Prefers async updates.')
    const li = screen.getByText('Prefers async updates.').closest('li')
    expect(li).not.toHaveAttribute('data-superseded')
  })
})

describe('isWikiSource source-path rendering (Fix M15)', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.spyOn(api, 'subscribeEntityEvents').mockImplementation(() => () => {})
  })

  const wikiPaths = [
    'wiki/artifacts/agent-pm/abc123.md',
    'team/people/sarah-chen.md',
    'wiki/facts/person/sarah-chen.jsonl',
    'wiki/insights/entity/sarah-chen.jsonl',
    'wiki/playbooks/onboarding-new-account.md',
    'agents/pm/notebook.md',
  ]

  for (const sourcePath of wikiPaths) {
    it(`renders a wikilink for schema path: ${sourcePath}`, async () => {
      const facts: api.Fact[] = [
        {
          id: 'f-wiki-path',
          kind: 'people',
          slug: 'sarah-chen',
          text: `Fact with wiki source path ${sourcePath}.`,
          recorded_by: 'archivist',
          created_at: '2026-04-20T00:00:00Z',
          source_path: sourcePath,
        },
      ]
      vi.spyOn(api, 'fetchFacts').mockResolvedValue(facts)
      render(<FactsOnFile kind="people" slug="sarah-chen" />)
      await screen.findByText(`Fact with wiki source path ${sourcePath}.`)
      const link = document.querySelector('a[data-wikilink="true"]')
      expect(link).not.toBeNull()
    })
  }

  it('does NOT render a wikilink for a non-wiki source path (messages/dm/123)', async () => {
    const facts: api.Fact[] = [
      {
        id: 'f-dm',
        kind: 'people',
        slug: 'sarah-chen',
        text: 'Fact from a Slack DM.',
        recorded_by: 'pm',
        created_at: '2026-04-20T00:00:00Z',
        source_path: 'messages/dm/123',
      },
    ]
    vi.spyOn(api, 'fetchFacts').mockResolvedValue(facts)
    render(<FactsOnFile kind="people" slug="sarah-chen" />)
    await screen.findByText('Fact from a Slack DM.')
    expect(document.querySelector('a[data-wikilink="true"]')).toBeNull()
  })
})
