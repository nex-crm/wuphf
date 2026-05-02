// biome-ignore-all lint/a11y/useValidAnchor: Anchor is intercepted by the app router or markdown renderer while preserving href fallback behavior.
import { useEffect, useMemo, useState } from "react";
import ReactMarkdown from "react-markdown";
import type { PluggableList } from "unified";

import type { EntityKind } from "../../api/entity";
import { detectPlaybook } from "../../api/playbook";
import {
  compressArticle,
  fetchArticle,
  fetchHistory,
  fetchHumans,
  type HumanIdentity,
  subscribeEditLog,
  type WikiArticle as WikiArticleT,
  type WikiCatalogEntry,
  type WikiHistoryCommit,
} from "../../api/wiki";
import { formatAgentName } from "../../lib/agentName";
import { keyedByOccurrence } from "../../lib/reactKeys";
import {
  buildMarkdownComponents,
  buildRehypePlugins,
  buildRemarkPlugins,
} from "../../lib/wikiMarkdownConfig";
import ArticleStatusBanner from "./ArticleStatusBanner";
import ArticleTitle from "./ArticleTitle";
import Byline from "./Byline";
import CategoriesFooter from "./CategoriesFooter";
import CiteThisPagePanel from "./CiteThisPagePanel";
import EntityBriefBar from "./EntityBriefBar";
import EntityRelatedPanel from "./EntityRelatedPanel";
import FactsOnFile from "./FactsOnFile";
import HatBar, { type HatBarTab } from "./HatBar";
import Hatnote from "./Hatnote";
import PageFooter from "./PageFooter";
import PageStatsPanel from "./PageStatsPanel";
import PlaybookExecutionLog from "./PlaybookExecutionLog";
import PlaybookSkillBadge from "./PlaybookSkillBadge";
import ReferencedBy from "./ReferencedBy";
import SeeAlso from "./SeeAlso";
import type { SourceItem } from "./Sources";
import Sources from "./Sources";
import TeamLearningPanel from "./TeamLearningPanel";
import TocBox, { type TocEntry } from "./TocBox";
import WikiEditor from "./WikiEditor";

const STALENESS_STALE_DAYS = 30;
const STALENESS_AGING_DAYS = 7;

// StalenessIndicator shows a small badge when an article has not been accessed
// by anyone (human or agent) in a while. "Agents only" signals an article
// actively used for context but never opened by a human.
function StalenessIndicator({ article }: { article: WikiArticleT }) {
  const days_unread = article.days_unread ?? 0;
  const human_read_count = article.human_read_count ?? 0;
  const agent_read_count = article.agent_read_count ?? 0;
  if (agent_read_count > 0 && human_read_count === 0) {
    return (
      <span
        className="wk-staleness-badge wk-staleness-agents-only"
        role="status"
        aria-label="Article accessed by agents only — never opened by a human"
      >
        agents only
      </span>
    );
  }
  if (days_unread >= STALENESS_STALE_DAYS) {
    return (
      <span
        className="wk-staleness-badge wk-staleness-stale"
        role="status"
        aria-label={`Article not read in ${days_unread} days`}
      >
        unread 30d+
      </span>
    );
  }
  if (days_unread >= STALENESS_AGING_DAYS) {
    return (
      <span
        className="wk-staleness-badge wk-staleness-aging"
        role="status"
        aria-label={`Article not read in ${days_unread} days`}
      >
        unread 7d+
      </span>
    );
  }
  return null;
}

// CompressButton triggers POST /wiki/compress and surfaces the queued vs.
// already-compressing states inline. The button is shown when an article is
// long enough to benefit from compression (word_count > MIN_COMPRESS_WORDS).
const MIN_COMPRESS_WORDS = 200;

interface CompressButtonProps {
  path: string;
  wordCount: number;
}

function CompressButton({ path, wordCount }: CompressButtonProps) {
  const [status, setStatus] = useState<
    "idle" | "pending" | "queued" | "in_flight" | "error"
  >("idle");
  const [message, setMessage] = useState<string>("");

  if (wordCount <= MIN_COMPRESS_WORDS) return null;

  async function handleClick() {
    setStatus("pending");
    setMessage("");
    try {
      const res = await compressArticle(path);
      if (res.queued) {
        setStatus("queued");
        setMessage("Compressing article…");
      } else if (res.in_flight) {
        setStatus("in_flight");
        setMessage("Already compressing, check back soon.");
      } else {
        setStatus("idle");
        setMessage("");
      }
    } catch (err: unknown) {
      setStatus("error");
      setMessage(err instanceof Error ? err.message : "Compress failed");
    }
  }

  return (
    <span className="wk-compress-control">
      <button
        type="button"
        className="wk-compress-btn"
        onClick={() => {
          void handleClick();
        }}
        disabled={status === "pending"}
        aria-label={`Compress this article (${wordCount} words)`}
      >
        {status === "pending" ? "Compressing…" : "Compress"}
      </button>
      {message ? (
        <span
          className={`wk-compress-toast wk-compress-${status}`}
          role="status"
        >
          {message}
        </span>
      ) : null}
    </span>
  );
}

// Real backend paths look like `team/people/nazz.md`. Mock/dev paths may
// drop the `team/` prefix or the `.md` suffix. Accept both so the entity
// surface lights up in demos without forcing every caller to normalize.
const ENTITY_PATH_RE =
  /^(?:team\/)?(people|companies|customers)\/([a-z0-9][a-z0-9-]*)(?:\.md)?$/;

function detectEntity(path: string): { kind: EntityKind; slug: string } | null {
  const m = path.match(ENTITY_PATH_RE);
  if (!m) return null;
  return { kind: m[1] as EntityKind, slug: m[2] };
}

interface WikiArticleProps {
  path: string;
  catalog: WikiCatalogEntry[];
  onNavigate: (path: string) => void;
  /**
   * Bumped by Pam (now hoisted to the Wiki shell) when an action completes,
   * so the article + history refetch without a navigation. Treated as an
   * additive trigger on top of the local refreshNonce used by inline edits.
   */
  externalRefreshNonce?: number;
}

type MarkdownComponents = ReturnType<typeof buildMarkdownComponents>;
type DetectedEntity = { kind: EntityKind; slug: string };
type DetectedPlaybook = NonNullable<ReturnType<typeof detectPlaybook>>;

export default function WikiArticle({
  path,
  catalog,
  onNavigate,
  externalRefreshNonce = 0,
}: WikiArticleProps) {
  const [article, setArticle] = useState<WikiArticleT | null>(null);
  const [tab, setTab] = useState<HatBarTab>("article");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [historyCommits, setHistoryCommits] = useState<
    WikiHistoryCommit[] | null
  >(null);
  const [historyLoading, setHistoryLoading] = useState(true);
  const [historyError, setHistoryError] = useState(false);
  const [liveAgent, setLiveAgent] = useState<string | null>(null);
  const [refreshNonce, setRefreshNonce] = useState(0);
  const [humans, setHumans] = useState<HumanIdentity[]>([]);

  // Fetch the human registry once per mount. The list is small (a handful
  // of team members) and changes rarely, so we skip refetching on every
  // path change. Failure falls through to an empty list — Byline gracefully
  // shows the agent path when no human identity matches.
  useEffect(() => {
    let cancelled = false;
    fetchHumans()
      .then((list) => {
        if (!cancelled) setHumans(list);
      })
      .catch(() => {
        if (!cancelled) setHumans([]);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    // These nonce values are explicit refetch triggers. The effect body only
    // needs their change notification, not their numeric value.
    void externalRefreshNonce;
    void refreshNonce;
    setLoading(true);
    setError(null);
    fetchArticle(path)
      .then((a) => {
        if (cancelled) return;
        setArticle(a);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : "Failed to load article");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [path, externalRefreshNonce, refreshNonce]);

  useEffect(() => {
    let cancelled = false;
    // These nonce values are explicit refetch triggers. The effect body only
    // needs their change notification, not their numeric value.
    void externalRefreshNonce;
    void refreshNonce;
    setHistoryCommits(null);
    setHistoryLoading(true);
    setHistoryError(false);
    fetchHistory(path)
      .then((res) => {
        if (cancelled) return;
        setHistoryCommits(res.commits ?? []);
      })
      .catch(() => {
        if (cancelled) return;
        // Graceful degradation: missing history should not block the article read.
        setHistoryError(true);
        setHistoryCommits([]);
      })
      .finally(() => {
        if (!cancelled) setHistoryLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [path, externalRefreshNonce, refreshNonce]);

  useEffect(() => {
    setLiveAgent(null);
    let clearTimer: ReturnType<typeof setTimeout> | null = null;
    const unsubscribe = subscribeEditLog((entry) => {
      if (entry.article_path !== path) return;
      setLiveAgent(entry.who);
      if (clearTimer) clearTimeout(clearTimer);
      clearTimer = setTimeout(() => setLiveAgent(null), 10_000);
    });
    return () => {
      if (clearTimer) clearTimeout(clearTimer);
      unsubscribe();
    };
  }, [path]);

  const sourceItems = useMemo<SourceItem[]>(() => {
    if (!historyCommits) return [];
    return historyCommits.map((c) => ({
      commitSha: c.sha,
      authorSlug: c.author_slug,
      authorName: formatAgentName(c.author_slug),
      msg: c.msg,
      date: c.date,
    }));
  }, [historyCommits]);

  const catalogSlugs = useMemo(
    () => new Set(catalog.map((c) => c.path)),
    [catalog],
  );
  const resolver = useMemo(
    () => (slug: string) => catalogSlugs.has(slug),
    [catalogSlugs],
  );

  const remarkPlugins: PluggableList = useMemo(
    () => buildRemarkPlugins(resolver),
    [resolver],
  );
  const rehypePlugins: PluggableList = useMemo(() => buildRehypePlugins(), []);
  const markdownComponents = useMemo(
    () => buildMarkdownComponents({ resolver, onNavigate }),
    [resolver, onNavigate],
  );

  if (loading) return <div className="wk-loading">Loading article…</div>;
  if (error) return <div className="wk-error">Error: {error}</div>;
  if (!article) return <div className="wk-error">Article not found.</div>;

  const toc = buildTocFromMarkdown(article.content);
  const entity = detectEntity(article.path);
  const playbook = detectPlaybook(article.path);
  const breadcrumbSegments = article.path.split("/").filter(Boolean);
  const context = breadcrumbSegments[0] || "";
  const byline = (
    <Byline
      authorSlug={article.last_edited_by}
      authorName={formatAgentName(article.last_edited_by)}
      lastEditedTs={article.last_edited_ts}
      revisions={article.revisions}
      humans={humans}
    />
  );
  const handleEditorSaved = (newSha: string) => {
    // Refetch after every save — covers both happy path and the
    // conflict-then-reload path, which passes the server current_sha back.
    void newSha;
    setRefreshNonce((n) => n + 1);
    setTab("article");
  };
  const handleEditorCancel = () => setTab("article");

  return (
    <>
      <main className="wk-article-col">
        <LiveEditingBanner liveAgent={liveAgent} article={article} />
        <ArticleSetupPanels
          entity={entity}
          playbook={playbook}
          onEntitySynthesized={() => setRefreshNonce((n) => n + 1)}
        />
        <HatBar
          active={tab}
          onChange={setTab}
          rightRail={context ? [context] : undefined}
        />
        <ArticleBreadcrumb
          article={article}
          segments={breadcrumbSegments}
          onNavigate={onNavigate}
        />
        <ArticleTitle title={article.title} />
        {byline}
        <ArticleBadges article={article} />
        <Hatnote>
          This article is auto-generated from team activity. See the commit
          history for the full trail.
        </Hatnote>
        <ArticleTabPanels
          tab={tab}
          article={article}
          catalog={catalog}
          remarkPlugins={remarkPlugins}
          rehypePlugins={rehypePlugins}
          markdownComponents={markdownComponents}
          onEditorSaved={handleEditorSaved}
          onEditorCancel={handleEditorCancel}
        />
        <ArticleRelatedPanels
          visible={tab === "article"}
          entity={entity}
          playbook={playbook}
        />
        <SeeAlso
          items={article.backlinks.map((b) => ({
            slug: b.path,
            display: b.title,
          }))}
          onNavigate={onNavigate}
        />
        <SourcesPanel
          historyError={historyError}
          sourceItems={sourceItems}
          historyLoading={historyLoading}
        />
        <CategoriesFooter tags={article.categories} />
        <PageFooter
          lastEditedBy={formatAgentName(article.last_edited_by)}
          lastEditedTs={article.last_edited_ts}
          articlePath={article.path}
        />
      </main>
      <ArticleRightSidebar
        article={article}
        toc={toc}
        onNavigate={onNavigate}
      />
    </>
  );
}

function LiveEditingBanner({
  liveAgent,
  article,
}: {
  liveAgent: string | null;
  article: WikiArticleT;
}) {
  if (!liveAgent) return null;
  return (
    <ArticleStatusBanner
      message={`${formatAgentName(liveAgent)} is editing this article right now.`}
      liveAgent={liveAgent}
      revisions={article.revisions}
      contributors={article.contributors.length}
      wordCount={article.word_count}
    />
  );
}

function ArticleSetupPanels({
  entity,
  playbook,
  onEntitySynthesized,
}: {
  entity: DetectedEntity | null;
  playbook: DetectedPlaybook | null;
  onEntitySynthesized: () => void;
}) {
  if (!(entity || playbook)) return null;
  return (
    <>
      {entity ? (
        <EntityBriefBar
          kind={entity.kind}
          slug={entity.slug}
          onSynthesized={onEntitySynthesized}
        />
      ) : null}
      {playbook ? <PlaybookSkillBadge slug={playbook.slug} /> : null}
    </>
  );
}

function ArticleBreadcrumb({
  article,
  segments,
  onNavigate,
}: {
  article: WikiArticleT;
  segments: string[];
  onNavigate: (path: string) => void;
}) {
  return (
    <div className="wk-breadcrumb">
      <a
        href="#/wiki"
        onClick={(e) => {
          e.preventDefault();
          onNavigate("");
        }}
      >
        Team Wiki
      </a>
      {keyedByOccurrence(segments, (seg) => seg).map(
        ({ key, value: seg, index: i }) => (
          <span key={key} style={{ display: "contents" }}>
            <span className="sep">›</span>
            {i < segments.length - 1 ? (
              <a href="#">{seg}</a>
            ) : (
              <span>{article.title}</span>
            )}
          </span>
        ),
      )}
    </div>
  );
}

function ArticleBadges({ article }: { article: WikiArticleT }) {
  return (
    <>
      <StalenessIndicator article={article} />
      <CompressButton path={article.path} wordCount={article.word_count} />
      <SynthesisQueuedBadge queued={article.synthesis_queued} />
    </>
  );
}

function SynthesisQueuedBadge({ queued }: { queued?: boolean }) {
  if (queued !== true) return null;
  return (
    <span
      className="wk-staleness-badge wk-synthesis-queued"
      role="status"
      aria-label="Brief is being generated from recent activity"
    >
      generating brief…
    </span>
  );
}

function ArticleTabPanels({
  tab,
  article,
  catalog,
  remarkPlugins,
  rehypePlugins,
  markdownComponents,
  onEditorSaved,
  onEditorCancel,
}: {
  tab: HatBarTab;
  article: WikiArticleT;
  catalog: WikiCatalogEntry[];
  remarkPlugins: PluggableList;
  rehypePlugins: PluggableList;
  markdownComponents: MarkdownComponents;
  onEditorSaved: (newSha: string) => void;
  onEditorCancel: () => void;
}) {
  switch (tab) {
    case "article":
      return (
        <div className="wk-article-body" data-testid="wk-article-body">
          <ReactMarkdown
            remarkPlugins={remarkPlugins}
            rehypePlugins={rehypePlugins}
            components={markdownComponents}
          >
            {article.content}
          </ReactMarkdown>
        </div>
      );
    case "edit":
      return (
        <WikiEditor
          path={article.path}
          initialContent={article.content}
          expectedSha={article.commit_sha ?? ""}
          serverLastEditedTs={article.last_edited_ts}
          catalog={catalog}
          onSaved={onEditorSaved}
          onCancel={onEditorCancel}
        />
      );
    case "raw":
      return (
        <pre
          style={{
            fontFamily: "var(--wk-mono)",
            background: "var(--wk-code-bg)",
            padding: 16,
            border: "1px solid var(--wk-border)",
            overflowX: "auto",
            fontSize: 13,
            lineHeight: 1.5,
            whiteSpace: "pre-wrap",
          }}
        >
          {article.content}
        </pre>
      );
    case "history":
      return (
        <div className="wk-loading">
          History view streams from <code>git log</code>. Wiring pending Lane A.
        </div>
      );
  }
  return null;
}

function ArticleRelatedPanels({
  visible,
  entity,
  playbook,
}: {
  visible: boolean;
  entity: DetectedEntity | null;
  playbook: DetectedPlaybook | null;
}) {
  if (!visible) return null;
  return (
    <>
      {entity ? <FactsOnFile kind={entity.kind} slug={entity.slug} /> : null}
      {entity ? (
        <EntityRelatedPanel kind={entity.kind} slug={entity.slug} />
      ) : null}
      {playbook ? <PlaybookExecutionLog slug={playbook.slug} /> : null}
      {playbook ? <TeamLearningPanel playbookSlug={playbook.slug} /> : null}
    </>
  );
}

function SourcesPanel({
  historyError,
  sourceItems,
  historyLoading,
}: {
  historyError: boolean;
  sourceItems: SourceItem[];
  historyLoading: boolean;
}) {
  if (historyError) return null;
  return <Sources items={sourceItems} loading={historyLoading} />;
}

function ArticleRightSidebar({
  article,
  toc,
  onNavigate,
}: {
  article: WikiArticleT;
  toc: TocEntry[];
  onNavigate: (path: string) => void;
}) {
  return (
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
  );
}

function buildTocFromMarkdown(md: string): TocEntry[] {
  const out: TocEntry[] = [];
  const lines = md.split("\n");
  let h2Count = 0;
  let h3Count = 0;
  const h2Re = /^##\s+(.+)$/;
  const h3Re = /^###\s+(.+)$/;
  for (const line of lines) {
    const h3 = line.match(h3Re);
    if (h3) {
      h3Count += 1;
      const title = h3[1].trim();
      out.push({
        level: 2,
        num: `${h2Count}.${h3Count}`,
        anchor: slugify(title),
        title,
      });
      continue;
    }
    const h2 = line.match(h2Re);
    if (h2) {
      h2Count += 1;
      h3Count = 0;
      const title = h2[1].trim();
      out.push({
        level: 1,
        num: String(h2Count),
        anchor: slugify(title),
        title,
      });
    }
  }
  return out;
}

function slugify(s: string): string {
  return s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}
