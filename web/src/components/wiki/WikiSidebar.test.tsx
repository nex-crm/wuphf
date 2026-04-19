import { describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import WikiSidebar from './WikiSidebar'
import type { WikiCatalogEntry } from '../../api/wiki'

const CATALOG: WikiCatalogEntry[] = [
  { path: 'people/nazz', title: 'Nazz', author_slug: 'pm', last_edited_ts: new Date().toISOString(), group: 'people' },
  { path: 'people/sarah', title: 'Sarah', author_slug: 'ceo', last_edited_ts: new Date().toISOString(), group: 'people' },
  { path: 'playbooks/churn', title: 'Churn prevention', author_slug: 'cmo', last_edited_ts: new Date().toISOString(), group: 'playbooks' },
]

describe('<WikiSidebar>', () => {
  it('renders grouped articles with a Tools section', () => {
    render(<WikiSidebar catalog={CATALOG} onNavigate={() => {}} />)
    expect(screen.getByText('people')).toBeInTheDocument()
    expect(screen.getByText('playbooks')).toBeInTheDocument()
    expect(screen.getByText('Nazz')).toBeInTheDocument()
    expect(screen.getByText('Tools')).toBeInTheDocument()
  })

  it('marks the current article', () => {
    render(<WikiSidebar catalog={CATALOG} currentPath="people/nazz" onNavigate={() => {}} />)
    const li = screen.getByText('Nazz').closest('li')
    expect(li).toHaveClass('current')
  })

  it('calls onNavigate when an article link is clicked', () => {
    const onNavigate = vi.fn()
    render(<WikiSidebar catalog={CATALOG} onNavigate={onNavigate} />)
    fireEvent.click(screen.getByText('Churn prevention'))
    expect(onNavigate).toHaveBeenCalledWith('playbooks/churn')
  })

  it('filters articles by the search query', () => {
    render(<WikiSidebar catalog={CATALOG} onNavigate={() => {}} />)
    const search = screen.getByPlaceholderText('Search wiki…')
    fireEvent.change(search, { target: { value: 'churn' } })
    expect(screen.getByText('Churn prevention')).toBeInTheDocument()
    expect(screen.queryByText('Nazz')).not.toBeInTheDocument()
  })
})
