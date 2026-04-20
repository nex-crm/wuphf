import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import * as api from './wiki'
import * as client from './client'

describe('wiki api client', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('fetchArticle returns the server response when the endpoint succeeds', async () => {
    const article: api.WikiArticle = {
      path: 'people/nazz',
      title: 'Nazz',
      content: 'Hi',
      last_edited_by: 'pm',
      last_edited_ts: new Date().toISOString(),
      revisions: 1,
      contributors: ['pm'],
      backlinks: [],
      word_count: 1,
      categories: [],
    }
    vi.spyOn(client, 'get').mockResolvedValue(article)
    const result = await api.fetchArticle('people/nazz')
    expect(result).toEqual(article)
  })

  it('fetchArticle falls back to a mock on network error', async () => {
    vi.spyOn(client, 'get').mockRejectedValue(new Error('boom'))
    const result = await api.fetchArticle('people/customer-x')
    expect(result.title).toBe('Customer X')
  })

  it('fetchCatalog returns entries array on success', async () => {
    const entries: api.WikiCatalogEntry[] = [
      { path: 'a', title: 'A', author_slug: 'pm', last_edited_ts: new Date().toISOString(), group: 'people' },
    ]
    vi.spyOn(client, 'get').mockResolvedValue({ articles: entries })
    const result = await api.fetchCatalog()
    expect(result).toEqual(entries)
  })

  it('fetchCatalog falls back to MOCK_CATALOG on error', async () => {
    vi.spyOn(client, 'get').mockRejectedValue(new Error('boom'))
    const result = await api.fetchCatalog()
    expect(result.length).toBeGreaterThan(0)
  })

  it('fetchHistory returns mock commits on error', async () => {
    vi.spyOn(client, 'get').mockRejectedValue(new Error('boom'))
    const result = await api.fetchHistory('people/customer-x')
    expect(result.commits.length).toBeGreaterThan(0)
  })

  it('mockArticle generates a fallback article for unknown paths', () => {
    const result = api.mockArticle('unknown/thing')
    expect(result.path).toBe('unknown/thing')
    expect(result.title).toMatch(/Thing/i)
  })

  it('fetchCatalog treats a non-array response as empty', async () => {
    vi.spyOn(client, 'get').mockResolvedValue({ articles: null })
    const result = await api.fetchCatalog()
    expect(result).toEqual([])
  })

  it('fetchHistory returns real commits when the endpoint succeeds', async () => {
    const commits = [{ sha: 'abc', author_slug: 'pm', msg: 'edit', date: '2026-01-14' }]
    vi.spyOn(client, 'get').mockResolvedValue({ commits })
    const result = await api.fetchHistory('a')
    expect(result.commits).toEqual(commits)
  })

  it('mockArticle generates the Customer X fixture for the canonical path', () => {
    const result = api.mockArticle('customer-x')
    expect(result.title).toBe('Customer X')
    expect(result.contributors.length).toBeGreaterThan(0)
  })

  it('subscribeEditLog returns an unsubscribe function even when SSE is unavailable', () => {
    // No EventSource in happy-dom by default — the client should not throw.
    const originalEventSource = (globalThis as { EventSource?: unknown }).EventSource
    ;(globalThis as { EventSource?: unknown }).EventSource = undefined
    try {
      const unsub = api.subscribeEditLog(() => {})
      expect(typeof unsub).toBe('function')
      unsub()
    } finally {
      ;(globalThis as { EventSource?: unknown }).EventSource = originalEventSource
    }
  })
})
