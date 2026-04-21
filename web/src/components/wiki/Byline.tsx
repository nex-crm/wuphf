import PixelAvatar from './PixelAvatar'
import { formatRelativeTime } from '../../lib/format'
import type { HumanIdentity } from '../../api/wiki'

/** Article byline: pixel avatar + last-edited-by + amber ts pulse + started date. */

interface BylineProps {
  authorSlug: string
  authorName: string
  lastEditedTs: string
  startedDate?: string
  startedBy?: string
  revisions?: number
  /**
   * Registered human identities from GET /humans. When the author slug
   * matches a registered human, the byline renders that human's display
   * name instead of the generic "Human" pill. Optional so callers that
   * don't care about human attribution (or haven't fetched the registry
   * yet) stay backwards-compatible.
   */
  humans?: HumanIdentity[]
}

export default function Byline({
  authorSlug,
  authorName,
  lastEditedTs,
  startedDate,
  startedBy,
  revisions,
  humans,
}: BylineProps) {
  // v1.4 legacy: `human` was the single synthetic author for every human
  // edit. v1.5 replaces that with per-human identities from GET /humans.
  // Both paths take a distinct "human pill" style so readers can tell
  // machine edits from hand edits at a glance.
  const registeredHuman = humans?.find((h) => h.slug === authorSlug)
  const isLegacyHuman = authorSlug === 'human'
  const isHuman = Boolean(registeredHuman) || isLegacyHuman
  const humanLabel = registeredHuman?.name ?? 'Human'
  return (
    <div className="wk-byline">
      <PixelAvatar slug={authorSlug} size={22} />
      <span>
        Last edited by{' '}
        {isHuman ? (
          <span className="wk-name wk-human-pill" data-testid="wk-human-byline">
            {humanLabel}
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
