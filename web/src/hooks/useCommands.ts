import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchCommands, type SlashCommandDescriptor } from '../api/client'

/**
 * Web-autocomplete view of one slash command. Mirrors the legacy
 * SLASH_COMMANDS constant shape used by Autocomplete, so the renderer does
 * not need to know whether the list came from the broker or the hardcoded
 * fallback.
 */
export interface SlashCommand {
  name: string
  desc: string
  icon: string
}

/**
 * Fallback used when the broker is unreachable. Keep the set + ordering in
 * sync with the broker's GET /commands output for webSupported=true. Today
 * both lists are derived from Composer.tsx's handleSlashCommand switch. If
 * they ever drift, the broker is the source of truth and this list is a
 * degraded-mode copy.
 *
 * Icons are web-only metadata — the broker does not carry them because the
 * TUI uses a different rendering path. Strings are escaped unicode to
 * match the codebase convention (the existing composer files avoid raw
 * emoji literals in source).
 */
export const FALLBACK_SLASH_COMMANDS: SlashCommand[] = [
  { name: '/ask', desc: 'Ask the team lead', icon: '💬' },
  { name: '/search', desc: 'Search messages + KB', icon: '🔎' },
  { name: '/remember', desc: 'Store a fact in memory', icon: '🧠' },
  { name: '/help', desc: 'Show all commands + keys', icon: '❓' },
  { name: '/clear', desc: 'Clear messages', icon: '🧹' },
  { name: '/reset', desc: 'Reset the office', icon: '🔄' },
  { name: '/tasks', desc: 'Open task board', icon: '📋' },
  { name: '/requests', desc: 'Open requests', icon: '🔔' },
  { name: '/recover', desc: 'Health Check view', icon: '🔁' },
  { name: '/1o1', desc: '1:1 with agent', icon: '💬' },
  { name: '/task', desc: 'Task actions', icon: '✅' },
  { name: '/cancel', desc: 'Cancel a task', icon: '❌' },
  { name: '/policies', desc: 'View policies', icon: '📜' },
  { name: '/calendar', desc: 'View schedule', icon: '📅' },
  { name: '/skills', desc: 'View skills', icon: '⚡' },
  { name: '/focus', desc: 'Switch to delegation mode', icon: '🎯' },
  { name: '/collab', desc: 'Switch to collaborative mode', icon: '🤝' },
  { name: '/pause', desc: 'Pause all agents', icon: '⏸' },
  { name: '/resume', desc: 'Resume all agents', icon: '▶' },
  { name: '/threads', desc: 'See every active thread', icon: '🧵' },
  { name: '/provider', desc: 'Switch runtime provider', icon: '⚙' },
]

/**
 * Icon map for commands returned by the broker. Keyed by bare command name
 * (no leading slash). Unknown commands fall back to a generic icon so the
 * autocomplete never renders a blank glyph if someone adds a TUI command
 * and flips webSupported before updating this list.
 */
const COMMAND_ICONS: Record<string, string> = {
  ask: '💬',
  search: '🔎',
  remember: '🧠',
  help: '❓',
  clear: '🧹',
  reset: '🔄',
  tasks: '📋',
  requests: '🔔',
  recover: '🔁',
  '1o1': '💬',
  task: '✅',
  cancel: '❌',
  policies: '📜',
  calendar: '📅',
  skills: '⚡',
  focus: '🎯',
  collab: '🤝',
  pause: '⏸',
  resume: '▶',
  threads: '🧵',
  provider: '⚙',
}

const DEFAULT_ICON = '›'

/**
 * Convert the broker's payload into the shape the autocomplete renderer
 * expects. Filters to webSupported=true and only keeps commands the web
 * actually knows how to execute.
 */
function toAutocomplete(commands: SlashCommandDescriptor[]): SlashCommand[] {
  return commands
    .filter((c) => c.webSupported)
    .map((c) => ({
      name: '/' + c.name,
      desc: c.description,
      icon: COMMAND_ICONS[c.name] ?? DEFAULT_ICON,
    }))
}

/**
 * Read the canonical slash-command registry. Returns the broker's view when
 * available, or the hardcoded fallback if the broker is unreachable. The
 * hook never throws — a missing registry is a recoverable degradation, not
 * an error state the UI needs to render.
 *
 * The autocomplete UX does not change between the two modes; only the set
 * of commands shown might.
 */
export function useCommands(): SlashCommand[] {
  const { data, isError } = useQuery({
    queryKey: ['commands'],
    queryFn: fetchCommands,
    // Registry only changes on rebuild. Five minutes is enough to absorb a
    // dev loop without hammering the broker.
    staleTime: 5 * 60_000,
    // Failures fall through to the fallback — don't retry aggressively.
    retry: 1,
  })

  // Memoize the derived view so consumers relying on the returned array as
  // a dependency (e.g. the autocomplete effect) don't see a fresh reference
  // on every render. Without this, every Composer render rebuilt `commands`,
  // which rebuilt the autocomplete `items` array, which re-fired the effect
  // that calls `onItems(items)` — looping setState until React bailed with
  // "Maximum update depth exceeded."
  return useMemo(() => {
    if (isError || !data) {
      return FALLBACK_SLASH_COMMANDS
    }
    const mapped = toAutocomplete(data)
    // Defensive: if the broker returns an empty webSupported set (e.g. an
    // older broker without the flag), prefer the fallback rather than an
    // empty autocomplete.
    return mapped.length > 0 ? mapped : FALLBACK_SLASH_COMMANDS
  }, [data, isError])
}

// Exported for tests.
export const __test__ = {
  toAutocomplete,
  COMMAND_ICONS,
  DEFAULT_ICON,
}
