import { useCallback, useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { useAppStore } from '../../stores/app'
import { useChannels } from '../../hooks/useChannels'
import { getMessages } from '../../api/client'
import type { Message } from '../../api/client'

interface SearchResult extends Message {
  /** Matched channel slug for navigation */
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
    // Reset regex lastIndex after test
    regex.lastIndex = 0
    return isMatch ? <mark key={i}>{part}</mark> : part
  })
}

export function SearchModal() {
  const searchOpen = useAppStore((s) => s.searchOpen)
  const setSearchOpen = useAppStore((s) => s.setSearchOpen)
  const setCurrentChannel = useAppStore((s) => s.setCurrentChannel)
  const { data: channels = [] } = useChannels()

  const [query, setQuery] = useState('')
  const [results, setResults] = useState<SearchResult[]>([])
  const [searching, setSearching] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Focus input when modal opens
  useEffect(() => {
    if (searchOpen) {
      // Small delay lets the DOM render before focusing
      const t = setTimeout(() => inputRef.current?.focus(), 50)
      return () => clearTimeout(t)
    }
    // Reset state when closing
    setQuery('')
    setResults([])
  }, [searchOpen])

  // Close on Escape
  useEffect(() => {
    if (!searchOpen) return

    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        setSearchOpen(false)
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [searchOpen, setSearchOpen])

  // Search: fetch recent messages from all channels and filter client-side
  const runSearch = useCallback(
    async (q: string) => {
      const trimmed = q.trim().toLowerCase()
      if (trimmed.length < 2) {
        setResults([])
        return
      }

      setSearching(true)

      try {
        const fetches = channels.map(async (ch) => {
          try {
            const { messages } = await getMessages(ch.slug, null, 100)
            return messages
              .filter(
                (m) =>
                  m.content?.toLowerCase().includes(trimmed) ||
                  m.from?.toLowerCase().includes(trimmed),
              )
              .map((m): SearchResult => ({ ...m, matchedChannel: ch.slug }))
          } catch {
            return [] as SearchResult[]
          }
        })

        const grouped = await Promise.all(fetches)
        const flat = grouped
          .flat()
          .sort((a, b) => {
            const ta = new Date(a.timestamp).getTime()
            const tb = new Date(b.timestamp).getTime()
            return tb - ta
          })
          .slice(0, 50)

        setResults(flat)
      } finally {
        setSearching(false)
      }
    },
    [channels],
  )

  function handleQueryChange(value: string) {
    setQuery(value)
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => runSearch(value), 300)
  }

  function handleResultClick(result: SearchResult) {
    setCurrentChannel(result.matchedChannel)
    setSearchOpen(false)
  }

  function handleOverlayClick(e: React.MouseEvent) {
    if (e.target === e.currentTarget) {
      setSearchOpen(false)
    }
  }

  if (!searchOpen) return null

  return (
    <div className="search-overlay" onClick={handleOverlayClick}>
      <div className="search-modal card">
        <div className="search-input-wrap">
          <svg
            className="search-input-icon"
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <circle cx="11" cy="11" r="8" />
            <path d="m21 21-4.3-4.3" />
          </svg>
          <input
            ref={inputRef}
            className="search-input"
            type="text"
            placeholder="Search messages..."
            value={query}
            onChange={(e) => handleQueryChange(e.target.value)}
          />
          {searching && <span className="search-spinner" />}
        </div>

        <div className="search-results">
          {query.trim().length > 0 && !searching && results.length === 0 && (
            <div className="search-empty">No results found</div>
          )}

          {results.map((r) => {
            const snippet = r.content.length > 200
              ? r.content.slice(0, 200) + '...'
              : r.content

            return (
              <button
                key={`${r.id}-${r.matchedChannel}`}
                className="search-result"
                onClick={() => handleResultClick(r)}
                type="button"
              >
                <div className="search-result-header">
                  <span className="search-result-author">{r.from}</span>
                  <span className="search-result-channel">#{r.matchedChannel}</span>
                  <span className="search-result-time">{formatTime(r.timestamp)}</span>
                </div>
                <div className="search-result-content">
                  {highlightMatch(snippet, query.trim())}
                </div>
              </button>
            )
          })}
        </div>
      </div>
    </div>
  )
}
