import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import AuthorShelfSidebar from './AuthorShelfSidebar'
import type { NotebookAgentSummary, NotebookEntrySummary } from '../../api/notebook'

const AGENT: NotebookAgentSummary = {
  agent_slug: 'pm',
  name: 'PM',
  role: 'Product Manager · agent',
  entries: [],
  total: 2,
  promoted_count: 1,
  last_updated_ts: new Date().toISOString(),
}

const ENTRIES: NotebookEntrySummary[] = [
  {
    entry_slug: 'customer-acme-rough-notes',
    title: 'Customer Acme — rough notes',
    last_edited_ts: new Date().toISOString(),
    status: 'draft',
  },
  {
    entry_slug: 'onboarding-gotchas-checklist',
    title: 'Onboarding gotchas checklist',
    last_edited_ts: new Date(Date.now() - 60 * 60 * 26 * 1000).toISOString(),
    status: 'promoted',
  },
]

describe('<AuthorShelfSidebar>', () => {
  it('renders author label and role', () => {
    render(
      <AuthorShelfSidebar
        agent={AGENT}
        entries={ENTRIES}
        currentEntrySlug={null}
        onSelect={() => {}}
      />,
    )
    expect(screen.getByRole('heading', { name: "PM's notebook" })).toBeInTheDocument()
    expect(screen.getByText('Product Manager · agent')).toBeInTheDocument()
  })

  it('lists entries grouped by day with their title', () => {
    render(
      <AuthorShelfSidebar
        agent={AGENT}
        entries={ENTRIES}
        currentEntrySlug={null}
        onSelect={() => {}}
      />,
    )
    expect(screen.getByText('Customer Acme — rough notes')).toBeInTheDocument()
    expect(screen.getByText('Onboarding gotchas checklist')).toBeInTheDocument()
  })

  it('highlights the current entry with aria-current', () => {
    render(
      <AuthorShelfSidebar
        agent={AGENT}
        entries={ENTRIES}
        currentEntrySlug="customer-acme-rough-notes"
        onSelect={() => {}}
      />,
    )
    const current = screen.getByText('Customer Acme — rough notes').closest('button')
    expect(current).toHaveAttribute('aria-current', 'page')
  })

  it('invokes onSelect when an entry is clicked', async () => {
    const onSelect = vi.fn()
    render(
      <AuthorShelfSidebar
        agent={AGENT}
        entries={ENTRIES}
        currentEntrySlug={null}
        onSelect={onSelect}
      />,
    )
    await userEvent.setup().click(screen.getByText('Customer Acme — rough notes'))
    expect(onSelect).toHaveBeenCalledWith('customer-acme-rough-notes')
  })

  it('shows an empty state when no entries exist', () => {
    render(
      <AuthorShelfSidebar
        agent={{ ...AGENT, entries: [], total: 0 }}
        entries={[]}
        currentEntrySlug={null}
        onSelect={() => {}}
      />,
    )
    expect(screen.getByText('No entries yet.')).toBeInTheDocument()
  })
})
