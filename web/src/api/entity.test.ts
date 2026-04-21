import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import * as api from './entity'
import * as client from './client'

describe('entity api client', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('fetchFacts unwraps the {facts:[...]} envelope', async () => {
    // Arrange
    const facts: api.Fact[] = [
      {
        id: 'f1',
        kind: 'people',
        slug: 'sarah-chen',
        text: 'Joined March 2024',
        recorded_by: 'pm',
        created_at: '2026-04-14T00:00:00Z',
      },
    ]
    const getSpy = vi.spyOn(client, 'get').mockResolvedValue({ facts })
    // Act
    const result = await api.fetchFacts('people', 'sarah-chen')
    // Assert
    expect(getSpy).toHaveBeenCalledWith('/entity/facts?kind=people&slug=sarah-chen')
    expect(result).toEqual(facts)
  })

  it('fetchFacts tolerates a missing facts key', async () => {
    vi.spyOn(client, 'get').mockResolvedValue({})
    const result = await api.fetchFacts('companies', 'acme')
    expect(result).toEqual([])
  })

  it('fetchBriefs unwraps the {briefs:[...]} envelope', async () => {
    const briefs: api.BriefSummary[] = [
      {
        kind: 'people',
        slug: 'sarah-chen',
        title: 'Sarah Chen',
        fact_count: 5,
        last_synthesized_ts: '2026-04-14T00:00:00Z',
        last_synthesized_sha: 'abcdef1',
        pending_delta: 2,
      },
    ]
    vi.spyOn(client, 'get').mockResolvedValue({ briefs })
    const result = await api.fetchBriefs()
    expect(result).toEqual(briefs)
  })

  it('fetchBriefs also accepts a bare array response', async () => {
    const briefs: api.BriefSummary[] = []
    vi.spyOn(client, 'get').mockResolvedValue(briefs)
    const result = await api.fetchBriefs()
    expect(result).toEqual([])
  })

  it('recordFact posts the request body verbatim', async () => {
    const postSpy = vi
      .spyOn(client, 'post')
      .mockResolvedValue({ fact_id: 'f1', fact_count: 1, threshold_crossed: false })
    const req: api.RecordFactRequest = {
      entity_kind: 'people',
      entity_slug: 'sarah-chen',
      fact: 'Prefers async updates over meetings.',
      recorded_by: 'pm',
    }
    const result = await api.recordFact(req)
    expect(postSpy).toHaveBeenCalledWith('/entity/fact', req)
    expect(result.threshold_crossed).toBe(false)
  })

  it('requestBriefSynthesis posts to the synthesize endpoint', async () => {
    const postSpy = vi
      .spyOn(client, 'post')
      .mockResolvedValue({ synthesis_id: 's1', queued_at: '2026-04-20T00:00:00Z' })
    await api.requestBriefSynthesis({
      entity_kind: 'people',
      entity_slug: 'sarah-chen',
      actor_slug: 'human',
    })
    expect(postSpy).toHaveBeenCalledWith('/entity/brief/synthesize', {
      entity_kind: 'people',
      entity_slug: 'sarah-chen',
      actor_slug: 'human',
    })
  })

  it('subscribeEntityEvents returns a no-op when EventSource is undefined', () => {
    const originalES = (globalThis as { EventSource?: unknown }).EventSource
    ;(globalThis as { EventSource?: unknown }).EventSource = undefined
    try {
      const unsub = api.subscribeEntityEvents('people', 'x', () => {}, () => {})
      expect(typeof unsub).toBe('function')
      unsub()
    } finally {
      ;(globalThis as { EventSource?: unknown }).EventSource = originalES
    }
  })

  it('subscribeEntityEvents filters SSE by kind + slug', () => {
    // Arrange — fake EventSource that captures listeners so we can fire events.
    type Listener = (ev: MessageEvent) => void
    const listeners: Record<string, Listener[]> = {}
    const close = vi.fn()
    class FakeES {
      constructor(public url: string) {}
      onerror: ((ev: Event) => void) | null = null
      addEventListener(name: string, cb: Listener) {
        ;(listeners[name] ??= []).push(cb)
      }
      removeEventListener(name: string, cb: Listener) {
        listeners[name] = (listeners[name] ?? []).filter((l) => l !== cb)
      }
      close = close
    }
    const originalES = (globalThis as { EventSource?: unknown }).EventSource
    ;(globalThis as { EventSource?: unknown }).EventSource = FakeES
    try {
      const factHits: api.FactRecordedEvent[] = []
      const synthHits: api.BriefSynthesizedEvent[] = []
      const unsub = api.subscribeEntityEvents(
        'people',
        'sarah-chen',
        (ev) => factHits.push(ev),
        (ev) => synthHits.push(ev),
      )
      // Fire a matching fact event.
      listeners['entity:fact_recorded'][0](
        new MessageEvent('message', {
          data: JSON.stringify({
            kind: 'people',
            slug: 'sarah-chen',
            fact_id: 'f1',
            recorded_by: 'pm',
            fact_count: 1,
            threshold_crossed: false,
            timestamp: '2026-04-20T00:00:00Z',
          }),
        }),
      )
      // Fire a non-matching event for the same kind, different slug.
      listeners['entity:fact_recorded'][0](
        new MessageEvent('message', {
          data: JSON.stringify({
            kind: 'people',
            slug: 'someone-else',
            fact_id: 'f2',
            recorded_by: 'pm',
            fact_count: 1,
            threshold_crossed: false,
            timestamp: '2026-04-20T00:00:00Z',
          }),
        }),
      )
      // Fire a matching synth event.
      listeners['entity:brief_synthesized'][0](
        new MessageEvent('message', {
          data: JSON.stringify({
            kind: 'people',
            slug: 'sarah-chen',
            commit_sha: 'abc',
            fact_count: 3,
            synthesized_ts: '2026-04-20T00:00:00Z',
          }),
        }),
      )
      // Malformed event should not throw.
      listeners['entity:fact_recorded'][0](
        new MessageEvent('message', { data: '{not-json' }),
      )

      expect(factHits).toHaveLength(1)
      expect(factHits[0].fact_id).toBe('f1')
      expect(synthHits).toHaveLength(1)
      expect(synthHits[0].commit_sha).toBe('abc')

      unsub()
      expect(close).toHaveBeenCalled()
    } finally {
      ;(globalThis as { EventSource?: unknown }).EventSource = originalES
    }
  })
})
