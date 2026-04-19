import { describe, expect, it } from 'vitest'
import { formatAgentName } from './agentName'

describe('formatAgentName', () => {
  it('uppercases 2-3 character slugs (role abbreviations)', () => {
    expect(formatAgentName('ceo')).toBe('CEO')
    expect(formatAgentName('pm')).toBe('PM')
    expect(formatAgentName('cro')).toBe('CRO')
    expect(formatAgentName('seo')).toBe('SEO')
  })

  it('title-cases longer slugs', () => {
    expect(formatAgentName('operator')).toBe('Operator')
    expect(formatAgentName('planner')).toBe('Planner')
    expect(formatAgentName('builder')).toBe('Builder')
    expect(formatAgentName('reviewer')).toBe('Reviewer')
    expect(formatAgentName('designer')).toBe('Designer')
  })

  it('title-cases each segment of hyphenated slugs', () => {
    expect(formatAgentName('eng-1')).toBe('Eng-1')
    expect(formatAgentName('product-manager')).toBe('Product-Manager')
    expect(formatAgentName('ops-lead-2')).toBe('Ops-Lead-2')
  })

  it('returns empty string for empty input', () => {
    expect(formatAgentName('')).toBe('')
  })

  it('handles already-capitalized input consistently', () => {
    // Short: always upper
    expect(formatAgentName('CEO')).toBe('CEO')
    // Long: normalize to Title Case
    expect(formatAgentName('OPERATOR')).toBe('Operator')
    expect(formatAgentName('Operator')).toBe('Operator')
  })
})
