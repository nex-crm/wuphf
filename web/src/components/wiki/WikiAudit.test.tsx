import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import WikiAudit from './WikiAudit'
import * as api from '../../api/wiki'

const ENTRIES: api.WikiAuditEntry[] = [
  {
    sha: 'aaa1111',
    author_slug: 'operator',
    timestamp: new Date(Date.now() - 60 * 1000).toISOString(),
    message: 'first meridian brief',
    paths: ['team/customers/meridian-freight.md'],
  },
  {
    sha: 'bbb2222',
    author_slug: 'wuphf-bootstrap',
    timestamp: new Date(Date.now() - 3600 * 1000).toISOString(),
    message: 'materialize niche-crm skeletons',
    paths: ['team/playbooks/renewal.md', 'team/decisions/product-log.md'],
  },
  {
    sha: 'ccc3333',
    author_slug: 'system',
    timestamp: new Date(Date.now() - 7200 * 1000).toISOString(),
    message: 'wuphf: init wiki',
    paths: [],
  },
]

describe('<WikiAudit>', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.spyOn(api, 'fetchAuditLog').mockResolvedValue({ entries: ENTRIES, total: ENTRIES.length })
  })

  it('renders every entry with author, message, and SHA', async () => {
    render(<WikiAudit onNavigate={() => {}} />)
    await waitFor(() => expect(screen.getByText('first meridian brief')).toBeInTheDocument())
    expect(screen.getByText('materialize niche-crm skeletons')).toBeInTheDocument()
    expect(screen.getByText('wuphf: init wiki')).toBeInTheDocument()
    expect(screen.getByText('aaa1111')).toBeInTheDocument()
    expect(screen.getByText('bbb2222')).toBeInTheDocument()
    expect(screen.getByText('ccc3333')).toBeInTheDocument()
  })

  it('labels system + bootstrap commits with distinguishing tags', async () => {
    render(<WikiAudit onNavigate={() => {}} />)
    await waitFor(() => expect(screen.getByText('first meridian brief')).toBeInTheDocument())
    expect(screen.getByText('bootstrap')).toBeInTheDocument()
    expect(screen.getByText('system')).toBeInTheDocument()
  })

  it('filters to agents only via the author dropdown', async () => {
    render(<WikiAudit onNavigate={() => {}} />)
    await waitFor(() => expect(screen.getByText('first meridian brief')).toBeInTheDocument())
    const select = screen.getByLabelText('Author') as HTMLSelectElement
    fireEvent.change(select, { target: { value: 'agents' } })
    expect(screen.getByText('first meridian brief')).toBeInTheDocument()
    expect(screen.queryByText('materialize niche-crm skeletons')).not.toBeInTheDocument()
    expect(screen.queryByText('wuphf: init wiki')).not.toBeInTheDocument()
  })

  it('filters to system only', async () => {
    render(<WikiAudit onNavigate={() => {}} />)
    await waitFor(() => expect(screen.getByText('first meridian brief')).toBeInTheDocument())
    const select = screen.getByLabelText('Author') as HTMLSelectElement
    fireEvent.change(select, { target: { value: 'system' } })
    expect(screen.queryByText('first meridian brief')).not.toBeInTheDocument()
    expect(screen.getByText('materialize niche-crm skeletons')).toBeInTheDocument()
    expect(screen.getByText('wuphf: init wiki')).toBeInTheDocument()
  })

  it('search matches against message and path', async () => {
    render(<WikiAudit onNavigate={() => {}} />)
    await waitFor(() => expect(screen.getByText('first meridian brief')).toBeInTheDocument())
    const search = screen.getByLabelText('Search') as HTMLInputElement
    fireEvent.change(search, { target: { value: 'renewal' } })
    // Only the bootstrap commit touches team/playbooks/renewal.md
    expect(screen.queryByText('first meridian brief')).not.toBeInTheDocument()
    expect(screen.getByText('materialize niche-crm skeletons')).toBeInTheDocument()
  })

  it('path links navigate to the article view', async () => {
    const onNavigate = vi.fn()
    render(<WikiAudit onNavigate={onNavigate} />)
    await waitFor(() =>
      expect(screen.getByText('team/customers/meridian-freight.md')).toBeInTheDocument(),
    )
    fireEvent.click(screen.getByText('team/customers/meridian-freight.md'))
    expect(onNavigate).toHaveBeenCalledWith('team/customers/meridian-freight.md')
  })

  it('renders helpful empty-state when the audit log is truly empty', async () => {
    vi.spyOn(api, 'fetchAuditLog').mockResolvedValue({ entries: [], total: 0 })
    render(<WikiAudit onNavigate={() => {}} />)
    await waitFor(() => expect(screen.getByText(/No edits yet/i)).toBeInTheDocument())
  })
})
