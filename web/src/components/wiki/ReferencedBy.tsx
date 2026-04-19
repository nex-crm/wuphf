import PixelAvatar from './PixelAvatar'

/** Right-rail backlinks: articles that link TO this article. */

export interface Backlink {
  path: string
  title: string
  author_slug: string
}

interface ReferencedByProps {
  backlinks: Backlink[]
  onNavigate?: (path: string) => void
}

export default function ReferencedBy({ backlinks, onNavigate }: ReferencedByProps) {
  return (
    <div>
      <h4>
        Referenced by
        <span className="wk-toggle" data-testid="wk-backlink-count">{backlinks.length}</span>
      </h4>
      <div className="wk-backlinks">
        {backlinks.map((b) => (
          <a
            key={b.path}
            href={`#/wiki/${encodeURI(b.path)}`}
            onClick={(e) => {
              if (onNavigate) {
                e.preventDefault()
                onNavigate(b.path)
              }
            }}
          >
            <PixelAvatar slug={b.author_slug} size={16} />
            {b.title}
            <span className="wk-path">{b.author_slug.toUpperCase()}</span>
          </a>
        ))}
      </div>
    </div>
  )
}
