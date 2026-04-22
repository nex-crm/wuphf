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

  it('fetchArticle resolves a bare slug by trying the standard group dirs', async () => {
    const article: api.WikiArticle = {
      path: 'team/companies/stripe.md',
      title: 'Stripe',
      content: 'Payments infra',
      last_edited_by: 'archivist',
      last_edited_ts: new Date().toISOString(),
      revisions: 1,
      contributors: ['archivist'],
      backlinks: [],
      word_count: 2,
      categories: [],
    }
    const spy = vi.spyOn(client, 'get')
      // First candidate (team/people/stripe.md) misses.
      .mockRejectedValueOnce(new Error('404'))
      // Second candidate (team/companies/stripe.md) hits.
      .mockResolvedValueOnce(article)
    const result = await api.fetchArticle('stripe')
    expect(result).toEqual(article)
    expect(spy).toHaveBeenNthCalledWith(
      1,
      expect.stringContaining(encodeURIComponent('team/people/stripe.md')),
    )
    expect(spy).toHaveBeenNthCalledWith(
      2,
      expect.stringContaining(encodeURIComponent('team/companies/stripe.md')),
    )
  })

  it('fetchArticle passes through a full team/ path without fanning out', async () => {
    const article: api.WikiArticle = {
      path: 'team/playbooks/pricing.md',
      title: 'Pricing',
      content: '',
      last_edited_by: 'cro',
      last_edited_ts: new Date().toISOString(),
      revisions: 1,
      contributors: ['cro'],
      backlinks: [],
      word_count: 0,
      categories: [],
    }
    const spy = vi.spyOn(client, 'get').mockResolvedValue(article)
    const result = await api.fetchArticle('team/playbooks/pricing.md')
    expect(result).toEqual(article)
    expect(spy).toHaveBeenCalledTimes(1)
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

  it('fetchSections returns the server response when the endpoint succeeds', async () => {
    const sections: api.DiscoveredSection[] = [
      {
        slug: 'people',
        title: 'People',
        article_paths: ['team/people/a.md'],
        article_count: 1,
        first_seen_ts: new Date().toISOString(),
        last_update_ts: new Date().toISOString(),
        from_schema: true,
      },
    ]
    vi.spyOn(client, 'get').mockResolvedValue({ sections })
    const result = await api.fetchSections()
    expect(result).toEqual(sections)
  })

  it('fetchSections returns an empty array on network error', async () => {
    vi.spyOn(client, 'get').mockRejectedValue(new Error('boom'))
    const result = await api.fetchSections()
    expect(result).toEqual([])
  })

  it('fetchSections tolerates a null payload', async () => {
    vi.spyOn(client, 'get').mockResolvedValue({ sections: null })
    const result = await api.fetchSections()
    expect(result).toEqual([])
  })

  it('subscribeSectionsUpdated returns an unsubscribe function even when SSE is unavailable', () => {
    const originalEventSource = (globalThis as { EventSource?: unknown }).EventSource
    ;(globalThis as { EventSource?: unknown }).EventSource = undefined
    try {
      const unsub = api.subscribeSectionsUpdated(() => {})
      expect(typeof unsub).toBe('function')
      unsub()
    } finally {
      ;(globalThis as { EventSource?: unknown }).EventSource = originalEventSource
    }
  })

  it('writeHumanArticle posts the expected payload and returns the ok envelope', async () => {
    const spy = vi
      .spyOn(client, 'post')
      .mockResolvedValue({ path: 'team/people/x.md', commit_sha: 'abc1234', bytes_written: 10 })

    const result = await api.writeHumanArticle({
      path: 'team/people/x.md',
      content: 'body',
      commitMessage: 'human: fix typo',
      expectedSha: 'deadbee',
    })

    expect(spy).toHaveBeenCalledWith('/wiki/write-human', {
      path: 'team/people/x.md',
      content: 'body',
      commit_message: 'human: fix typo',
      expected_sha: 'deadbee',
    })
    expect(result).toEqual({ path: 'team/people/x.md', commit_sha: 'abc1234', bytes_written: 10 })
  })

  it('writeHumanArticle parses a 409 body into a WriteHumanConflict', async () => {
    const conflictBody = JSON.stringify({
      error: 'wiki: article changed since it was opened',
      current_sha: 'newsha9',
      current_content: '# new content',
    })
    // The shared post() helper rethrows as Error(text). Simulate that.
    vi.spyOn(client, 'post').mockRejectedValue(new Error(conflictBody))

    const result = await api.writeHumanArticle({
      path: 'team/people/x.md',
      content: 'my edit',
      commitMessage: 'human: stale',
      expectedSha: 'oldsha1',
    })

    expect('conflict' in result && result.conflict).toBe(true)
    if ('conflict' in result) {
      expect(result.current_sha).toBe('newsha9')
      expect(result.current_content).toBe('# new content')
    }
  })

  it('writeHumanArticle rethrows unrecognized errors', async () => {
    vi.spyOn(client, 'post').mockRejectedValue(new Error('500 Internal Server Error'))
    await expect(
      api.writeHumanArticle({
        path: 'team/people/x.md',
        content: 'x',
        commitMessage: 'human: nope',
        expectedSha: 'abc',
      }),
    ).rejects.toThrow(/500/)
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
