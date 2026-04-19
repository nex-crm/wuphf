import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import WikiArticle from './WikiArticle'
import * as api from '../../api/wiki'

const CATALOG: api.WikiCatalogEntry[] = [
  { path: 'people/sarah-chen', title: 'Sarah Chen', author_slug: 'ceo', last_edited_ts: new Date().toISOString(), group: 'people' },
]

describe('<WikiArticle>', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('fetches an article, renders its markdown, and distinguishes broken wikilinks', async () => {
    // Arrange
    vi.spyOn(api, 'fetchArticle').mockResolvedValue({
      path: 'people/customer-x',
      title: 'Customer X',
      content:
        '**Customer X** is a mid-market logistics company. See [[people/sarah-chen|Sarah Chen]] and [[missing|Missing page]].',
      last_edited_by: 'ceo',
      last_edited_ts: new Date().toISOString(),
      revisions: 47,
      contributors: ['ceo', 'pm'],
      backlinks: [],
      word_count: 100,
      categories: ['Active pilot'],
    })

    // Act
    render(
      <WikiArticle
        path="people/customer-x"
        catalog={CATALOG}
        onNavigate={() => {}}
      />,
    )

    // Assert
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Customer X' })).toBeInTheDocument())
    expect(screen.getByText(/mid-market logistics company/i)).toBeInTheDocument()

    const okLink = await screen.findByText('Sarah Chen')
    expect(okLink.closest('a')).toHaveAttribute('data-wikilink', 'true')
    expect(okLink.closest('a')).toHaveAttribute('data-broken', 'false')

    const brokenLink = await screen.findByText('Missing page')
    expect(brokenLink.closest('a')).toHaveAttribute('data-broken', 'true')
  })

  it('switches to raw markdown tab and shows the source', async () => {
    vi.spyOn(api, 'fetchArticle').mockResolvedValue({
      path: 'a/b',
      title: 'A',
      content: '## Heading A\n\nBody.\n\n### Sub\n\nMore.',
      last_edited_by: 'pm',
      last_edited_ts: new Date().toISOString(),
      revisions: 1,
      contributors: ['pm'],
      backlinks: [],
      word_count: 5,
      categories: [],
    })
    const { getByRole, findByText, getByText } = render(
      <WikiArticle path="a/b" catalog={[]} onNavigate={() => {}} />,
    )
    await findByText(/Body\./)
    getByRole('button', { name: 'Raw markdown' }).click()
    await waitFor(() => expect(getByText(/## Heading A/)).toBeInTheDocument())
    getByRole('button', { name: 'History' }).click()
    await waitFor(() => expect(getByText(/streams from/)).toBeInTheDocument())
  })

  it('renders an error state when fetchArticle rejects', async () => {
    vi.spyOn(api, 'fetchArticle').mockRejectedValue(new Error('network down'))
    render(<WikiArticle path="broken" catalog={[]} onNavigate={() => {}} />)
    await waitFor(() => expect(screen.getByText(/network down/)).toBeInTheDocument())
  })

  it('shows a loading state before the fetch resolves', async () => {
    // Arrange
    type Resolve = (v: api.WikiArticle) => void
    let resolveFn: Resolve | null = null
    vi.spyOn(api, 'fetchArticle').mockImplementation(
      () => new Promise<api.WikiArticle>((r) => { resolveFn = r as Resolve }),
    )
    // Act
    render(<WikiArticle path="a" catalog={[]} onNavigate={() => {}} />)
    expect(screen.getByText(/Loading article/i)).toBeInTheDocument()
    // Finalize
    const finish = resolveFn as Resolve | null
    finish?.({
      path: 'a', title: 'A', content: 'body', last_edited_by: 'pm',
      last_edited_ts: new Date().toISOString(), revisions: 1, contributors: ['pm'],
      backlinks: [], word_count: 1, categories: [],
    })
    await waitFor(() => expect(screen.queryByText(/Loading article/i)).not.toBeInTheDocument())
  })
})
