import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import EntityRelatedPanel from './EntityRelatedPanel'
import * as api from '../../api/entity'

describe('<EntityRelatedPanel>', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.spyOn(api, 'subscribeEntityEvents').mockImplementation(() => () => {})
  })

  it('renders the empty state when the graph has no edges', async () => {
    vi.spyOn(api, 'fetchEntityGraph').mockResolvedValue([])
    render(<EntityRelatedPanel kind="people" slug="sarah" />)
    await waitFor(() =>
      expect(screen.getByText(/No related entities yet/i)).toBeInTheDocument(),
    )
    expect(screen.getByRole('heading', { name: /related/i })).toBeInTheDocument()
  })

  it('lists up to 5 out-edges as wikilinks', async () => {
    const edges: api.GraphEdge[] = [
      {
        from_kind: 'people',
        from_slug: 'sarah',
        to_kind: 'companies',
        to_slug: 'acme',
        first_seen_fact_id: 'f1',
        last_seen_ts: '2026-04-20T00:00:00Z',
        occurrence_count: 2,
      },
      {
        from_kind: 'people',
        from_slug: 'sarah',
        to_kind: 'customers',
        to_slug: 'globex',
        first_seen_fact_id: 'f2',
        last_seen_ts: '2026-04-19T00:00:00Z',
        occurrence_count: 1,
      },
    ]
    vi.spyOn(api, 'fetchEntityGraph').mockResolvedValue(edges)
    render(<EntityRelatedPanel kind="people" slug="sarah" />)
    await screen.findByText('companies/acme')
    expect(screen.getByText('customers/globex')).toBeInTheDocument()
    // Occurrence count only shown when >1.
    expect(screen.getByText('×2')).toBeInTheDocument()
    expect(screen.queryByText('×1')).toBeNull()
    // Render as a data-wikilink anchor so the wiki router can intercept.
    const link = screen.getByText('companies/acme').closest('a')
    expect(link).toHaveAttribute('data-wikilink', 'true')
    expect(link?.getAttribute('href')).toContain('companies/acme.md')
  })

  it('caps the list at 5 entries', async () => {
    const edges: api.GraphEdge[] = Array.from({ length: 8 }, (_v, i) => ({
      from_kind: 'people' as const,
      from_slug: 'sarah',
      to_kind: 'companies' as const,
      to_slug: `target-${i}`,
      first_seen_fact_id: `f${i}`,
      last_seen_ts: `2026-04-2${i}T00:00:00Z`,
      occurrence_count: 1,
    }))
    vi.spyOn(api, 'fetchEntityGraph').mockResolvedValue(edges)
    render(<EntityRelatedPanel kind="people" slug="sarah" />)
    await screen.findByText('companies/target-0')
    expect(screen.getByText('companies/target-4')).toBeInTheDocument()
    expect(screen.queryByText('companies/target-5')).toBeNull()
  })

  it('surfaces a load error', async () => {
    vi.spyOn(api, 'fetchEntityGraph').mockRejectedValue(new Error('boom'))
    render(<EntityRelatedPanel kind="people" slug="sarah" />)
    await waitFor(() => expect(screen.getByText('boom')).toBeInTheDocument())
  })
})
