export type InsightPriority = 'critical' | 'high' | 'medium' | 'low'

export interface Insight {
  priority: InsightPriority
  category?: string
  title: string
  body?: string
  target?: string
  time?: string
}

interface InsightsListProps {
  insights: Insight[]
  emptyLabel?: string
  /** Maximum to render before the "and N more" footer. */
  limit?: number
}

const PRIORITY_LABEL: Record<InsightPriority, string> = {
  critical: 'CRIT',
  high: 'HIGH',
  medium: 'MED',
  low: 'LOW',
}

/**
 * Insights renderer ported from internal/tui/render/insights.go.
 * Each row: priority badge, [category], title, time, body, target.
 */
export function InsightsList({ insights, emptyLabel, limit }: InsightsListProps) {
  if (insights.length === 0) {
    return (
      <div className="insight-empty">
        {emptyLabel ?? '(no insights)'}
      </div>
    )
  }
  const visible = typeof limit === 'number' ? insights.slice(0, limit) : insights
  const overflow = insights.length - visible.length

  return (
    <div className="insight-list">
      {visible.map((ins, i) => (
        <div key={i} className={`insight-row insight-${ins.priority}`}>
          <div className="insight-head">
            <span className={`insight-badge insight-badge-${ins.priority}`}>
              [{PRIORITY_LABEL[ins.priority]}]
            </span>
            {ins.category && <span className="insight-category">[{ins.category}]</span>}
            <span className="insight-title">{ins.title}</span>
            {ins.time && <span className="insight-time">{ins.time}</span>}
          </div>
          {ins.body && (
            <div className="insight-body">{truncate(ins.body, 220)}</div>
          )}
          {ins.target && (
            <div className="insight-target">({ins.target})</div>
          )}
        </div>
      ))}
      {overflow > 0 && (
        <div className="insight-more">+ {overflow} more</div>
      )}
    </div>
  )
}

function truncate(text: string, max: number): string {
  if (text.length <= max) return text
  return text.slice(0, max - 3) + '...'
}
