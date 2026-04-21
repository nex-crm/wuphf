import PixelAvatar from './PixelAvatar'
import { formatRelativeTime } from '../../lib/format'

/** Article byline: pixel avatar + last-edited-by + amber ts pulse + started date. */

interface BylineProps {
  authorSlug: string
  authorName: string
  lastEditedTs: string
  startedDate?: string
  startedBy?: string
  revisions?: number
}

export default function Byline({
  authorSlug,
  authorName,
  lastEditedTs,
  startedDate,
  startedBy,
  revisions,
}: BylineProps) {
  // Human edits get a distinct pill so readers can tell machine edits
  // from hand edits at a glance. Matches the agent pixel-avatar motif
  // without rendering an avatar (`human` has no generated sprite).
  const isHuman = authorSlug === 'human'
  return (
    <div className="wk-byline">
      <PixelAvatar slug={authorSlug} size={22} />
      <span>
        Last edited by{' '}
        {isHuman ? (
          <span className="wk-name wk-human-pill" data-testid="wk-human-byline">
            Human
          </span>
        ) : (
          <span className="wk-name">{authorName}</span>
        )}
      </span>
      <span className="wk-ts" data-testid="wk-ts">
        {formatBylineTime(lastEditedTs)}
      </span>
      {startedDate && (
        <>
          <span className="wk-dot">•</span>
          <span>
            started <span className="wk-started-date">{startedDate}</span>
            {startedBy ? <> by {startedBy}</> : null}
          </span>
        </>
      )}
      {typeof revisions === 'number' && revisions > 0 && (
        <>
          <span className="wk-dot">•</span>
          <span>{revisions} revisions</span>
        </>
      )}
    </div>
  )
}

function formatBylineTime(ts: string): string {
  try {
    return formatRelativeTime(ts)
  } catch {
    return ts
  }
}
