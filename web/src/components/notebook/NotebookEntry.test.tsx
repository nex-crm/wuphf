import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import NotebookEntryView from './NotebookEntry'
import * as api from '../../api/notebook'
import type { NotebookEntry } from '../../api/notebook'

const DRAFT_ENTRY: NotebookEntry = {
  agent_slug: 'pm',
  entry_slug: 'customer-acme-rough-notes',
  title: 'Customer Acme — rough notes',
  subtitle: 'Thursday, April 20th · working draft',
  body_md: '## First\n\nBody content here.',
  last_edited_ts: new Date().toISOString(),
  revisions: 3,
  status: 'draft',
  file_path: '~/.wuphf/wiki/agents/pm/notebook/2026-04-20.md',
  reviewer_slug: 'ceo',
}

const PROMOTED_ENTRY: NotebookEntry = {
  ...DRAFT_ENTRY,
  status: 'promoted',
  promoted_to_path: 'playbooks/customer-onboarding',
}

describe('<NotebookEntryView>', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('renders title, subtitle, and DRAFT stamp for a draft entry', () => {
    render(<NotebookEntryView entry={DRAFT_ENTRY} />)
    expect(screen.getByRole('heading', { name: 'Customer Acme — rough notes' })).toBeInTheDocument()
    expect(screen.getByText('Thursday, April 20th · working draft')).toBeInTheDocument()
    expect(screen.getByRole('img', { name: 'Draft entry, not yet reviewed' })).toBeInTheDocument()
  })

  it('does NOT render the DRAFT stamp for a promoted entry', () => {
    render(<NotebookEntryView entry={PROMOTED_ENTRY} />)
    expect(screen.queryByRole('img', { name: /Draft entry/ })).not.toBeInTheDocument()
  })

  it('carries the draft aria-label on main', () => {
    render(<NotebookEntryView entry={DRAFT_ENTRY} />)
    expect(
      screen.getByLabelText('Draft: Customer Acme — rough notes. Not yet reviewed.'),
    ).toBeInTheDocument()
  })

  it('renders markdown body from the entry', () => {
    render(<NotebookEntryView entry={DRAFT_ENTRY} />)
    expect(screen.getByRole('heading', { name: 'First', level: 2 })).toBeInTheDocument()
    expect(screen.getByText('Body content here.')).toBeInTheDocument()
  })

  it('renders promoted-back callout when the entry has a back-link', () => {
    const withBack: NotebookEntry = {
      ...DRAFT_ENTRY,
      promoted_back: {
        section: 'onboarding gotchas',
        promoted_to_path: 'playbooks/customer-onboarding',
        promoted_by_slug: 'ceo',
        promoted_ts: new Date().toISOString(),
      },
    }
    render(<NotebookEntryView entry={withBack} />)
    expect(screen.getByText('onboarding gotchas')).toBeInTheDocument()
  })

  it('transitions to pending-pill state after Promote click', async () => {
    const promoteSpy = vi.spyOn(api, 'promoteEntry').mockResolvedValue({
      id: 'mock',
      agent_slug: DRAFT_ENTRY.agent_slug,
      entry_slug: DRAFT_ENTRY.entry_slug,
      entry_title: DRAFT_ENTRY.title,
      proposed_wiki_path: 'drafts/pm-customer-acme-rough-notes',
      excerpt: '',
      reviewer_slug: 'ceo',
      state: 'pending',
      submitted_ts: new Date().toISOString(),
      updated_ts: new Date().toISOString(),
      comments: [],
    })
    render(<NotebookEntryView entry={DRAFT_ENTRY} />)
    await userEvent.setup().click(screen.getByRole('button', { name: /Submit this draft for review by CEO/ }))
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Pending review by CEO/ })).toBeDisabled()
    })
    expect(promoteSpy).toHaveBeenCalledWith('pm', 'customer-acme-rough-notes', expect.any(Object))
  })
})
