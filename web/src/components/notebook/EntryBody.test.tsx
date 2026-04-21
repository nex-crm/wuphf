import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import EntryBody from './EntryBody'

describe('<EntryBody>', () => {
  it('renders plain markdown with headings and paragraphs', () => {
    render(<EntryBody markdown={'## First section\n\nBody paragraph.'} />)
    expect(screen.getByRole('heading', { name: 'First section', level: 2 })).toBeInTheDocument()
    expect(screen.getByText('Body paragraph.')).toBeInTheDocument()
  })

  it('renders GFM task list items', () => {
    render(<EntryBody markdown={'- [x] Done item\n- [ ] Open item'} />)
    expect(screen.getByText('Done item')).toBeInTheDocument()
    expect(screen.getByText('Open item')).toBeInTheDocument()
  })

  it('renders blockquote as a marginalia-styled callout', () => {
    const { container } = render(<EntryBody markdown={'> Q: what about elasticity?'} />)
    const margin = container.querySelector('.nb-margin')
    expect(margin).not.toBeNull()
    expect(margin?.textContent).toContain('what about elasticity')
  })

  it('intercepts wikilink clicks via onWikiNavigate', async () => {
    const onWikiNavigate = vi.fn()
    render(
      <EntryBody
        markdown={'See [[people/sarah-chen|Sarah]] for details.'}
        onWikiNavigate={onWikiNavigate}
      />,
    )
    const link = screen.getByText('Sarah')
    await userEvent.setup().click(link)
    expect(onWikiNavigate).toHaveBeenCalledWith('people/sarah-chen')
  })

  it('flags broken wikilinks when the resolver returns false', () => {
    const { container } = render(
      <EntryBody
        markdown={'See [[people/missing|missing link]].'}
        wikiExists={() => false}
      />,
    )
    const broken = container.querySelector('.nb-wikilink.is-broken')
    expect(broken).not.toBeNull()
  })
})
