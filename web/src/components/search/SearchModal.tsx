import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { useAppStore } from '../../stores/app'
import { useChannels } from '../../hooks/useChannels'
import { useOfficeMembers } from '../../hooks/useMembers'
import { getMessages, post, type Message } from '../../api/client'
import { showNotice } from '../ui/Toast'
import { SLASH_COMMANDS } from '../messages/Autocomplete'

interface PaletteItem {
  id: string
  group: 'Channels' | 'Agents' | 'Commands' | 'Messages'
  icon: string
  label: string
  desc?: string
  meta?: string
  run: () => void
}

interface MessageHit extends Message {
  matchedChannel: string
}

function formatTime(ts: string): string {
  try {
    const d = new Date(ts)
    return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  } catch {
    return ts
  }
}

/** Split text into alternating plain/highlighted segments using React elements. */
function highlightMatch(text: string, query: string): ReactNode {
  if (!query) return text
  const escaped = query.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const regex = new RegExp(`(${escaped})`, 'gi')
  const parts = text.split(regex)
  return parts.map((part, i) => {
    const isMatch = regex.test(part) && part.toLowerCase() === query.toLowerCase()
    regex.lastIndex = 0
    return isMatch ? <mark key={i}>{part}</mark> : part
  })
}

/**
 * Cmd+K command palette. Searches across channels, agents, slash commands,
 * and recent messages. Mirrors the legacy IIFE behavior.
 */
export function SearchModal() {
  const searchOpen = useAppStore((s) => s.searchOpen)
  const setSearchOpen = useAppStore((s) => s.setSearchOpen)
  const setCurrentChannel = useAppStore((s) => s.setCurrentChannel)
  const setCurrentApp = useAppStore((s) => s.setCurrentApp)
  const setActiveAgentSlug = useAppStore((s) => s.setActiveAgentSlug)
  const enterDM = useAppStore((s) => s.enterDM)
  const setLastMessageId = useAppStore((s) => s.setLastMessageId)
  const { data: channels = [] } = useChannels()
  const { data: members = [] } = useOfficeMembers()

  const [query, setQuery] = useState('')
  const [selectedIdx, setSelectedIdx] = useState(0)
  const [messageHits, setMessageHits] = useState<MessageHit[]>([])
  const [searching, setSearching] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const close = useCallback(() => setSearchOpen(false), [setSearchOpen])

  // Focus input when modal opens; reset state when closing
  useEffect(() => {
    if (searchOpen) {
      const t = setTimeout(() => inputRef.current?.focus(), 50)
      return () => clearTimeout(t)
    }
    setQuery('')
    setMessageHits([])
    setSelectedIdx(0)
  }, [searchOpen])

  // Debounced message search
  const runMessageSearch = useCallback(
    async (q: string) => {
      const trimmed = q.trim().toLowerCase()
      if (trimmed.length < 2 || channels.length === 0) {
        setMessageHits([])
        return
      }
      setSearching(true)
      try {
        const fetches = channels.map(async (ch) => {
          try {
            const { messages } = await getMessages(ch.slug, null, 100)
            return messages
              .filter((m) => m.content?.toLowerCase().includes(trimmed))
              .map((m): MessageHit => ({ ...m, matchedChannel: ch.slug }))
          } catch {
            return [] as MessageHit[]
          }
        })
        const grouped = await Promise.all(fetches)
        const flat = grouped
          .flat()
          .sort((a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime())
          .slice(0, 8)
        setMessageHits(flat)
      } finally {
        setSearching(false)
      }
    },
    [channels],
  )

  function handleQueryChange(value: string) {
    setQuery(value)
    setSelectedIdx(0)
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => runMessageSearch(value), 250)
  }

  // Build the flat list of items in display order
  const items = useMemo<PaletteItem[]>(() => {
    const q = query.trim().toLowerCase()
    const list: PaletteItem[] = []

    // Channels
    for (const ch of channels) {
      const hay = `${ch.slug} ${ch.name ?? ''} ${ch.description ?? ''}`.toLowerCase()
      if (q && !hay.includes(q.replace(/^#/, ''))) continue
      list.push({
        id: 'ch:' + ch.slug,
        group: 'Channels',
        icon: '#',
        label: ch.name || ch.slug,
        desc: ch.description,
        meta: '#' + ch.slug,
        run: () => {
          setCurrentApp(null)
          setCurrentChannel(ch.slug)
          setLastMessageId(null)
          close()
        },
      })
    }

    // Agents
    for (const m of members) {
      if (!m.slug || m.slug === 'human' || m.slug === 'you' || m.slug === 'system') continue
      const hay = `${m.slug} ${m.name ?? ''} ${m.role ?? ''}`.toLowerCase()
      if (q && !hay.includes(q.replace(/^@/, ''))) continue
      list.push({
        id: 'ag:' + m.slug,
        group: 'Agents',
        icon: m.emoji || '\uD83E\uDD16',
        label: m.name || m.slug,
        desc: m.role,
        meta: '@' + m.slug,
        run: () => {
          setActiveAgentSlug(m.slug)
          close()
        },
      })
    }

    // Slash commands
    for (const c of SLASH_COMMANDS) {
      const hay = `${c.name} ${c.desc}`.toLowerCase()
      if (q && !hay.includes(q.replace(/^\//, ''))) continue
      list.push({
        id: 'cmd:' + c.name,
        group: 'Commands',
        icon: c.icon,
        label: c.name,
        desc: c.desc,
        run: () => {
          // Map command to its action via the same dispatcher the composer uses
          dispatchPaletteCommand(c.name, {
            setCurrentApp,
            setCurrentChannel,
            setLastMessageId,
            setSearchOpen,
            enterDM,
          })
          close()
        },
      })
    }

    // Message hits (only when there's a query)
    if (q.length >= 2) {
      for (const hit of messageHits) {
        const snippet = hit.content.length > 100 ? hit.content.slice(0, 100) + '...' : hit.content
        list.push({
          id: 'msg:' + hit.id + ':' + hit.matchedChannel,
          group: 'Messages',
          icon: '\uD83D\uDCAC',
          label: `${hit.from}: ${snippet}`,
          desc: '#' + hit.matchedChannel + ' · ' + formatTime(hit.timestamp),
          run: () => {
            setCurrentApp(null)
            setCurrentChannel(hit.matchedChannel)
            setLastMessageId(null)
            close()
          },
        })
      }
    }

    return list
  }, [query, channels, members, messageHits, setCurrentApp, setCurrentChannel, setActiveAgentSlug, setLastMessageId, setSearchOpen, enterDM, close])

  // Clamp selection
  useEffect(() => {
    setSelectedIdx((idx) => Math.min(idx, Math.max(items.length - 1, 0)))
  }, [items.length])

  // Keyboard handling
  useEffect(() => {
    if (!searchOpen) return
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.preventDefault()
        close()
        return
      }
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setSelectedIdx((i) => (items.length === 0 ? 0 : (i + 1) % items.length))
        return
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault()
        setSelectedIdx((i) => (items.length === 0 ? 0 : (i - 1 + items.length) % items.length))
        return
      }
      if (e.key === 'Enter') {
        e.preventDefault()
        const item = items[selectedIdx]
        if (item) item.run()
      }
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [searchOpen, items, selectedIdx, close])

  // Group items for rendering, preserving the flat index for selection
  const grouped = useMemo(() => {
    const out: { group: PaletteItem['group']; items: { item: PaletteItem; flatIdx: number }[] }[] = []
    items.forEach((item, idx) => {
      const last = out[out.length - 1]
      if (last && last.group === item.group) {
        last.items.push({ item, flatIdx: idx })
      } else {
        out.push({ group: item.group, items: [{ item, flatIdx: idx }] })
      }
    })
    return out
  }, [items])

  function handleOverlayClick(e: React.MouseEvent) {
    if (e.target === e.currentTarget) close()
  }

  if (!searchOpen) return null

  return (
    <div className="search-overlay" onClick={handleOverlayClick}>
      <div className="search-modal card cmd-palette">
        <div className="search-input-wrap">
          <svg className="search-input-icon" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <circle cx="11" cy="11" r="8" />
            <path d="m21 21-4.3-4.3" />
          </svg>
          <input
            ref={inputRef}
            className="search-input"
            type="text"
            placeholder="Search channels, agents, commands, messages..."
            value={query}
            onChange={(e) => handleQueryChange(e.target.value)}
          />
          {searching && <span className="search-spinner" />}
        </div>

        <div className="cmd-palette-results">
          {items.length === 0 ? (
            <div className="cmd-palette-empty">
              {query ? `No results for "${query}"` : 'Start typing to search...'}
            </div>
          ) : (
            grouped.map((g) => (
              <div key={g.group} className="cmd-palette-group">
                <div className="cmd-palette-group-title">{g.group}</div>
                {g.items.map(({ item, flatIdx }) => (
                  <button
                    key={item.id}
                    type="button"
                    className={`cmd-palette-item${flatIdx === selectedIdx ? ' selected' : ''}`}
                    onMouseEnter={() => setSelectedIdx(flatIdx)}
                    onClick={item.run}
                  >
                    <span className="cmd-palette-item-icon">{item.icon}</span>
                    <span className="cmd-palette-item-text">
                      <span className="cmd-palette-item-label">
                        {item.group === 'Messages' ? highlightMatch(item.label, query.trim()) : item.label}
                      </span>
                      {item.desc && (
                        <span className="cmd-palette-item-desc">{item.desc}</span>
                      )}
                    </span>
                    {item.meta && <span className="cmd-palette-item-meta">{item.meta}</span>}
                  </button>
                ))}
              </div>
            ))
          )}
        </div>

        <div className="cmd-palette-footer">
          <span><kbd>↑</kbd><kbd>↓</kbd> navigate</span>
          <span><kbd>↵</kbd> open</span>
          <span><kbd>esc</kbd> close</span>
        </div>
      </div>
    </div>
  )
}

interface CommandDeps {
  setCurrentApp: (id: string | null) => void
  setCurrentChannel: (slug: string) => void
  setLastMessageId: (id: string | null) => void
  setSearchOpen: (open: boolean) => void
  enterDM: (agentSlug: string, channelSlug: string) => void
}

function dispatchPaletteCommand(name: string, deps: CommandDeps) {
  switch (name) {
    case '/clear':
      showNotice('Messages cleared', 'info')
      return
    case '/help':
      showNotice('Use the palette: type to filter, ↑↓ to move, Enter to open.', 'info')
      return
    case '/requests':
      deps.setCurrentApp('requests'); return
    case '/policies':
      deps.setCurrentApp('policies'); return
    case '/skills':
      deps.setCurrentApp('skills'); return
    case '/calendar':
      deps.setCurrentApp('calendar'); return
    case '/tasks':
      deps.setCurrentApp('tasks'); return
    case '/recover':
    case '/doctor':
      deps.setCurrentApp('health-check'); return
    case '/search':
      deps.setSearchOpen(true); return
    case '/focus':
      post('/focus-mode', { focus_mode: true })
        .then(() => showNotice('Switched to delegation mode', 'success'))
        .catch((e: Error) => showNotice('Failed to switch mode: ' + e.message, 'error'))
      return
    case '/collab':
      post('/focus-mode', { focus_mode: false })
        .then(() => showNotice('Switched to collaborative mode', 'success'))
        .catch((e: Error) => showNotice('Failed to switch mode: ' + e.message, 'error'))
      return
    case '/pause':
      post('/signals', { kind: 'pause', summary: 'Human paused all agents' })
        .then(() => showNotice('All agents paused', 'success'))
        .catch((e: Error) => showNotice('Pause failed: ' + e.message, 'error'))
      return
    case '/resume':
      post('/signals', { kind: 'resume', summary: 'Human resumed agents' })
        .then(() => showNotice('Agents resumed', 'success'))
        .catch((e: Error) => showNotice('Resume failed: ' + e.message, 'error'))
      return
    case '/reset':
      post('/reset', {})
        .then(() => {
          deps.setLastMessageId(null)
          deps.setCurrentChannel('general')
          showNotice('Office reset', 'success')
        })
        .catch((e: Error) => showNotice('Reset failed: ' + e.message, 'error'))
      return
    default:
      showNotice(`${name} requires arguments — type it in the composer.`, 'info')
      return
  }
}
