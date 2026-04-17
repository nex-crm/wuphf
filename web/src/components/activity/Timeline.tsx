import { formatRelativeTime } from '../../lib/format'

export type TimelineEventType =
  | 'created'
  | 'updated'
  | 'deleted'
  | 'note'
  | 'task'
  | 'relationship'
  | 'decision'
  | 'action'
  | 'watchdog'

export interface TimelineEvent {
  type: TimelineEventType
  timestamp: string
  actor?: string
  content: string
  /** Optional secondary line (channel, target, etc.) */
  meta?: string
}

interface TimelineProps {
  events: TimelineEvent[]
  emptyLabel?: string
  limit?: number
}

const ICONS: Record<TimelineEventType, string> = {
  created: '\u25CF', // ●
  updated: '\u25C6', // ◆
  deleted: '\u2715', // ✕
  note: '\u270E', // ✎
  task: '\u2610', // ☐
  relationship: '\u21C4', // ⇄
  decision: '\u25CE', // ◎
  action: '\u2192', // →
  watchdog: '\u26A0', // ⚠
}

/**
 * Vertical timeline ported from internal/tui/render/timeline.go. Icons and
 * dashed connectors between rows. Sorts by timestamp descending.
 */
export function Timeline({ events, emptyLabel, limit }: TimelineProps) {
  if (events.length === 0) {
    return <div className="timeline-empty">{emptyLabel ?? '(no timeline events)'}</div>
  }
  const sorted = [...events].sort((a, b) => b.timestamp.localeCompare(a.timestamp))
  const visible = typeof limit === 'number' ? sorted.slice(0, limit) : sorted

  return (
    <div className="timeline">
      {visible.map((ev, i) => (
        <div key={i} className={`timeline-row timeline-${ev.type}`}>
          <div className="timeline-rail">
            <span className="timeline-icon">{ICONS[ev.type] || '\u00B7'}</span>
            {i < visible.length - 1 && <span className="timeline-connector" />}
          </div>
          <div className="timeline-body">
            <div className="timeline-content">{truncate(ev.content, 220)}</div>
            <div className="timeline-meta">
              {ev.timestamp && <span>{formatRelativeTime(ev.timestamp)}</span>}
              {ev.actor && <span>@{ev.actor}</span>}
              {ev.meta && <span>{ev.meta}</span>}
            </div>
          </div>
        </div>
      ))}
    </div>
  )
}

function truncate(text: string, max: number): string {
  if (text.length <= max) return text
  return text.slice(0, max - 3) + '...'
}
