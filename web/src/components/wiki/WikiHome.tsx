import { useEffect, useMemo, useRef, useState } from "react";

import {
  fetchAuditLog,
  searchWiki,
  type WikiAuditEntry,
  type WikiCatalogEntry,
  type WikiSearchHit,
} from "../../api/wiki";
import { formatAgentName } from "../../lib/agentName";
import { formatRelativeTime, pluralize } from "../../lib/format";
import NewArticleModal from "./NewArticleModal";
import { categoryLabel } from "./WikiCategoryPage";

/**
 * The wiki's overview page, docmost-style: a quiet landing next to the
 * always-visible page tree (the tree is the navigation — no category-card
 * detour). A prominent search box with instant title suggestions
 * (client-side over the catalog) and full-text results on submit
 * (GET /wiki/search), then a single recently-updated list.
 */

interface WikiHomeProps {
  catalog: WikiCatalogEntry[];
  onNavigate: (path: string) => void;
  /**
   * Injectable recent-changes list for tests/Storybook. When omitted the
   * component fetches the audit log itself and falls back to the catalog's
   * most recently edited articles when the audit log is empty.
   */
  recentChanges?: WikiAuditEntry[];
}

const SUGGESTION_LIMIT = 8;
const RECENT_LIMIT = 8;

interface TitleSuggestion {
  path: string;
  title: string;
  group: string;
}

/** Instant title suggestions: prefix matches first, then substring. */
export function suggestTitles(
  catalog: WikiCatalogEntry[],
  query: string,
): TitleSuggestion[] {
  const q = query.trim().toLowerCase();
  if (!q) return [];
  const prefix: TitleSuggestion[] = [];
  const substring: TitleSuggestion[] = [];
  for (const entry of catalog) {
    const title = entry.title.toLowerCase();
    const path = entry.path.toLowerCase();
    if (title.startsWith(q)) {
      prefix.push(entry);
    } else if (title.includes(q) || path.includes(q)) {
      substring.push(entry);
    }
  }
  return [...prefix, ...substring].slice(0, SUGGESTION_LIMIT);
}

export default function WikiHome({
  catalog,
  onNavigate,
  recentChanges,
}: WikiHomeProps) {
  const [query, setQuery] = useState("");
  const [hits, setHits] = useState<WikiSearchHit[] | null>(null);
  const [searching, setSearching] = useState(false);
  const [showNew, setShowNew] = useState(false);
  const [auditEntries, setAuditEntries] = useState<WikiAuditEntry[]>(
    recentChanges ?? [],
  );
  const searchSeqRef = useRef(0);

  useEffect(() => {
    if (recentChanges) return;
    let cancelled = false;
    fetchAuditLog({ limit: RECENT_LIMIT })
      .then((res) => {
        if (!cancelled) setAuditEntries(res.entries ?? []);
      })
      .catch(() => {
        // fetchAuditLog already swallows; belt-and-suspenders for mocks.
      });
    return () => {
      cancelled = true;
    };
  }, [recentChanges]);

  const suggestions = useMemo(
    () => suggestTitles(catalog, query),
    [catalog, query],
  );

  // Recently updated articles from the catalog — the fallback "recent
  // changes" source when the audit log has nothing (fresh installs, mocks).
  const recentArticles = useMemo(
    () =>
      [...catalog]
        .sort((a, b) => (a.last_edited_ts < b.last_edited_ts ? 1 : -1))
        .slice(0, RECENT_LIMIT),
    [catalog],
  );

  const runFullSearch = (pattern: string) => {
    const trimmed = pattern.trim();
    if (!trimmed) return;
    const seq = ++searchSeqRef.current;
    setSearching(true);
    searchWiki(trimmed)
      .then((res) => {
        if (searchSeqRef.current !== seq) return;
        setHits(res);
      })
      .finally(() => {
        if (searchSeqRef.current === seq) setSearching(false);
      });
  };

  return (
    <main className="wk-home" data-testid="wk-home">
      <header className="wk-home-masthead">
        <h1 className="wk-home-title">Company Brain</h1>
        <p className="wk-home-tagline">
          Your team’s encyclopedia ·{" "}
          {`${catalog.length} ${pluralize(catalog.length, "article")}`}
        </p>
        <form
          className="wk-home-search"
          role="search"
          onSubmit={(e) => {
            e.preventDefault();
            if (suggestions.length === 1) {
              onNavigate(suggestions[0].path);
              return;
            }
            runFullSearch(query);
          }}
        >
          <input
            type="search"
            className="wk-home-search-input"
            data-testid="wk-home-search"
            placeholder="Search the company brain"
            aria-label="Search the company brain"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setHits(null);
            }}
            // biome-ignore lint/a11y/noAutofocus: search-first landing — focusing the search box IS the page's purpose, mirroring Wikipedia's search portal.
            autoFocus={true}
          />
          {suggestions.length > 0 && hits === null ? (
            <ul className="wk-home-suggestions" data-testid="wk-suggestions">
              {suggestions.map((s) => (
                <li key={s.path}>
                  <a
                    href={`#/wiki/${encodeURI(s.path)}`}
                    onClick={(e) => {
                      e.preventDefault();
                      onNavigate(s.path);
                    }}
                  >
                    <span className="wk-suggestion-title">{s.title}</span>
                    <span className="wk-suggestion-group">
                      {categoryLabel(s.group)}
                    </span>
                  </a>
                </li>
              ))}
              <li className="wk-home-suggestions-foot">
                <button
                  type="button"
                  onClick={() => runFullSearch(query)}
                  disabled={searching}
                >
                  {searching
                    ? "Searching…"
                    : `Search article text for “${query.trim()}”`}
                </button>
              </li>
            </ul>
          ) : null}
        </form>
        {hits !== null ? (
          <section
            className="wk-home-results"
            aria-label="Search results"
            data-testid="wk-search-results"
          >
            <h2>
              {hits.length} {pluralize(hits.length, "match", "matches")}
            </h2>
            {hits.length === 0 ? (
              <p className="wk-home-empty">
                No article text matches “{query.trim()}”.
              </p>
            ) : (
              <ul>
                {hits.map((hit) => (
                  <li key={`${hit.path}:${hit.line}`}>
                    <a
                      href={`#/wiki/${encodeURI(hit.path)}`}
                      onClick={(e) => {
                        e.preventDefault();
                        onNavigate(hit.path);
                      }}
                    >
                      {hit.path}
                    </a>
                    <span className="wk-home-snippet">{hit.snippet}</span>
                  </li>
                ))}
              </ul>
            )}
          </section>
        ) : null}
      </header>

      <div className="wk-home-grid">
        <section className="wk-home-card" aria-label="Recently updated">
          <div className="wk-home-card-head">
            <h2>Recently updated</h2>
            <button
              type="button"
              className="wk-home-new-btn"
              data-testid="wk-home-new"
              onClick={() => setShowNew(true)}
            >
              + New page
            </button>
          </div>
          {auditEntries.length > 0 ? (
            <ul className="wk-home-recent">
              {auditEntries.slice(0, RECENT_LIMIT).map((entry) => (
                <li key={entry.sha}>
                  <span className="wk-home-recent-msg">{entry.message}</span>
                  <span className="wk-home-recent-meta">
                    {formatAgentName(entry.author_slug)} ·{" "}
                    {safeRelative(entry.timestamp)}
                  </span>
                  {entry.paths.slice(0, 3).map((p) => (
                    <a
                      key={p}
                      className="wk-home-recent-path"
                      href={`#/wiki/${encodeURI(p)}`}
                      onClick={(e) => {
                        e.preventDefault();
                        onNavigate(p);
                      }}
                    >
                      {p}
                    </a>
                  ))}
                </li>
              ))}
            </ul>
          ) : (
            <ul className="wk-home-recent">
              {recentArticles.map((entry) => (
                <li key={entry.path}>
                  <a
                    href={`#/wiki/${encodeURI(entry.path)}`}
                    onClick={(e) => {
                      e.preventDefault();
                      onNavigate(entry.path);
                    }}
                  >
                    {entry.title}
                  </a>
                  <span className="wk-home-recent-meta">
                    {formatAgentName(entry.author_slug)} ·{" "}
                    {safeRelative(entry.last_edited_ts)}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </section>
      </div>

      {showNew ? (
        <NewArticleModal
          catalog={catalog}
          onCancel={() => setShowNew(false)}
          onCreated={(path) => {
            setShowNew(false);
            onNavigate(path);
          }}
        />
      ) : null}
    </main>
  );
}

function safeRelative(iso: string): string {
  try {
    return formatRelativeTime(iso);
  } catch {
    return iso;
  }
}
