import { describe, expect, it } from 'vitest'
import { resolveGroupOrder } from './groupOrder'

describe('resolveGroupOrder', () => {
  it('returns preferred groups in canonical order when present', () => {
    expect(resolveGroupOrder(['playbooks', 'people'])).toEqual(['people', 'playbooks'])
  })

  it('appends blueprint-specific groups after preferred ones', () => {
    expect(resolveGroupOrder(['customers', 'playbooks', 'scripts'])).toEqual([
      'customers',
      'playbooks',
      'scripts',
    ])
  })

  it('dedupes repeated groups, keeping first-seen', () => {
    expect(resolveGroupOrder(['playbooks', 'scripts', 'playbooks', 'videos'])).toEqual([
      'playbooks',
      'scripts',
      'videos',
    ])
  })

  it('preserves order of first appearance for unknown groups', () => {
    expect(resolveGroupOrder(['videos', 'members', 'events'])).toEqual([
      'videos',
      'members',
      'events',
    ])
  })

  it('handles empty input', () => {
    expect(resolveGroupOrder([])).toEqual([])
  })
})
