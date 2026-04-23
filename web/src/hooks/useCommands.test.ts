import { describe, expect, it } from 'vitest'
import type { SlashCommandDescriptor } from '../api/client'
import { __test__, FALLBACK_SLASH_COMMANDS } from './useCommands'

const { toAutocomplete, COMMAND_ICONS, DEFAULT_ICON } = __test__

describe('toAutocomplete', () => {
  it('filters out TUI-only commands and prefixes slash to the name', () => {
    const broker: SlashCommandDescriptor[] = [
      { name: 'ask', description: 'Ask the team lead', webSupported: true },
      { name: 'object', description: 'Object commands', webSupported: false },
      { name: 'clear', description: 'Clear messages', webSupported: true },
    ]
    const mapped = toAutocomplete(broker)
    expect(mapped.map((c) => c.name)).toEqual(['/ask', '/clear'])
  })

  it('maps known commands to their icon', () => {
    const broker: SlashCommandDescriptor[] = [
      { name: 'ask', description: 'Ask', webSupported: true },
      { name: 'calendar', description: 'Calendar', webSupported: true },
    ]
    const mapped = toAutocomplete(broker)
    expect(mapped[0].icon).toBe(COMMAND_ICONS.ask)
    expect(mapped[1].icon).toBe(COMMAND_ICONS.calendar)
  })

  it('assigns the default icon to unknown commands so autocomplete never shows a blank glyph', () => {
    const broker: SlashCommandDescriptor[] = [
      { name: 'brand-new-command', description: 'Future command', webSupported: true },
    ]
    const mapped = toAutocomplete(broker)
    expect(mapped[0].icon).toBe(DEFAULT_ICON)
  })

  it('preserves the broker description verbatim — broker is the source of truth', () => {
    const broker: SlashCommandDescriptor[] = [
      { name: 'ask', description: 'Custom override description', webSupported: true },
    ]
    const mapped = toAutocomplete(broker)
    expect(mapped[0].desc).toBe('Custom override description')
  })

  it('returns an empty array when every command is TUI-only', () => {
    const broker: SlashCommandDescriptor[] = [
      { name: 'object', description: 'TUI', webSupported: false },
      { name: 'record', description: 'TUI', webSupported: false },
    ]
    expect(toAutocomplete(broker)).toEqual([])
  })

  it('returns an empty array for an empty broker response', () => {
    expect(toAutocomplete([])).toEqual([])
  })
})

describe('FALLBACK_SLASH_COMMANDS', () => {
  // This locks in the fallback contract: if the broker is unreachable, the
  // autocomplete still populates with the web-supported command set the
  // composer knows how to execute.
  it('covers every command the composer handler currently implements', () => {
    const expected = [
      '/ask', '/search', '/remember', '/help', '/clear', '/reset',
      '/tasks', '/requests', '/recover', '/1o1', '/task', '/cancel',
      '/policies', '/calendar', '/skills', '/focus', '/collab',
      '/pause', '/resume', '/threads', '/provider',
    ].sort()
    expect(FALLBACK_SLASH_COMMANDS.map((c) => c.name).sort()).toEqual(expected)
  })

  it('never ships an empty icon — every fallback entry has a glyph', () => {
    for (const cmd of FALLBACK_SLASH_COMMANDS) {
      expect(cmd.icon).not.toBe('')
    }
  })

  // Real-world bug: useCommands returned a fresh array on every render, so
  // the Autocomplete effect that watches `commands` + items fired on every
  // render, called `onItems(items)` which called setAcItems in Composer,
  // re-rendering Composer, which re-ran useCommands, which returned a new
  // array... React bailed with "Maximum update depth exceeded" and the UI
  // thrashed into unresponsiveness. Referential stability of the returned
  // list is load-bearing, not cosmetic.
  it('toAutocomplete returns a stable result shape for identical input', () => {
    const broker: SlashCommandDescriptor[] = [
      { name: 'ask', description: 'Ask the team lead', webSupported: true },
    ]
    const a = toAutocomplete(broker)
    const b = toAutocomplete(broker)
    // Same-content input → equal shape. The useMemo in useCommands takes
    // care of referential identity across renders; this pins the pure
    // helper's deterministic output contract.
    expect(a).toEqual(b)
  })
})
