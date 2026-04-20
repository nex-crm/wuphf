import { useMemo } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { PluggableList } from 'unified'
import { wikiLinkRemarkPlugin } from '../../lib/wikilink'

/**
 * Notebook markdown renderer. Reuses the wiki's remark-gfm + remark-wiki-link
 * pipeline so wikilinks cross-navigate to `/wiki/{path}`, styled per the
 * notebook design tokens (dashed ink-blue underline).
 */

interface EntryBodyProps {
  markdown: string
  /** Resolve `[[slug]]` → true if the wiki has an article at that path. */
  wikiExists?: (slug: string) => boolean
  /** Called when a wikilink is clicked; receives the resolved wiki path. */
  onWikiNavigate?: (slug: string) => void
}

export default function EntryBody({
  markdown,
  wikiExists,
  onWikiNavigate,
}: EntryBodyProps) {
  const remarkPlugins: PluggableList = useMemo(() => {
    const resolver = wikiExists ?? (() => true)
    return [remarkGfm, wikiLinkRemarkPlugin(resolver)]
  }, [wikiExists])

  return (
    <div className="nb-body" data-testid="nb-entry-body">
      <ReactMarkdown
        remarkPlugins={remarkPlugins}
        components={{
          a: ({ node, ...props }) => {
            const p = props as Record<string, unknown>
            const isWikilink = p['data-wikilink'] === 'true'
            if (isWikilink) {
              const slug = p['data-slug'] as string | undefined
              const broken = p['data-broken'] === 'true'
              return (
                <a
                  {...props}
                  className={`nb-wikilink${broken ? ' is-broken' : ''}`}
                  onClick={(e) => {
                    if (slug && onWikiNavigate) {
                      e.preventDefault()
                      onWikiNavigate(slug)
                    }
                  }}
                />
              )
            }
            return <a {...props} />
          },
          blockquote: ({ node, children, ...props }) => {
            // Desktop CSS renders blockquotes as right-gutter marginalia
            // via `.nb-body blockquote`. On mobile (<768px) the same class
            // collapses to an inline callout. Spec-compliant positioning.
            return (
              <blockquote {...props} className="nb-margin">
                {children}
              </blockquote>
            )
          },
        }}
      >
        {markdown}
      </ReactMarkdown>
    </div>
  )
}
