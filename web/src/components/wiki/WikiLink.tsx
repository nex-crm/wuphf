/** Internal wiki link: dashed-underline blue by default, red-with-marker if broken. */

interface WikiLinkProps {
  slug: string
  display?: string
  broken?: boolean
  onNavigate?: (slug: string) => void
}

export default function WikiLink({ slug, display, broken = false, onNavigate }: WikiLinkProps) {
  const label = display ?? slug
  const className = broken ? 'wk-wikilink wk-broken' : 'wk-wikilink'
  const href = `#/wiki/${encodeURI(slug)}`
  return (
    <a
      href={href}
      className={className}
      data-wikilink="true"
      data-broken={broken ? 'true' : 'false'}
      data-slug={slug}
      onClick={(e) => {
        if (onNavigate) {
          e.preventDefault()
          onNavigate(slug)
        }
      }}
    >
      {label}
    </a>
  )
}
