import { describe, expect, it } from 'vitest'
import { resolveKnownPortraitSprite } from './avatarSprites.generated'
import { getAgentColor, resolvePortraitSprite } from './pixelAvatar'

describe('pixel avatar sprite resolution', () => {
  it('maps operation-created agent slugs into the generated avatar catalog', () => {
    const mappings = new Map([
      ['planner', 'hybridPm'],
      ['builder', 'hybridEng'],
      ['growth', 'hybridGtm'],
      ['reviewer', 'hybridQa'],
      ['operator', 'hybridNex'],
    ])

    for (const [slug, id] of mappings) {
      expect(resolveKnownPortraitSprite(slug)?.id).toBe(id)
    }
  })

  it('keeps arbitrary new-agent slugs on generated office sprites', () => {
    const avatar = resolvePortraitSprite('custom-ops-agent')
    const idParts = avatar.id.split(':')
    const baseID = idParts[idParts.length - 1]

    expect(avatar.id).toMatch(/^procedural:custom-ops-agent:hybrid/)
    expect(['hybridCeo', 'hybridGeneric', 'hybridHuman', 'hybridPam', 'hybridPamCute']).not.toContain(baseID)
    expect(avatar.portrait.length).toBeGreaterThan(0)
  })

  it('procedurally varies generated office palettes by slug', () => {
    const first = resolvePortraitSprite('custom-ops-agent')
    const again = resolvePortraitSprite('custom-ops-agent')
    const second = resolvePortraitSprite('custom-sales-agent')

    expect(first.id).toBe(again.id)
    expect(first.palette).toEqual(again.palette)
    expect(`${first.id}:${first.palette.join(',')}`).not.toBe(`${second.id}:${second.palette.join(',')}`)
  })

  it('keeps procedural agent colors stable and accent-like', () => {
    expect(getAgentColor('ceo')).toBe('#E8A838')
    expect(getAgentColor('custom-ops-agent')).toMatch(/^#[0-9A-F]{6}$/i)
    expect(getAgentColor('custom-ops-agent')).toBe(getAgentColor('custom-ops-agent'))
  })
})
