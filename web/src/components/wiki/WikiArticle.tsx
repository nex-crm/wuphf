import { useEffect, useMemo, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeSlug from 'rehype-slug'
import rehypeAutolinkHeadings from 'rehype-autolink-headings'
import type { PluggableList } from 'unified'
import ArticleStatusBanner from './ArticleStatusBanner'
import EntityBriefBar from './EntityBriefBar'
import FactsOnFile from './FactsOnFile'
import ImageEmbed from './ImageEmbed'
import PlaybookExecutionLog from './PlaybookExecutionLog'
import PlaybookSkillBadge from './PlaybookSkillBadge'
import HatBar, { type HatBarTab } from './HatBar'
import ArticleTitle from './ArticleTitle'
import Byline from './Byline'
import Hatnote from './Hatnote'
import SeeAlso from './SeeAlso'
import Sources from './Sources'
import CategoriesFooter from './CategoriesFooter'
import PageFooter from './PageFooter'
import TocBox, { type TocEntry } from './TocBox'
import PageStatsPanel from './PageStatsPanel'
import CiteThisPagePanel from './CiteThisPagePanel'
import ReferencedBy from './ReferencedBy'
import {
  fetchArticle,
  fetchHistory,
  subscribeEditLog,
  type WikiArticle as WikiArticleT,
  type WikiCatalogEntry,
  type WikiHistoryCommit,
} from '../../api/wiki'
import type { SourceItem } from './Sources'
import { wikiLinkRemarkPlugin } from '../../lib/wikilink'
import { formatAgentName } from '../../lib/agentName'
import type { EntityKind } from '../../api/entity'
import { detectPlaybook } from '../../api/playbook'

// Real backend paths look like `team/people/nazz.md`. Mock/dev paths may
// drop the `team/` prefix or the `.md` suffix. Accept both so the entity
// surface lights up in demos without forcing every caller to normalize.
const ENTITY_PATH_RE = /^(?:team\/)?(people|companies|customers)\/([a-z0-9][a-z0-9-]*)(?:\.md)?$/

function detectEntity(path: string): { kind: EntityKind; slug: string } | null {
  const m = path.match(ENTITY_PATH_RE)
  if (!m) return null
  return { kind: m[1] as EntityKind, slug: m[2] }
}

interface WikiArticleProps {
  path: string
  catalog: WikiCatalogEntry[]
  onNavigate: (path: string) => void
}

export default function WikiArticle({ path, catalog, onNavigate }: WikiArticleProps) {
  const [article, setArticle] = useState<WikiArticleT | null>(null)
  const [tab, setTab] = useState<HatBarTab>('article')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [historyCommits, setHistoryCommits] = useState<WikiHistoryCommit[] | null>(null)
  const [historyLoading, setHistoryLoading] = useState(true)
  const [historyError, setHistoryError] = useState(false)
  const [liveAgent, setLiveAgent] = useState<string | null>(null)
  const [refreshNonce, setRefreshNonce] = useState(0)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)
    fetchArticle(path)
      .then((a) => {
        if (cancelled) return
        setArticle(a)
      })
      .catch((err: unknown) => {
        if (cancelled) return
        setError(err instanceof Error ? err.message : 'Failed to load article')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [path, refreshNonce])

  useEffect(() => {
    let cancelled = false
    setHistoryCommits(null)
    setHistoryLoading(true)
    setHistoryError(false)
    fetchHistory(path)
      .then((res) => {
        if (cancelled) return
        setHistoryCommits(res.commits ?? [])
      })
      .catch(() => {
        if (cancelled) return
        // Graceful degradation: missing history should not block the article read.
        setHistoryError(true)
        setHistoryCommits([])
      })
      .finally(() => {
        if (!cancelled) setHistoryLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [path, refreshNonce])

  useEffect(() => {
    setLiveAgent(null)
    let clearTimer: ReturnType<typeof setTimeout> | null = null
    const unsubscribe = subscribeEditLog((entry) => {
      if (entry.article_path !== path) return
      setLiveAgent(entry.who)
      if (clearTimer) clearTimeout(clearTimer)
      clearTimer = setTimeout(() => setLiveAgent(null), 10_000)
    })
    return () => {
      if (clearTimer) clearTimeout(clearTimer)
      unsubscribe()
    }
  }, [path])

  const sourceItems = useMemo<SourceItem[]>(() => {
    if (!historyCommits) return []
    return historyCommits.map((c) => ({
      commitSha: c.sha,
      authorSlug: c.author_slug,
      authorName: formatAgentName(c.author_slug),
      msg: c.msg,
      date: c.date,
    }))
  }, [historyCommits])

  const catalogSlugs = useMemo(() => new Set(catalog.map((c) => c.path)), [catalog])
  const resolver = useMemo(
    () => (slug: string) => catalogSlugs.has(slug),
    [catalogSlugs],
  )

  const remarkPlugins: PluggableList = useMemo(
    () => [remarkGfm, wikiLinkRemarkPlugin(resolver)],
    [resolver],
  )
  const rehypePlugins: PluggableList = useMemo(
    () => [rehypeSlug, [rehypeAutolinkHeadings, { behavior: 'wrap' }]],
    [],
  )

  if (loading) return <div className="wk-loading">Loading article…</div>
  if (error) return <div className="wk-error">Error: {error}</div>
  if (!article) return <div className="wk-error">Article not found.</div>

  const toc = buildTocFromMarkdown(article.content)
  const entity = detectEntity(article.path)
  const playbook = detectPlaybook(article.path)
  const breadcrumbSegments = article.path.split('/').filter(Boolean)
  const context = breadcrumbSegments[0] || ''
  const byline = (
    <Byline
      authorSlug={article.last_edited_by}
      authorName={formatAgentName(article.last_edited_by)}
      lastEditedTs={article.last_edited_ts}
      revisions={article.revisions}
    />
  )

  return (
    <>
      <main className="wk-article-col">
        {liveAgent && (
          <ArticleStatusBanner
            message={`${formatAgentName(liveAgent)} is editing this article right now.`}
            liveAgent={liveAgent}
            revisions={article.revisions}
            contributors={article.contributors.length}
            wordCount={article.word_count}
          />
        )}
        {entity && (
          <EntityBriefBar
            kind={entity.kind}
            slug={entity.slug}
            onSynthesized={() => setRefreshNonce((n) => n + 1)}
          />
        )}
        {playbook && <PlaybookSkillBadge slug={playbook.slug} />}
        <HatBar
          active={tab}
          onChange={setTab}
          rightRail={context ? [context] : undefined}
        />
        <div className="wk-breadcrumb">
          <a
            href="#/wiki"
            onClick={(e) => {
              e.preventDefault()
              onNavigate('')
            }}
          >
            Team Wiki
          </a>
          {breadcrumbSegments.map((seg, i) => (
            <span key={`${seg}-${i}`} style={{ display: 'contents' }}>
              <span className="sep">›</span>
              {i < breadcrumbSegments.length - 1 ? (
                <a href="#">{seg}</a>
              ) : (
                <span>{article.title}</span>
              )}
            </span>
          ))}
        </div>
        <ArticleTitle title={article.title} />
        {byline}
        <Hatnote>
          This article is auto-generated from team activity. See the commit history for the full trail.
        </Hatnote>
        {tab === 'article' && (
          <div className="wk-article-body" data-testid="wk-article-body">
            <ReactMarkdown
              remarkPlugins={remarkPlugins}
              rehypePlugins={rehypePlugins}
              components={{
                a: ({ node, ...props }) => {
                  const isWikilink = (props as Record<string, unknown>)['data-wikilink'] === 'true'
                  if (isWikilink) {
                    const slug = (props as Record<string, unknown>)['data-slug'] as string | undefined
                    return (
                      <a
                        {...props}
                        onClick={(e) => {
                          if (slug) {
                            e.preventDefault()
                            onNavigate(slug)
                          }
                        }}
                      />
                    )
                  }
                  return <a {...props} />
                },
                // Agents embed images as standard markdown (`![alt](https://...)`).
                // Route them through ImageEmbed so editorial articles get
                // lazy-loading, lightbox, and no-referrer leaks to the source host.
                img: ({ src, alt, width, height }) => {
                  if (!src) return null
                  const w = typeof width === 'string' ? parseInt(width, 10) || undefined : width
                  const h = typeof height === 'string' ? parseInt(height, 10) || undefined : height
                  return <ImageEmbed src={String(src)} alt={alt} width={w} height={h} />
                },
              }}
            >
              {article.content}
            </ReactMarkdown>
          </div>
        )}
        {tab === 'raw' && (
          <pre
            style={{
              fontFamily: 'var(--wk-mono)',
              background: 'var(--wk-code-bg)',
              padding: 16,
              border: '1px solid var(--wk-border)',
              overflowX: 'auto',
              fontSize: 13,
              lineHeight: 1.5,
              whiteSpace: 'pre-wrap',
            }}
          >
            {article.content}
          </pre>
        )}
        {tab === 'history' && (
          <div className="wk-loading">
            History view streams from <code>git log</code>. Wiring pending Lane A.
          </div>
        )}
        {entity && tab === 'article' && (
          <FactsOnFile kind={entity.kind} slug={entity.slug} />
        )}
        {playbook && tab === 'article' && (
          <PlaybookExecutionLog slug={playbook.slug} />
        )}
        <SeeAlso
          items={article.backlinks.map((b) => ({ slug: b.path, display: b.title }))}
          onNavigate={onNavigate}
        />
        {historyError ? null : (
          <Sources items={sourceItems} loading={historyLoading} />
        )}
        <CategoriesFooter tags={article.categories} />
        <PageFooter
          lastEditedBy={formatAgentName(article.last_edited_by)}
          lastEditedTs={article.last_edited_ts}
          articlePath={article.path}
        />
      </main>
      <aside className="wk-right-sidebar">
        <TocBox entries={toc} />
        <PageStatsPanel
          revisions={article.revisions}
          contributors={article.contributors.length}
          wordCount={article.word_count}
          created={article.last_edited_ts}
          lastEdit={article.last_edited_ts}
        />
        <CiteThisPagePanel slug={article.path} />
        <ReferencedBy backlinks={article.backlinks} onNavigate={onNavigate} />
      </aside>
    </>
  )
}

function buildTocFromMarkdown(md: string): TocEntry[] {
  const out: TocEntry[] = []
  const lines = md.split('\n')
  let h2Count = 0
  let h3Count = 0
  const h2Re = /^##\s+(.+)$/
  const h3Re = /^###\s+(.+)$/
  for (const line of lines) {
    const h3 = line.match(h3Re)
    if (h3) {
      h3Count += 1
      const title = h3[1].trim()
      out.push({
        level: 2,
        num: `${h2Count}.${h3Count}`,
        anchor: slugify(title),
        title,
      })
      continue
    }
    const h2 = line.match(h2Re)
    if (h2) {
      h2Count += 1
      h3Count = 0
      const title = h2[1].trim()
      out.push({
        level: 1,
        num: String(h2Count),
        anchor: slugify(title),
        title,
      })
    }
  }
  return out
}

function slugify(s: string): string {
  return s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
}
