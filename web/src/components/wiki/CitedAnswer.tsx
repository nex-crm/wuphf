import { useEffect, useMemo, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import type { PluggableList } from 'unified'
import Hatnote from './Hatnote'
import { get } from '../../api/client'
import { buildRemarkPlugins, buildRehypePlugins, buildMarkdownComponents } from '../../lib/wikiMarkdownConfig'

// QueryAnswer mirrors the JSON shape returned by GET /wiki/lookup.
export interface QuerySource {
  kind: string
  slug_or_id: string
  title: string
  excerpt: string
  valid_from?: string
  valid_until?: string
  staleness: number
  source_path?: string
}

export interface QueryAnswer {
  query_class: string
  answer_markdown: string
  sources_cited: number[]
  sources: QuerySource[]
  confidence: number
  coverage: string // 'complete' | 'partial' | 'none'
  notes?: string
  latency_ms: number
}

export interface CitedAnswerProps {
  /** The natural-language question to answer. */
  query: string
}

/**
 * CitedAnswer renders the /lookup cited-answer loop result as a wiki-shaped
 * surface per DESIGN-WIKI.md:
 *
 *   - Hatnote: italic context note (scope + coverage)
 *   - Body: answer_markdown with <sup>[n]</sup> inline citations
 *   - Sources: numbered list of cited sources only
 *   - PageFooter: latency + source count action-links style
 *
 * Anti-pattern 12 (DESIGN-WIKI.md): NO card, NO callout, NO alert block.
 *
 * States:
 *   - Loading: role="status" aria-busy="true" skeleton
 *   - Error: hatnote-styled error note
 *   - Out-of-scope: hatnote note + no Sources block (coverage=none, class=general)
 *   - Answer: full composition
 */
export default function CitedAnswer({ query }: CitedAnswerProps) {
  const [answer, setAnswer] = useState<QueryAnswer | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const remarkPlugins = useMemo<PluggableList>(() => buildRemarkPlugins([]), [])
  const rehypePlugins = useMemo<PluggableList>(() => buildRehypePlugins(), [])
  const markdownComponents = useMemo(() => buildMarkdownComponents({}), [])

  useEffect(() => {
    if (!query.trim()) {
      setLoading(false)
      return
    }
    setLoading(true)
    setAnswer(null)
    setError(null)

    get<QueryAnswer>('/wiki/lookup', { q: query })
      .then((ans) => {
        setAnswer(ans)
        setLoading(false)
      })
      .catch((e: Error) => {
        setError(e.message)
        setLoading(false)
      })
  }, [query])

  // Loading skeleton
  if (loading) {
    return (
      <div
        className="wk-cited-answer wk-cited-answer--loading"
        role="status"
        aria-busy="true"
        aria-label="Loading cited answer…"
      >
        <div className="wk-hatnote wk-skeleton" aria-hidden="true" />
        <div className="wk-skeleton wk-skeleton--body" aria-hidden="true" />
        <div className="wk-skeleton wk-skeleton--body wk-skeleton--short" aria-hidden="true" />
      </div>
    )
  }

  // Error state — hatnote-styled
  if (error) {
    return (
      <div className="wk-cited-answer wk-cited-answer--error">
        <Hatnote>
          <em>Wiki lookup failed:</em> {error}
        </Hatnote>
      </div>
    )
  }

  if (!answer) return null

  const isOutOfScope =
    answer.query_class === 'general' || answer.coverage === 'none'

  const citedSources = answer.sources.filter((_, i) =>
    answer.sources_cited.includes(i + 1),
  )

  const mostRecentValidFrom = answer.sources.reduce<string>((best, s) => {
    const vf = (s.valid_from || '').trim()
    if (!vf) return best
    return best === '' || vf > best ? vf : best
  }, '')

  return (
    <article className="wk-cited-answer">
      {/* Hatnote — always present, coverage context */}
      <Hatnote>
        <em>From the wiki</em>
        {answer.coverage === 'partial' && ' (partial match)'}
        {answer.coverage === 'none' && ' (no match)'}
      </Hatnote>

      {/* Body — only when there is an answer */}
      {answer.answer_markdown && (
        <div className="wk-article-body" data-testid="wk-cited-answer-body">
          <ReactMarkdown
            remarkPlugins={remarkPlugins}
            rehypePlugins={rehypePlugins}
            components={markdownComponents}
          >
            {answer.answer_markdown}
          </ReactMarkdown>
        </div>
      )}

      {/* Out-of-scope: no sources block */}
      {isOutOfScope && (
        <p className="wk-cited-answer-oos">
          I can help with questions about people, companies, and activities in your workspace.
        </p>
      )}

      {/* Sources — only cited entries, only when not out-of-scope */}
      {!isOutOfScope && citedSources.length > 0 && (
        <section
          className="wk-sources"
          aria-labelledby="ca-sources-heading"
        >
          <h2 id="ca-sources-heading">Sources</h2>
          <ol>
            {answer.sources.map((src, i) => {
              if (!answer.sources_cited.includes(i + 1)) return null
              const excerpt = src.excerpt.length > 120
                ? src.excerpt.slice(0, 120) + '…'
                : src.excerpt
              return (
                <li key={src.slug_or_id || `src-${i}`} id={`ca-s${i + 1}`}>
                  <span className="wk-commit-msg">{excerpt}</span>
                  {src.title && (
                    <span className="wk-agent">{src.title}</span>
                  )}
                  {src.valid_from && (
                    <span className="wk-dim"> · {src.valid_from.slice(0, 10)}</span>
                  )}
                </li>
              )
            })}
          </ol>
        </section>
      )}

      {/* PageFooter — action-links style, no article git metadata */}
      <div className="wk-page-footer">
        <div className="wk-actions">
          {mostRecentValidFrom && (
            <span>Last updated: {mostRecentValidFrom}</span>
          )}
          <span aria-label="Answer latency">
            {answer.latency_ms}ms
          </span>
          {answer.sources.length > 0 && (
            <span>
              {answer.sources.length} {answer.sources.length === 1 ? 'source' : 'sources'}
            </span>
          )}
        </div>
      </div>
    </article>
  )
}
