// biome-ignore-all lint/a11y/useValidAnchor: Anchor is intercepted by the app router or markdown renderer while preserving href fallback behavior.

import { useEffect, useMemo, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";

import type { EntityKind } from "../../api/entity";
import { detectPlaybook } from "../../api/playbook";
import {
  fetchWikiVisualArtifact,
  type RichArtifactDetail,
} from "../../api/richArtifacts";
import {
  compressArticle,
  deletePage,
  fetchArticle,
  fetchHistory,
  fetchHumans,
  type HumanIdentity,
  subscribeEditLog,
  type WikiArticle as WikiArticleT,
  type WikiCatalogEntry,
  type WikiHistoryCommit,
  type WikiMaintenanceAction,
} from "../../api/wiki";
import { useInlineArtifacts } from "../../hooks/useInlineArtifacts";
import { formatAgentName } from "../../lib/agentName";
import { keyedByOccurrence } from "../../lib/reactKeys";
import ArticleContents from "./ArticleContents";
import ArticleReadView from "./ArticleReadView";
import ArticleStatusBanner from "./ArticleStatusBanner";
import ArticleTitle from "./ArticleTitle";
import { makeWikilinkResolver } from "./articleContent";
import Byline from "./Byline";
import CategoriesFooter from "./CategoriesFooter";
import CiteThisPagePanel from "./CiteThisPagePanel";
import EntityBriefBar from "./EntityBriefBar";
import EntityRelatedPanel from "./EntityRelatedPanel";
import { useFocusTrap } from "./editor/inserts/useFocusTrap";
import FactsOnFile from "./FactsOnFile";
import HatBar, { type HatBarTab } from "./HatBar";
import { consumeMaintenanceTarget } from "./maintenanceTarget";
import { consumeOpenInEdit } from "./openInEditTarget";
import PageFooter from "./PageFooter";
import PageStatsPanel from "./PageStatsPanel";
import PlaybookExecutionLog from "./PlaybookExecutionLog";
import PlaybookSkillBadge from "./PlaybookSkillBadge";
import ReferencedBy from "./ReferencedBy";
import RequestAIChangeControl from "./RequestAIChangeControl";
import SeeAlso from "./SeeAlso";
import type { SourceItem } from "./Sources";
import Sources from "./Sources";
import TeamLearningPanel from "./TeamLearningPanel";
import type { TocEntry } from "./TocBox";
import { WIKI_TREE_QUERY_KEY } from "./tree/WikiTree";
import VersionHistory from "./VersionHistory";
import WikiEditor from "./WikiEditor";
import WikiMaintenanceAssistant from "./WikiMaintenanceAssistant";
import { categoryPath } from "./wikiPaths";

const STALENESS_STALE_DAYS = 30;
const STALENESS_AGING_DAYS = 7;

/**
 * If the article fetch has not resolved within this window we treat the
 * loader as stalled and surface a retry-able error state. Picked to be
 * comfortably longer than a healthy local broker response (~tens of ms)
 * while still short enough that a hung request stops looking like a
 * permanent "Loading article…" placeholder.
 */
export const WIKI_ARTICLE_FETCH_TIMEOUT_MS = 5_000;

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
  articlePath: string;
  wordCount: number;
}

function CompressButton({ articlePath, wordCount }: CompressButtonProps) {
  const [status, setStatus] = useState<
    "idle" | "pending" | "queued" | "in_flight" | "error"
  >("idle");
  const [message, setMessage] = useState<string>("");

  if (wordCount <= MIN_COMPRESS_WORDS) return null;

  async function handleClick() {
    setStatus("pending");
    setMessage("");
    try {
      const res = await compressArticle(articlePath);
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
        disabled={
          status === "pending" || status === "queued" || status === "in_flight"
        }
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

type DetectedEntity = { kind: EntityKind; slug: string };
type DetectedPlaybook = NonNullable<ReturnType<typeof detectPlaybook>>;

interface ArticleFetchState {
  article: WikiArticleT | null;
  loading: boolean;
  error: string | null;
  /**
   * True once the fetch has been outstanding longer than
   * WIKI_ARTICLE_FETCH_TIMEOUT_MS. The fetch itself is not aborted — if
   * the broker eventually answers we still take the response — but the
   * UI swaps the loading placeholder for a retry-able error state so
   * the user is never stuck on an indefinite "Loading article…" hang.
   */
  timedOut: boolean;
  retry: () => void;
}

/**
 * Drives the article-fetch lifecycle for the wiki right pane. Owns the
 * timeout that flips the placeholder into an error state and the
 * retry-nonce that re-runs the fetch when the user clicks Retry.
 */
function useArticleFetch(
  path: string,
  externalRefreshNonce: number,
  refreshNonce: number,
): ArticleFetchState {
  const [article, setArticle] = useState<WikiArticleT | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [timedOut, setTimedOut] = useState(false);
  const [retryNonce, setRetryNonce] = useState(0);
  useEffect(() => {
    let cancelled = false;
    void externalRefreshNonce;
    void refreshNonce;
    void retryNonce;
    setLoading(true);
    setError(null);
    setTimedOut(false);
    const timeoutId = globalThis.setTimeout(() => {
      if (cancelled) return;
      setTimedOut(true);
    }, WIKI_ARTICLE_FETCH_TIMEOUT_MS);
    fetchArticle(path)
      .then((a) => {
        if (cancelled) return;
        setArticle(a);
        setTimedOut(false);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : "Failed to load article");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
        globalThis.clearTimeout(timeoutId);
      });
    return () => {
      cancelled = true;
      globalThis.clearTimeout(timeoutId);
    };
  }, [path, externalRefreshNonce, refreshNonce, retryNonce]);
  return {
    article,
    loading,
    error,
    timedOut,
    retry: () => setRetryNonce((n) => n + 1),
  };
}

/**
 * Consume the tree's "open in edit" hand-off for `path` and flip the tab to the
 * editor when it matches. Guarded by a ref so the side-effecting sessionStorage
 * read fires exactly once per path even under React strict mode's intentional
 * double-invoke (same defense ArticleRightSidebar uses for the maintenance
 * target). Lives outside the component body so the main render stays lean.
 */
function useOpenInEditTab(
  path: string,
  setTab: (tab: HatBarTab) => void,
): void {
  const consumedRef = useRef<string | null>(null);
  useEffect(() => {
    if (consumedRef.current === path) return;
    consumedRef.current = path;
    if (consumeOpenInEdit(path)) setTab("edit");
  }, [path, setTab]);
}

export default function WikiArticle({
  path,
  catalog,
  onNavigate,
  externalRefreshNonce = 0,
}: WikiArticleProps) {
  const [tab, setTab] = useState<HatBarTab>("article");
  const [refreshNonce, setRefreshNonce] = useState(0);
  const {
    article,
    loading,
    error,
    timedOut,
    retry: handleRetry,
  } = useArticleFetch(path, externalRefreshNonce, refreshNonce);
  const [historyCommits, setHistoryCommits] = useState<
    WikiHistoryCommit[] | null
  >(null);
  const [historyLoading, setHistoryLoading] = useState(true);
  const [historyError, setHistoryError] = useState(false);
  const [liveAgent, setLiveAgent] = useState<string | null>(null);
  const [humans, setHumans] = useState<HumanIdentity[]>([]);
  const [visualArtifact, setVisualArtifact] =
    useState<RichArtifactDetail | null>(null);
  const visualPathRef = useRef<string | null>(null);
  const visualAutoOpenedPathRef = useRef<string | null>(null);
  // Artifacts referenced inline in the article body via standalone
  // `visual-artifact:<id>` marker lines. These are fetched by id and
  // embedded in document order, mirroring the notebook entry surface.
  const inlineArtifacts = useInlineArtifacts(article?.content ?? null);

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

  // The repo-root path of the loaded article (e.g. "team/people/nazz.md"). The
  // URL splat is the bare, group-relative form ("people/nazz"); the visual API
  // keys on the same canonical path /wiki/file and /wiki/catalog emit, so use
  // the server-resolved article.path — passing the splat 400s ("must be within
  // team/").
  const repoArticlePath = article?.path ?? null;
  useEffect(() => {
    let cancelled = false;
    void externalRefreshNonce;
    void refreshNonce;
    if (visualPathRef.current !== path) {
      visualPathRef.current = path;
      visualAutoOpenedPathRef.current = null;
      setTab("article");
      setVisualArtifact(null);
    }
    // Wait for the article fetch to resolve the canonical path before asking
    // for its visual view; there is nothing to look up until then.
    if (!repoArticlePath) return;
    fetchWikiVisualArtifact(repoArticlePath)
      .then((detail) => {
        if (cancelled) return;
        setVisualArtifact(detail);
      })
      .catch(() => {
        if (cancelled) return;
        setVisualArtifact(null);
      });
    return () => {
      cancelled = true;
    };
  }, [path, repoArticlePath, externalRefreshNonce, refreshNonce]);

  // A page created via the tree's "New page" flow parks an "open in edit"
  // intent (see openInEditTarget.ts). This pops it after the visual-artifact
  // effect's `setTab("article")` reset above, so a just-created page lands
  // straight in the WYSIWYG editor with no flash of read view.
  useOpenInEditTab(path, setTab);

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
      // Refetch the article body when an out-of-band wiki:write lands for
      // the currently open article (e.g. background synthesis or a staged
      // demo write). Without this the live editor's banner updates but the
      // rendered body stays stale.
      setRefreshNonce((n) => n + 1);
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

  const resolver = useMemo(
    () => makeWikilinkResolver(catalog.map((c) => c.path)),
    [catalog],
  );

  if (loading && !timedOut) {
    return (
      <div className="wk-loading" role="status" aria-busy="true">
        Loading article…
      </div>
    );
  }
  if (loading && timedOut) {
    return (
      <ArticleErrorState
        message="Still waiting on the broker. The request has not responded in 5 seconds."
        onRetry={handleRetry}
      />
    );
  }
  if (error) {
    return (
      <ArticleErrorState message={`Error: ${error}`} onRetry={handleRetry} />
    );
  }
  if (!article) {
    return (
      <ArticleErrorState message="Article not found." onRetry={handleRetry} />
    );
  }

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
  const handleVersionRestored = (newSha: string) => {
    // A restore writes a fresh forward commit; bump the same nonce the editor
    // uses so the article body + history + sources all refetch, then drop the
    // human back on the rendered article showing the restored content.
    void newSha;
    setRefreshNonce((n) => n + 1);
    setTab("article");
  };

  // Full-page editing surface. In Edit mode we drop the reader chrome (title,
  // breadcrumb, hatnote, related rails) AND the right sidebar so the editor
  // takes over the whole content area — the immersive, full-page writing
  // experience of the reference app. The column spans the article + right-rail
  // grid tracks (see `.wk-article-col--editing`) and the editor fills its
  // height. The HatBar stays so the writer can switch back to the article view.
  // Category line at the bottom — derived from the article's path kind
  // ("team/companies/acme.md" → companies) when the backend sends no
  // explicit categories. Links to the auto-generated category index page.
  const pathCategories =
    article.categories.length > 0
      ? article.categories
      : breadcrumbSegments.length > 2 && breadcrumbSegments[0] === "team"
        ? [breadcrumbSegments[1]]
        : [];

  if (tab === "edit") {
    return (
      <main
        className="wk-article-col wk-article-col--editing"
        aria-label={`Editing ${article.title}`}
      >
        <LiveEditingBanner liveAgent={liveAgent} article={article} />
        <HatBar
          active={tab}
          onChange={setTab}
          rightRail={context ? [context] : undefined}
        />
        <ArticleTabPanels
          tab={tab}
          article={article}
          catalog={catalog}
          resolver={resolver}
          onNavigate={onNavigate}
          onEditSection={() => setTab("edit")}
          visualArtifact={visualArtifact}
          inlineArtifacts={inlineArtifacts}
          onEditorSaved={handleEditorSaved}
          onEditorCancel={handleEditorCancel}
          onVersionRestored={handleVersionRestored}
        />
      </main>
    );
  }

  return (
    <>
      <main className="wk-article-col">
        <div className="wk-article-page">
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
          {/* One clean header row: breadcrumb left (single line, middle
              segments truncate), page actions right. The title sits BELOW
              on its own line — previously the two floated toolbars
              overlapped the wrapping breadcrumb. */}
          <div className="wk-article-header" data-testid="wk-article-header">
            <ArticleBreadcrumb
              article={article}
              segments={breadcrumbSegments}
              onNavigate={onNavigate}
            />
            <div className="wk-article-actions">
              <RequestAIChangeControl
                title={article.title}
                path={article.path}
              />
              <ArticleDeleteControl
                title={article.title}
                path={article.path}
                onDeleted={() => onNavigate("")}
              />
            </div>
          </div>
          <ArticleTitle title={article.title} />
          {byline}
          <ArticleBadges article={article} />
          <ArticleTabPanels
            tab={tab}
            article={article}
            catalog={catalog}
            resolver={resolver}
            onNavigate={onNavigate}
            onEditSection={() => setTab("edit")}
            visualArtifact={visualArtifact}
            inlineArtifacts={inlineArtifacts}
            onEditorSaved={handleEditorSaved}
            onEditorCancel={handleEditorCancel}
            onVersionRestored={handleVersionRestored}
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
          <CategoriesFooter
            tags={pathCategories}
            onSelect={(tag) => onNavigate(categoryPath(tag))}
          />
          <PageFooter
            lastEditedBy={formatAgentName(article.last_edited_by)}
            lastEditedTs={article.last_edited_ts}
            articlePath={article.path}
          />
        </div>
      </main>
      <ArticleRightSidebar
        article={article}
        toc={toc}
        onNavigate={onNavigate}
        onMaintenanceApplied={() => setRefreshNonce((n) => n + 1)}
      />
    </>
  );
}

function ArticleErrorState({
  message,
  onRetry,
}: {
  message: string;
  onRetry: () => void;
}) {
  return (
    <div className="wk-error" role="alert">
      <p className="wk-error-msg">{message}</p>
      <button
        type="button"
        className="wk-retry-btn"
        onClick={onRetry}
        aria-label="Retry loading article"
      >
        Retry
      </button>
    </div>
  );
}

/**
 * Read-view "Delete page" affordance: a small token-styled trigger plus a
 * confirm dialog. Owns its own open/pending/error state so the parent article
 * component stays lean. Destructive, so it follows tell-don't-ask: the dialog
 * shows the recommendation up front and focus lands on Cancel (a stray Enter
 * cancels rather than destroying data). On success the parent navigates away
 * via onDeleted so the user is never stranded on a now-404 page.
 */
function ArticleDeleteControl({
  title,
  path,
  onDeleted,
}: {
  title: string;
  path: string;
  onDeleted: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const queryClient = useQueryClient();

  const confirm = async () => {
    if (pending) return;
    setPending(true);
    setError(null);
    try {
      await deletePage(path);
      // Drop the page from the always-visible sidebar file tree. The tree is a
      // React Query (WIKI_TREE_QUERY_KEY) that only refetches on its OWN
      // mutations; a delete fired from the article view bypassed it, so the
      // deleted page lingered in the index until a manual reload. Invalidate it
      // here so the index reflects the delete immediately.
      await queryClient.invalidateQueries({ queryKey: WIKI_TREE_QUERY_KEY });
      setOpen(false);
      onDeleted();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Delete failed.");
    } finally {
      setPending(false);
    }
  };

  return (
    <>
      <div className="wk-article-toolbar">
        <button
          type="button"
          className="wk-article-delete-btn"
          data-testid="wk-article-delete"
          onClick={() => {
            setError(null);
            setOpen(true);
          }}
        >
          Delete page
        </button>
      </div>
      {open ? (
        <ConfirmDeleteArticle
          title={title}
          path={path}
          pending={pending}
          error={error}
          onCancel={() => setOpen(false)}
          onConfirm={() => {
            void confirm();
          }}
        />
      ) : null}
    </>
  );
}

/**
 * Confirm-before-delete dialog for the article view. Mirrors the tree's
 * ConfirmDelete: focus lands on Cancel first (a stray Enter cancels rather than
 * destroying data), Escape closes, and the recommendation is shown up front so
 * the destructive action is tell-don't-ask with a one-click veto.
 */
function ConfirmDeleteArticle({
  title,
  path,
  pending,
  error,
  onCancel,
  onConfirm,
}: {
  title: string;
  path: string;
  pending: boolean;
  error: string | null;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const trapRef = useFocusTrap<HTMLDivElement>();
  return (
    <div
      ref={trapRef}
      className="wk-modal-backdrop"
      role="dialog"
      aria-modal="true"
      aria-labelledby="wk-article-delete-title"
      data-testid="wk-article-delete-confirm"
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.preventDefault();
          onCancel();
        }
      }}
    >
      <div className="wk-modal wk-tree2-modal">
        <h2 id="wk-article-delete-title">Delete “{title}”?</h2>
        <p className="wk-editor-help">
          This permanently deletes <code>{path}</code>. We recommend deleting
          only pages you are sure are no longer referenced. This cannot be
          undone.
        </p>
        {error ? (
          <div
            className="wk-editor-banner wk-editor-banner--error"
            role="alert"
          >
            {error}
          </div>
        ) : null}
        <div className="wk-editor-actions">
          <button
            type="button"
            className="wk-editor-cancel"
            onClick={onCancel}
            disabled={pending}
          >
            Cancel
          </button>
          <button
            type="button"
            className="wk-editor-save wk-tree2-danger-btn"
            data-testid="wk-article-delete-confirm-btn"
            onClick={onConfirm}
            disabled={pending}
          >
            {pending ? "Deleting…" : "Delete"}
          </button>
        </div>
      </div>
    </div>
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
  // The repo-root "team" segment is plumbing, not a space — docmost-style
  // breadcrumbs read "space / parent / page".
  const displaySegments = segments[0] === "team" ? segments.slice(1) : segments;
  return (
    <nav className="wk-breadcrumb" aria-label="Breadcrumb">
      <a
        href="#/wiki"
        onClick={(e) => {
          e.preventDefault();
          onNavigate("");
        }}
      >
        Wiki
      </a>
      {keyedByOccurrence(displaySegments, (seg) => seg).map(
        ({ key, value: seg, index: i }) => (
          <span key={key} style={{ display: "contents" }}>
            <span className="sep" aria-hidden="true">
              /
            </span>
            {i < displaySegments.length - 1 ? (
              // Middle segments are path kinds (companies, people, …) —
              // the wiki's "spaces". They link to the auto-generated
              // category index page, not a folder.
              <a
                href={`#/wiki/${categoryPath(seg)}`}
                onClick={(e) => {
                  e.preventDefault();
                  onNavigate(categoryPath(seg));
                }}
              >
                {seg}
              </a>
            ) : (
              <span aria-current="page">{article.title}</span>
            )}
          </span>
        ),
      )}
    </nav>
  );
}

function ArticleBadges({ article }: { article: WikiArticleT }) {
  return (
    <>
      <StalenessIndicator article={article} />
      <CompressButton
        key={article.path}
        articlePath={article.path}
        wordCount={article.word_count}
      />
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
  resolver,
  onNavigate,
  onEditSection,
  visualArtifact,
  inlineArtifacts,
  onEditorSaved,
  onEditorCancel,
  onVersionRestored,
}: {
  tab: HatBarTab;
  article: WikiArticleT;
  catalog: WikiCatalogEntry[];
  resolver: (slug: string) => boolean;
  onNavigate: (path: string) => void;
  onEditSection: () => void;
  visualArtifact: RichArtifactDetail | null;
  inlineArtifacts: RichArtifactDetail[];
  onEditorSaved: (newSha: string) => void;
  onEditorCancel: () => void;
  onVersionRestored: (newSha: string) => void;
}) {
  switch (tab) {
    case "article":
      return (
        <ArticleReadView
          content={article.content}
          title={article.title}
          articlePath={article.path}
          resolver={resolver}
          onNavigate={onNavigate}
          onEditSection={onEditSection}
          visualArtifact={visualArtifact}
          inlineArtifacts={inlineArtifacts}
        />
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
        <VersionHistory path={article.path} onRestored={onVersionRestored} />
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
  onMaintenanceApplied,
}: {
  article: WikiArticleT;
  toc: TocEntry[];
  onNavigate: (path: string) => void;
  onMaintenanceApplied: () => void;
}) {
  // Consume the WikiLint "Suggest fix" hand-off exactly once per article
  // path. The consume call removes the slot from sessionStorage, so it is a
  // side effect — running it inside useMemo would let React 19 strict mode
  // double-invoke it and lose the hand-off (the first call clears the slot;
  // the second sees nothing). We do the consume inside a useEffect guarded
  // by a ref so the result is captured by the first mount and not undone
  // by strict-mode's intentional double-invoke.
  const [initialMaintenanceAction, setInitialMaintenanceAction] = useState<
    WikiMaintenanceAction | undefined
  >(undefined);
  const consumedForPathRef = useRef<string | null>(null);
  useEffect(() => {
    if (consumedForPathRef.current === article.path) return;
    consumedForPathRef.current = article.path;
    const target = consumeMaintenanceTarget(article.path) ?? undefined;
    if (target) {
      setInitialMaintenanceAction(target);
    }
  }, [article.path]);
  return (
    <aside className="wk-right-sidebar">
      <ArticleContents entries={toc} />
      <PageStatsPanel
        revisions={article.revisions}
        contributors={article.contributors.length}
        wordCount={article.word_count}
        created={article.last_edited_ts}
        lastEdit={article.last_edited_ts}
      />
      <WikiMaintenanceAssistant
        articlePath={article.path}
        articleSha={article.commit_sha ?? ""}
        onApplied={onMaintenanceApplied}
        initialAction={initialMaintenanceAction ?? undefined}
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
