import { describe, expect, it } from 'vitest'
import { currentTrigger, applyAutocomplete } from './Autocomplete'

describe('currentTrigger', () => {
  describe('mention scoping — TUI parity with internal/tui/mention.go', () => {
    it('triggers on @ at start of input', () => {
      const t = currentTrigger('@ce', 3)
      expect(t).toEqual({ kind: 'mention', query: 'ce', start: 0 })
    })

    it('triggers on @ after a space', () => {
      const t = currentTrigger('hi @ce', 6)
      expect(t).toEqual({ kind: 'mention', query: 'ce', start: 3 })
    })

    it('triggers on @ after a newline', () => {
      const t = currentTrigger('hi\n@ce', 6)
      expect(t).toEqual({ kind: 'mention', query: 'ce', start: 3 })
    })

    it('does NOT trigger on @ preceded by non-whitespace (foo@bar email)', () => {
      const t = currentTrigger('foo@bar', 7)
      expect(t).toBeNull()
    })

    it('does NOT trigger on @ surrounded by letters', () => {
      const t = currentTrigger('hi.foo@example.com', 18)
      expect(t).toBeNull()
    })

    it('does not trigger when a space sits between @ and the caret', () => {
      const t = currentTrigger('@ce and more', 12)
      expect(t).toBeNull()
    })
  })

  describe('slash scoping', () => {
    it('triggers only when value starts with /', () => {
      expect(currentTrigger('/cl', 3)).toEqual({ kind: 'slash', query: 'cl', start: 0 })
    })

    it('does not trigger for leading whitespace before /', () => {
      expect(currentTrigger(' /cl', 4)).toBeNull()
    })

    it('does not trigger once the slash token has a space', () => {
      expect(currentTrigger('/ask hi', 7)).toBeNull()
    })
  })
})

describe('applyAutocomplete', () => {
  it('replaces an @mention prefix with the full slug', () => {
    const result = applyAutocomplete('hi @ce', 6, {
      insert: '@ceo',
      label: '@ceo',
    })
    expect(result.text).toBe('hi @ceo ')
    expect(result.caret).toBe(8)
  })

  it('replaces a slash prefix with the full command', () => {
    const result = applyAutocomplete('/as', 3, {
      insert: '/ask',
      label: '/ask',
    })
    expect(result.text).toBe('/ask ')
    expect(result.caret).toBe(5)
  })
})
