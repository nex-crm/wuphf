import { useMemo } from 'react'
import { PixelAvatar } from '../ui/PixelAvatar'
import { formatDateLabel } from '../../lib/format'
import type {
  NotebookAgentSummary,
  NotebookEntrySummary,
} from '../../api/notebook'

/**
 * Left-hand author shelf for `/notebooks/{agent-slug}`. Shows the agent's
 * avatar + "PM's notebook" label, then a reverse-chron dated log of their
 * entries grouped by date header (Caveat display).
 */

interface AuthorShelfSidebarProps {
  agent: NotebookAgentSummary
  entries: NotebookEntrySummary[]
  currentEntrySlug?: string | null
  onSelect: (entrySlug: string) => void
}

interface Group {
  label: string
  key: string
  items: NotebookEntrySummary[]
}

function groupByDay(entries: NotebookEntrySummary[]): Group[] {
  const groups: Record<string, Group> = {}
  const order: string[] = []
  for (const e of entries) {
    const d = new Date(e.last_edited_ts)
    const key = isNaN(d.getTime())
      ? 'unknown'
      : `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
    if (!groups[key]) {
      const label = isNaN(d.getTime())
        ? 'Unknown'
        : `${formatDateLabel(e.last_edited_ts)} · ${key}`
      groups[key] = { label, key, items: [] }
      order.push(key)
    }
    groups[key].items.push(e)
  }
  order.sort((a, b) => (a < b ? 1 : -1))
  return order.map((k) => groups[k])
}

function statusTag(status: NotebookEntrySummary['status']): { label: string; className: string } | null {
  if (status === 'promoted') return { label: '→ Promoted', className: 'nb-promoted' }
  if (status === 'draft') return { label: 'DRAFT', className: 'nb-status-draft' }
  if (status === 'in-review') return { label: 'in review', className: 'nb-status-review' }
  if (status === 'changes-requested') return { label: 'changes req.', className: 'nb-status-changes' }
  return null
}

function formatTimeOnly(iso: string): string {
  const d = new Date(iso)
  if (isNaN(d.getTime())) return ''
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

export default function AuthorShelfSidebar({
  agent,
  entries,
  currentEntrySlug,
  onSelect,
}: AuthorShelfSidebarProps) {
  const groups = useMemo(() => groupByDay(entries), [entries])

  return (
    <aside
      className="nb-shelf"
      aria-label={`${agent.name}'s notebook entries`}
    >
      <div className="nb-shelf-head">
        <PixelAvatar slug={agent.agent_slug} size={22} />
        <div>
          <h2>{agent.name}'s notebook</h2>
          <div className="nb-shelf-role">{agent.role}</div>
        </div>
      </div>
      {entries.length === 0 ? (
        <p className="nb-shelf-empty">No entries yet.</p>
      ) : (
        <ul className="nb-shelf-list">
          {groups.flatMap((g) => [
            <li key={`head-${g.key}`} className="nb-date-head">
              {g.label}
            </li>,
            ...g.items.map((item) => {
              const tag = statusTag(item.status)
              const isCurrent = item.entry_slug === currentEntrySlug
              return (
                <li key={item.entry_slug} style={{ padding: 0, listStyle: 'none' }}>
                  <button
                    type="button"
                    className={`nb-shelf-item${isCurrent ? ' is-current' : ''}`}
                    onClick={() => onSelect(item.entry_slug)}
                    aria-current={isCurrent ? 'page' : undefined}
                  >
                    <span className="nb-shelf-t">{item.title}</span>
                    <span className="nb-shelf-meta">
                      {formatTimeOnly(item.last_edited_ts)}
                      {tag && (
                        <>
                          {' · '}
                          <span className={tag.className}>{tag.label}</span>
                        </>
                      )}
                    </span>
                  </button>
                </li>
              )
            }),
          ])}
        </ul>
      )}
    </aside>
  )
}
