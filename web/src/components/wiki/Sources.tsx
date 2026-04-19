import PixelAvatar from './PixelAvatar'

/** Numbered references — each entry is one git commit that informed the article. */

export interface SourceItem {
  commitSha: string
  authorSlug: string
  authorName?: string
  msg: string
  date: string
}

interface SourcesProps {
  items: SourceItem[]
}

export default function Sources({ items }: SourcesProps) {
  if (items.length === 0) return null
  return (
    <section className="wk-sources" aria-labelledby="wk-sources-heading">
      <h2 id="wk-sources-heading">Sources</h2>
      <ol>
        {items.map((item, i) => (
          <li key={item.commitSha || `src-${i}`} id={`s${i + 1}`}>
            <span className="wk-commit-msg">{item.msg}</span>
            <span className="wk-agent">
              <PixelAvatar slug={item.authorSlug} size={12} />
              {item.authorName || item.authorSlug.toUpperCase()}
              {' • '}
              <a href={`#/wiki/commit/${item.commitSha}`}>{item.commitSha.slice(0, 7)}</a>
              {' • '}
              {formatShortDate(item.date)}
            </span>
          </li>
        ))}
      </ol>
    </section>
  )
}

function formatShortDate(iso: string): string {
  try {
    const d = new Date(iso)
    if (Number.isNaN(d.getTime())) return iso
    return d.toISOString().slice(0, 10)
  } catch {
    return iso
  }
}
