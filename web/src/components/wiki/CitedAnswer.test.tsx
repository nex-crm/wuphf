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

  it('shows loading skeleton while fetching', () => {
    vi.spyOn(apiClient, 'get').mockReturnValue(new Promise(() => {}))
    render(<CitedAnswer query="Who is Sarah Jones?" />)
    const skeleton = document.querySelector('.wk-cited-answer--loading')
    expect(skeleton).toBeTruthy()
    expect(skeleton?.getAttribute('aria-busy')).toBe('true')
    expect(skeleton?.getAttribute('role')).toBe('status')
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
    // Sources section has the cited excerpt
    expect(screen.getByText(/Sarah Jones is VP/)).toBeTruthy()
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
