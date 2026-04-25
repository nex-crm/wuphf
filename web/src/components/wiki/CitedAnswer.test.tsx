import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import CitedAnswer, { type QueryAnswer } from './CitedAnswer'
import * as apiClient from '../../api/client'

const STUB_ANSWER: QueryAnswer = {
  query_class: 'status',
  answer_markdown: 'Sarah Jones is VP of Sales at Acme Corp.<sup>[1]</sup>',
  sources_cited: [1],
  sources: [
    {
      kind: 'fact',
      slug_or_id: 'people/sarah-jones',
      title: 'Sarah Jones',
      excerpt: 'Sarah Jones is VP of Sales at Acme Corp.',
      valid_from: '2026-01-01',
      staleness: 0.1,
      source_path: 'team/people/sarah-jones.md',
    },
  ],
  confidence: 0.9,
  coverage: 'complete',
  latency_ms: 42,
}

const STUB_GENERAL: QueryAnswer = {
  query_class: 'general',
  answer_markdown: "I don't have information about that.",
  sources_cited: [],
  sources: [],
  confidence: 0.85,
  coverage: 'none',
  latency_ms: 5,
}

const STUB_PARTIAL: QueryAnswer = {
  ...STUB_ANSWER,
  coverage: 'partial',
  sources_cited: [],
}

describe('<CitedAnswer>', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('shows "Searching the wiki…" status line while fetching', () => {
    vi.spyOn(apiClient, 'get').mockReturnValue(new Promise(() => {}))
    render(<CitedAnswer query="Who is Sarah Jones?" />)
    const loadingEl = document.querySelector('.wk-cited-answer--loading')
    expect(loadingEl).toBeTruthy()
    expect(loadingEl?.getAttribute('aria-busy')).toBe('true')
    expect(loadingEl?.getAttribute('role')).toBe('status')
    // Fraunces-italic status line — no amber, no dark-mode, no skeleton blocks.
    const status = loadingEl?.querySelector('.wk-lookup-status')
    expect(status).toBeTruthy()
    expect(status?.textContent).toBe('Searching the wiki…')
  })

  it('renders answer_markdown and hatnote on success', async () => {
    vi.spyOn(apiClient, 'get').mockResolvedValue(STUB_ANSWER)
    render(<CitedAnswer query="Who is Sarah Jones?" />)
    await waitFor(() => {
      expect(screen.getByTestId('wk-cited-answer-body')).toBeTruthy()
    })
    // Hatnote present
    expect(document.querySelector('.wk-hatnote')).toBeTruthy()
    // Body contains the answer text
    expect(screen.getByTestId('wk-cited-answer-body').textContent).toContain('Sarah Jones')
  })

  it('renders cited sources list on successful answer', async () => {
    vi.spyOn(apiClient, 'get').mockResolvedValue(STUB_ANSWER)
    render(<CitedAnswer query="Who is Sarah Jones?" />)
    await waitFor(() => {
      expect(screen.getByText(/Sources/i)).toBeTruthy()
    })
    // Sources section has the cited excerpt — scope to the <ol> so we don't
    // match the body text that happens to share prose with the excerpt.
    const sourcesList = document.querySelector('.wk-sources ol')
    expect(sourcesList).toBeTruthy()
    expect(sourcesList!.textContent).toContain('Sarah Jones is VP')
  })

  it('preserves citation numbering when some sources are uncited (gap case)', async () => {
    const gapped: QueryAnswer = {
      ...STUB_ANSWER,
      answer_markdown: 'First fact.<sup>[1]</sup> Third fact.<sup>[3]</sup>',
      sources_cited: [1, 3],
      sources: [
        { ...STUB_ANSWER.sources[0], slug_or_id: 'a', excerpt: 'First source excerpt.' },
        { ...STUB_ANSWER.sources[0], slug_or_id: 'b', excerpt: 'Second source uncited.' },
        { ...STUB_ANSWER.sources[0], slug_or_id: 'c', excerpt: 'Third source excerpt.' },
      ],
    }
    vi.spyOn(apiClient, 'get').mockResolvedValue(gapped)
    render(<CitedAnswer query="gap?" />)
    await waitFor(() => {
      expect(document.querySelector('#ca-sources-heading')).toBeTruthy()
    })
    const items = document.querySelectorAll('.wk-sources ol > li')
    expect(items.length).toBe(2)
    expect(items[0].getAttribute('value')).toBe('1')
    expect(items[0].getAttribute('id')).toBe('ca-s1')
    expect(items[1].getAttribute('value')).toBe('3')
    expect(items[1].getAttribute('id')).toBe('ca-s3')
  })

  it('shows no Sources block for out-of-scope queries', async () => {
    vi.spyOn(apiClient, 'get').mockResolvedValue(STUB_GENERAL)
    render(<CitedAnswer query="What is the weather like?" />)
    await waitFor(() => {
      // Out-of-scope guidance text appears
      expect(screen.getByText(/help with questions/)).toBeTruthy()
    })
    // No sources section
    expect(document.querySelector('#ca-sources-heading')).toBeNull()
  })

  it('shows hatnote-styled error when the API fails', async () => {
    vi.spyOn(apiClient, 'get').mockRejectedValue(new Error('wiki backend is not active'))
    render(<CitedAnswer query="Who is Sarah Jones?" />)
    await waitFor(() => {
      expect(document.querySelector('.wk-cited-answer--error')).toBeTruthy()
    })
    expect(screen.getByText(/wiki backend is not active/)).toBeTruthy()
  })
})
