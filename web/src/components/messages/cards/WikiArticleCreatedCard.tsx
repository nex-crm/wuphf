/**
 * WikiArticleCreatedCard — chat card emitted by the broker in #general
 * when a brand-new wiki article is first created (not on updates).
 *
 * Click → navigates to /wiki/<path>. Reuses .issue-lifecycle-card styling
 * (same chrome, same hover/disabled behavior) so the cards look like a
 * family — the only thing that varies between them is the icon, eyebrow,
 * and destination route.
 */

import { router } from "../../../lib/router";

export interface WikiArticleCreatedPayload {
  path?: string;
  title?: string;
  author?: string;
}

function isStringField(value: unknown): value is string {
  return typeof value === "string" && value.length > 0;
}

export function parseWikiArticleCreatedPayload(
  raw: unknown,
): WikiArticleCreatedPayload {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
    return {};
  }
  const r = raw as Record<string, unknown>;
  const out: WikiArticleCreatedPayload = {};
  if (isStringField(r.path)) out.path = r.path;
  if (isStringField(r.title)) out.title = r.title;
  if (isStringField(r.author)) out.author = r.author;
  return out;
}

export interface WikiArticleCreatedCardProps {
  payload: WikiArticleCreatedPayload;
}

export function WikiArticleCreatedCard({
  payload,
}: WikiArticleCreatedCardProps) {
  const path = payload.path ?? "";
  const title = payload.title ?? path ?? "(untitled article)";
  const author = payload.author;

  function openArticle() {
    if (!path) return;
    void router.navigate({
      to: "/wiki/$",
      params: { _splat: path },
    });
  }

  return (
    <button
      type="button"
      className="issue-lifecycle-card issue-lifecycle-card--review"
      onClick={openArticle}
      data-testid="wiki-article-created-card"
      data-article-path={path}
      aria-label={`Open wiki article ${title}`}
      disabled={!path}
    >
      <span className="issue-lifecycle-card-icon" aria-hidden="true">
        📖
      </span>
      <span className="issue-lifecycle-card-body">
        <span className="issue-lifecycle-card-eyebrow">
          New wiki article
          {author ? (
            <span className="issue-lifecycle-card-id"> · by @{author}</span>
          ) : null}
        </span>
        <span className="issue-lifecycle-card-title">{title}</span>
        {path ? (
          <span className="issue-lifecycle-card-meta">{path}</span>
        ) : null}
      </span>
      <span className="issue-lifecycle-card-cta" aria-hidden="true">
        Open →
      </span>
    </button>
  );
}
