import { PixelAvatar } from '../ui/PixelAvatar'
import { formatRelativeTime } from '../../lib/format'
import type { NotebookAgentSummary } from '../../api/notebook'

/**
 * One agent's row on the `/notebooks` bookshelf. Shows avatar + Caveat
 * name + system-ui role, the last 3-5 entries as mini cards, and the
 * right-aligned mono stats pill.
 */

interface AgentShelfProps {
  agent: NotebookAgentSummary
  onOpenAgent: (agentSlug: string) => void
  onOpenEntry: (agentSlug: string, entrySlug: string) => void
  /** How many recent entries to preview (default 5). */
  previewCount?: number
}

function safeRelative(ts: string): string {
  try { return formatRelativeTime(ts) } catch { return ts }
}

function badgeForStatus(status: string): { text: string; className: string } | null {
  if (status === 'draft') return { text: 'DRAFT', className: 'nb-shelf-card-badge-draft' }
  if (status === 'promoted') return { text: 'promoted', className: 'nb-shelf-card-badge-promoted' }
  if (status === 'in-review') return { text: 'in review', className: 'nb-shelf-card-badge-review' }
  if (status === 'changes-requested') return { text: 'changes', className: 'nb-shelf-card-badge-review' }
  return null
}

export default function AgentShelf({
  agent,
  onOpenAgent,
  onOpenEntry,
  previewCount = 5,
}: AgentShelfProps) {
  const preview = agent.entries.slice(0, previewCount)

  return (
    <section
      className="nb-shelf-row"
      aria-label={`${agent.name}'s notebook shelf`}
    >
      <div className="nb-shelf-row-head">
        <PixelAvatar slug={agent.agent_slug} size={28} />
        <div>
          <button
            type="button"
            className="nb-shelf-row-name"
            onClick={() => onOpenAgent(agent.agent_slug)}
          >
            {agent.name}'s notebook
          </button>
          <div className="nb-shelf-row-role">{agent.role}</div>
          <div className="nb-shelf-row-stats">
            {agent.total} entr{agent.total === 1 ? 'y' : 'ies'} · {agent.promoted_count}{' '}
            promoted · updated {safeRelative(agent.last_updated_ts)}
          </div>
        </div>
      </div>
      {preview.length === 0 ? (
        <p className="nb-shelf-row-empty">No entries yet — still a blank page.</p>
      ) : (
        <div className="nb-shelf-row-cards">
          {preview.map((entry) => {
            const badge = badgeForStatus(entry.status)
            return (
              <button
                type="button"
                key={entry.entry_slug}
                className="nb-shelf-card"
                onClick={() => onOpenEntry(agent.agent_slug, entry.entry_slug)}
              >
                <span className="nb-shelf-card-t">{entry.title}</span>
                <span className="nb-shelf-card-meta">
                  <span>{safeRelative(entry.last_edited_ts)}</span>
                  {badge && <span className={badge.className}>{badge.text}</span>}
                </span>
              </button>
            )
          })}
        </div>
      )}
    </section>
  )
}
