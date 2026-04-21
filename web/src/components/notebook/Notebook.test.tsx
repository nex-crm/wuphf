import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import Notebook from './Notebook'
import * as api from '../../api/notebook'
import type { NotebookAgentSummary, NotebookCatalogSummary } from '../../api/notebook'

const CATALOG: NotebookCatalogSummary = {
  total_agents: 1,
  total_entries: 1,
  pending_promotion: 0,
  agents: [
    {
      agent_slug: 'pm',
      name: 'PM',
      role: 'Product Manager · agent',
      entries: [
        { entry_slug: 'e1', title: 'Entry one', last_edited_ts: new Date().toISOString(), status: 'draft' },
      ],
      total: 1,
      promoted_count: 0,
      last_updated_ts: new Date().toISOString(),
    },
  ],
}

const AGENT_SUMMARY: NotebookAgentSummary = CATALOG.agents[0]

describe('<Notebook>', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.spyOn(api, 'subscribeNotebookEvents').mockImplementation(() => () => {})
  })

  it('renders the bookshelf when no agent is selected', async () => {
    vi.spyOn(api, 'fetchCatalog').mockResolvedValue(CATALOG)
    render(
      <Notebook
        agentSlug={null}
        entrySlug={null}
        onOpenCatalog={() => {}}
        onOpenAgent={() => {}}
        onOpenEntry={() => {}}
        onNavigateWiki={() => {}}
      />,
    )
    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Team notebooks' })).toBeInTheDocument(),
    )
    expect(screen.getByTestId('notebook-surface')).toBeInTheDocument()
  })

  it('renders the agent view when agentSlug is set', async () => {
    vi.spyOn(api, 'fetchAgentEntries').mockResolvedValue({
      agent: AGENT_SUMMARY,
      entries: [],
    })
    render(
      <Notebook
        agentSlug="pm"
        entrySlug={null}
        onOpenCatalog={() => {}}
        onOpenAgent={() => {}}
        onOpenEntry={() => {}}
        onNavigateWiki={() => {}}
      />,
    )
    await waitFor(() =>
      expect(screen.getByRole('heading', { name: "PM's notebook" })).toBeInTheDocument(),
    )
  })

  it('renders an error state + Retry when catalog fetch fails', async () => {
    vi.spyOn(api, 'fetchCatalog').mockRejectedValue(new Error('broker down'))
    render(
      <Notebook
        agentSlug={null}
        entrySlug={null}
        onOpenCatalog={() => {}}
        onOpenAgent={() => {}}
        onOpenEntry={() => {}}
        onNavigateWiki={() => {}}
      />,
    )
    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument())
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument()
  })

  it('subscribes to notebook events on mount and unsubscribes on unmount', () => {
    const unsub = vi.fn()
    const spy = vi.spyOn(api, 'subscribeNotebookEvents').mockImplementation(() => unsub)
    vi.spyOn(api, 'fetchCatalog').mockResolvedValue(CATALOG)
    const { unmount } = render(
      <Notebook
        agentSlug={null}
        entrySlug={null}
        onOpenCatalog={() => {}}
        onOpenAgent={() => {}}
        onOpenEntry={() => {}}
        onNavigateWiki={() => {}}
      />,
    )
    expect(spy).toHaveBeenCalled()
    unmount()
    expect(unsub).toHaveBeenCalled()
  })
})
