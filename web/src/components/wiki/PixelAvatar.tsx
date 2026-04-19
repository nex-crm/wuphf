import { PixelAvatar as CanvasPixelAvatar } from '../ui/PixelAvatar'

/**
 * Wiki-surface pixel avatar — default-export wrapper over the shared canvas
 * sprite compositor in `components/ui/PixelAvatar`. Keeps the wiki's byline,
 * backlinks, edit-log entries, Sources list, and catalog cards visually in
 * sync with agent avatars rendered elsewhere in the app.
 */

interface PixelAvatarProps {
  slug: string
  size?: number
  className?: string
  title?: string
}

export default function PixelAvatar({
  slug,
  size = 14,
  className = 'wk-avatar',
  title,
}: PixelAvatarProps) {
  // The underlying component is aria-hidden; the wiki uses avatars purely
  // decorative next to agent slug labels, so no extra role/title is needed.
  // `title` is accepted for API compatibility with the legacy stub and set
  // via a wrapping span when provided.
  const avatar = (
    <CanvasPixelAvatar slug={slug} size={size} className={className} />
  )
  if (title) {
    return (
      <span className="wk-avatar-wrap" title={title}>
        {avatar}
      </span>
    )
  }
  return avatar
}
