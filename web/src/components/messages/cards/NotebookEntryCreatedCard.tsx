/**
 * NotebookEntryCreatedCard — chat card emitted by the broker in #general
 * when an agent writes a NEW notebook entry (not on updates).
 *
 * Click → navigates to /notebooks/<agentSlug>/<entrySlug>. The route
 * expects { agentSlug, entrySlug } — we derive entrySlug from the
 * filename (strip dir, strip .md). Reuses .issue-lifecycle-card chrome.
 */

import { router } from "../../../lib/router";

export interface NotebookEntryCreatedPayload {
  slug?: string;
  path?: string;
  title?: string;
  author?: string;
}

function isStringField(value: unknown): value is string {
  return typeof value === "string" && value.length > 0;
}

export function parseNotebookEntryCreatedPayload(
  raw: unknown,
): NotebookEntryCreatedPayload {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
    return {};
  }
  const r = raw as Record<string, unknown>;
  const out: NotebookEntryCreatedPayload = {};
  if (isStringField(r.slug)) out.slug = r.slug;
  if (isStringField(r.path)) out.path = r.path;
  if (isStringField(r.title)) out.title = r.title;
  if (isStringField(r.author)) out.author = r.author;
  return out;
}

function entrySlugFromPath(path: string): string {
  const base = path.split("/").pop() ?? path;
  return base.replace(/\.md$/i, "");
}

export interface NotebookEntryCreatedCardProps {
  payload: NotebookEntryCreatedPayload;
}

export function NotebookEntryCreatedCard({
  payload,
}: NotebookEntryCreatedCardProps) {
  // `??` would let an empty title slip through and yield a blank
  // visible title; use `||` so the fallback chain (entry slug, then
  // literal placeholder) actually fires for empty / whitespace values.
  const agentSlug = payload.slug?.trim() ?? "";
  const path = payload.path?.trim() ?? "";
  const entrySlug = path ? entrySlugFromPath(path) : "";
  const title = payload.title?.trim() || entrySlug || "(untitled entry)";
  const author = payload.author?.trim();
  const canNavigate = Boolean(agentSlug && entrySlug);

  function openEntry() {
    if (!canNavigate) return;
    void router.navigate({
      to: "/notebooks/$agentSlug/$entrySlug",
      params: { agentSlug, entrySlug },
    });
  }

  return (
    <button
      type="button"
      className="issue-lifecycle-card issue-lifecycle-card--neutral"
      onClick={openEntry}
      data-testid="notebook-entry-created-card"
      data-agent-slug={agentSlug}
      data-entry-slug={entrySlug}
      aria-label={`Open notebook entry ${title}`}
      disabled={!canNavigate}
    >
      <span className="issue-lifecycle-card-icon" aria-hidden="true">
        📓
      </span>
      <span className="issue-lifecycle-card-body">
        <span className="issue-lifecycle-card-eyebrow">
          New notebook entry
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
