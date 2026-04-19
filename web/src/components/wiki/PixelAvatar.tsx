import { getAgentColor } from '../../lib/pixelAvatar'

/**
 * Lightweight SVG pixel-avatar stub used in the wiki surface.
 * TODO: swap for the canvas-based composeAvatar pipeline once we have a
 *       shared React wrapper over `drawPixelAvatar` (see lib/pixelAvatar.ts).
 */

interface PixelAvatarProps {
  slug: string
  size?: number
  className?: string
}

export default function PixelAvatar({ slug, size = 14, className }: PixelAvatarProps) {
  const color = getAgentColor(slug)
  return (
    <svg
      className={className ?? 'wk-avatar'}
      width={size}
      height={size}
      viewBox="0 0 7 7"
      xmlns="http://www.w3.org/2000/svg"
      aria-label={`${slug} avatar`}
    >
      <rect width="7" height="7" fill={color} />
      <rect x="1" y="1" width="5" height="2" fill="rgba(255,255,255,0.18)" />
      <rect x="2" y="4" width="3" height="1" fill="rgba(0,0,0,0.25)" />
    </svg>
  )
}
