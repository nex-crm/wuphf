import { useEffect, useState } from 'react'
import AuthorShelfSidebar from './AuthorShelfSidebar'
import NotebookEntryView from './NotebookEntry'
import {
  fetchAgentEntries,
  type NotebookAgentSummary,
  type NotebookEntry,
} from '../../api/notebook'

/**
 * `/notebooks/{agent-slug}[/{entry-slug}]` — two-column view. Left: dated
 * log of this agent's entries. Right: the selected entry rendered in full
 * (defaults to most recent).
 */

interface AgentNotebookViewProps {
  agentSlug: string
  entrySlug?: string | null
  onNavigateCatalog: () => void
  onSelectEntry: (entrySlug: string | null) => void
  onNavigateWiki?: (wikiPath: string) => void
}

export default function AgentNotebookView({
  agentSlug,
  entrySlug,
  onNavigateCatalog,
  onSelectEntry,
  onNavigateWiki,
}: AgentNotebookViewProps) {
  const [agent, setAgent] = useState<NotebookAgentSummary | null>(null)
  const [entries, setEntries] = useState<NotebookEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)
    fetchAgentEntries(agentSlug)
      .then((res) => {
        if (cancelled) return
        setAgent(res.agent)
        setEntries(res.entries)
      })
      .catch((err: unknown) => {
        if (cancelled) return
        setError(err instanceof Error ? err.message : 'Failed to load notebook')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => { cancelled = true }
  }, [agentSlug])

  if (loading) {
    return (
      <div className="nb-layout">
        <div className="nb-shelf"><span className="nb-skeleton" /><span className="nb-skeleton" /></div>
        <div className="nb-article" aria-busy="true">
          <div className="nb-loading">Loading notebook…</div>
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="nb-article">
        <p className="nb-error" role="alert">Error: {error}</p>
        <button
          type="button"
          className="nb-retry-btn"
          onClick={() => {
            setLoading(true)
            setError(null)
            fetchAgentEntries(agentSlug)
              .then((res) => {
                setAgent(res.agent)
                setEntries(res.entries)
              })
              .catch((err: unknown) => setError(err instanceof Error ? err.message : 'Retry failed'))
              .finally(() => setLoading(false))
          }}
        >
          Retry
        </button>
      </div>
    )
  }

  if (!agent) {
    return (
      <div className="nb-article">
        <p className="nb-error">Agent not found: {agentSlug}</p>
        <button type="button" className="nb-retry-btn" onClick={onNavigateCatalog}>
          Back to bookshelf
        </button>
      </div>
    )
  }

  // Pick the entry to render: explicit slug → matching entry; else first.
  const activeEntry: NotebookEntry | null = entrySlug
    ? entries.find((e) => e.entry_slug === entrySlug) ?? null
    : entries[0] ?? null

  return (
    <div className="nb-layout">
      <AuthorShelfSidebar
        agent={agent}
        entries={entries.map((e) => ({
          entry_slug: e.entry_slug,
          title: e.title,
          last_edited_ts: e.last_edited_ts,
          status: e.status,
        }))}
        currentEntrySlug={activeEntry?.entry_slug ?? null}
        onSelect={(slug) => onSelectEntry(slug)}
      />
      {activeEntry ? (
        <NotebookEntryView
          entry={activeEntry}
          onNavigateCatalog={onNavigateCatalog}
          onNavigateAgent={() => onSelectEntry(null)}
          onNavigateWiki={onNavigateWiki}
        />
      ) : (
        <div className="nb-empty-prompt">
          <p>No entries yet — {agent.name} has not written anything.</p>
        </div>
      )}
    </div>
  )
}
