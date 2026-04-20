import { describe, expect, it } from 'vitest'
import { parseWikiLink, wikiLinkRemarkPlugin } from './wikilink'
import fixtures from '../../tests/fixtures/wikilinks.json'

interface Fixture {
  input: string
  expected: { slug: string; display: string } | null
}

describe('parseWikiLink', () => {
  for (const entry of fixtures as Fixture[]) {
    it(`handles ${JSON.stringify(entry.input)}`, () => {
      // Arrange
      const input = entry.input
      // Act
      const result = parseWikiLink(input)
      // Assert
      expect(result).toEqual(entry.expected)
    })
  }

  it('rejects non-string input', () => {
    // @ts-expect-error intentional misuse
    expect(parseWikiLink(null)).toBeNull()
    // @ts-expect-error intentional misuse
    expect(parseWikiLink(undefined)).toBeNull()
  })

  it('rejects input without enclosing brackets', () => {
    expect(parseWikiLink('slug')).toBeNull()
  })
})

describe('wikiLinkRemarkPlugin', () => {
  it('transforms [[slug]] inside a text node into a link AST node', () => {
    // Arrange
    const resolver = (slug: string) => slug === 'people/nazz'
    const buildTransformer = wikiLinkRemarkPlugin(resolver)
    const transformer = buildTransformer()
    const tree = {
      type: 'root',
      children: [
        {
          type: 'paragraph',
          children: [
            { type: 'text', value: 'See [[people/nazz]] and [[missing|Miss]] here.' },
          ],
        },
      ],
    }

    // Act
    transformer(tree)

    // Assert
    const paragraph = tree.children[0] as { children: Array<{ type: string; url?: string; data?: { hProperties?: Record<string, string> } }> }
    expect(paragraph.children).toHaveLength(5)
    expect(paragraph.children[0]).toMatchObject({ type: 'text', value: 'See ' })
    expect(paragraph.children[1]).toMatchObject({ type: 'link', url: '#/wiki/people/nazz' })
    expect(paragraph.children[1].data?.hProperties).toMatchObject({
      'data-wikilink': 'true',
      'data-broken': 'false',
      className: 'wk-wikilink',
    })
    expect(paragraph.children[3]).toMatchObject({ type: 'link', url: '#/wiki/missing' })
    expect(paragraph.children[3].data?.hProperties).toMatchObject({
      'data-broken': 'true',
      className: 'wk-wikilink wk-broken',
    })
  })

  it('leaves text untouched when no wikilinks are present', () => {
    // Arrange
    const buildTransformer = wikiLinkRemarkPlugin(() => true)
    const transformer = buildTransformer()
    const tree = {
      type: 'root',
      children: [
        { type: 'paragraph', children: [{ type: 'text', value: 'Plain prose only.' }] },
      ],
    }
    // Act
    transformer(tree)
    // Assert
    const para = tree.children[0] as { children: { type: string; value?: string }[] }
    expect(para.children).toHaveLength(1)
    expect(para.children[0]).toMatchObject({ type: 'text', value: 'Plain prose only.' })
  })

  it('ignores malformed wikilinks like [[..]] and [[a|b|c]]', () => {
    // Arrange
    const buildTransformer = wikiLinkRemarkPlugin(() => true)
    const transformer = buildTransformer()
    const tree = {
      type: 'root',
      children: [
        { type: 'paragraph', children: [{ type: 'text', value: '[[..]] and [[a|b|c]] are bad.' }] },
      ],
    }
    // Act
    transformer(tree)
    // Assert — no replacement; original single text node remains.
    const para = tree.children[0] as { children: { type: string; value?: string }[] }
    expect(para.children).toHaveLength(1)
  })
})
