import { useEffect, useMemo, useState } from "react";
import ReactMarkdown from "react-markdown";
import type { PluggableList } from "unified";

import { get } from "../../api/client";
import {
  buildMarkdownComponents,
  buildRehypePlugins,
  buildRemarkPlugins,
} from "../../lib/wikiMarkdownConfig";
import Hatnote from "./Hatnote";

// QueryAnswer mirrors the JSON shape returned by GET /wiki/lookup.
export interface QuerySource {
  kind: string;
  slug_or_id: string;
  title: string;
  excerpt: string;
  valid_from?: string;
  valid_until?: string;
  staleness: number;
  source_path?: string;
}

export interface QueryAnswer {
  query_class: string;
  answer_markdown: string;
  sources_cited: number[];
  sources: QuerySource[];
  confidence: number;
  coverage: string; // 'complete' | 'partial' | 'none'
  notes?: string;
  latency_ms: number;
}

export interface CitedAnswerProps {
  /** The natural-language question to answer. */
  query: string;
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
  const [answer, setAnswer] = useState<QueryAnswer | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  // CitedAnswer does not resolve wikilinks against the local catalog — the
  // broker's answer_query prompt only wikilinks entities that exist in sources.
  // A permissive resolver avoids false "broken link" reds on citation anchors.
  const resolver = useMemo(() => () => true, []);
  const remarkPlugins = useMemo<PluggableList>(
    () => buildRemarkPlugins(resolver),
    [resolver],
  );
  const rehypePlugins = useMemo<PluggableList>(() => buildRehypePlugins(), []);
  const markdownComponents = useMemo(
    () => buildMarkdownComponents({ resolver }),
    [resolver],
  );

  useEffect(() => {
    if (!query.trim()) {
      setLoading(false);
      return;
    }
    setLoading(true);
    setAnswer(null);
    setError(null);

    get<QueryAnswer>("/wiki/lookup", { q: query })
      .then((ans) => {
        setAnswer(ans);
        setLoading(false);
      })
      .catch((e: Error) => {
        setError(e.message);
        setLoading(false);
      });
  }, [query]);

  // Loading state — Fraunces-italic status line per DESIGN-WIKI.md hatnote
  // typography (§9 typography, §5 hatnote voice). Previously we rendered
  // skeleton blocks, but the editorial surface reads better with a single
  // calm status line than geometric shimmer blocks.
  if (loading) {
    return (
      <div
        className="wk-cited-answer wk-cited-answer--loading"
        role="status"
        aria-busy="true"
        aria-label="Loading cited answer…"
      >
        <p className="wk-lookup-status">Searching the wiki…</p>
      </div>
    );
  }

  // Error state — hatnote-styled
  if (error) {
    return (
      <div className="wk-cited-answer wk-cited-answer--error">
        <Hatnote>
          <em>Wiki lookup failed:</em> {error}
        </Hatnote>
      </div>
    );
  }

  if (!answer) return null;

  const isOutOfScope =
    answer.query_class === "general" || answer.coverage === "none";

  const citedSources = answer.sources.filter((_, i) =>
    answer.sources_cited.includes(i + 1),
  );

  const mostRecentValidFrom = answer.sources.reduce<string>((best, s) => {
    const vf = (s.valid_from || "").trim();
    if (!vf) return best;
    return best === "" || vf > best ? vf : best;
  }, "");

  return (
    <article className="wk-cited-answer">
      {/* Hatnote — always present, coverage context */}
      <Hatnote>
        <em>From the wiki</em>
        {answer.coverage === "partial" && " (partial match)"}
        {answer.coverage === "none" && " (no match)"}
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
          I can help with questions about people, companies, and activities in
          your workspace.
        </p>
      )}

      {/* Sources — only cited entries, only when not out-of-scope.
          Each <li> carries an explicit `value` so browser numbering matches
          the [n] citations in the body even when gaps exist (e.g. cited [3, 5]
          drops sources 1, 2, 4 entirely). */}
      {!isOutOfScope && citedSources.length > 0 && (
        <section className="wk-sources" aria-labelledby="ca-sources-heading">
          <h2 id="ca-sources-heading">Sources</h2>
          <ol>
            {answer.sources
              .map((src, i) => ({ src, n: i + 1 }))
              .filter(({ n }) => answer.sources_cited.includes(n))
              .map(({ src, n }) => {
                const excerpt =
                  src.excerpt.length > 120
                    ? `${src.excerpt.slice(0, 120)}…`
                    : src.excerpt;
                return (
                  <li
                    key={src.slug_or_id || `src-${n}`}
                    id={`ca-s${n}`}
                    value={n}
                  >
                    <span className="wk-commit-msg">{excerpt}</span>
                    {src.title && <span className="wk-agent">{src.title}</span>}
                    {src.valid_from && (
                      <span className="wk-dim">
                        {" "}
                        · {src.valid_from.slice(0, 10)}
                      </span>
                    )}
                  </li>
                );
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
          <span aria-label="Answer latency">{answer.latency_ms}ms</span>
          {answer.sources.length > 0 && (
            <span>
              {answer.sources.length}{" "}
              {answer.sources.length === 1 ? "source" : "sources"}
            </span>
          )}
        </div>
      </div>
    </article>
  );
}
