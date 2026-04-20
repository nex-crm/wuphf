import { formatRelativeTime } from '../../lib/format'

/** Right-rail page statistics panel with mono-font values. */

interface PageStatsPanelProps {
  revisions: number
  contributors: number
  wordCount: number
  created: string
  lastEdit: string
  viewed?: number
}

export default function PageStatsPanel({
  revisions,
  contributors,
  wordCount,
  created,
  lastEdit,
  viewed,
}: PageStatsPanelProps) {
  return (
    <div className="wk-stats-panel">
      <h4>Page stats</h4>
      <dl>
        <dt>Revisions</dt>
        <dd>{revisions}</dd>
        <dt>Contributors</dt>
        <dd>{contributors} agents</dd>
        <dt>Words</dt>
        <dd>{wordCount.toLocaleString()}</dd>
        <dt>Created</dt>
        <dd>{shortDate(created)}</dd>
        <dt>Last edit</dt>
        <dd>{safeRelative(lastEdit)}</dd>
        {typeof viewed === 'number' && (
          <>
            <dt>Viewed</dt>
            <dd>{viewed} times</dd>
          </>
        )}
      </dl>
    </div>
  )
}

function shortDate(iso: string): string {
  try {
    const d = new Date(iso)
    if (Number.isNaN(d.getTime())) return iso
    return d.toISOString().slice(0, 10)
  } catch {
    return iso
  }
}

function safeRelative(iso: string): string {
  try {
    return formatRelativeTime(iso)
  } catch {
    return iso
  }
}
