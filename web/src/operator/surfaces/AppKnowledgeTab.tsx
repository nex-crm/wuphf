// AppKnowledgeTab — the Knowledge tab for a built app, backed by the REAL
// workspace wiki (gbrain), not mock pages. Knowledge is owned once at the
// workspace level and inherited by every app, so this is a reader over the live
// wiki catalog: pick a page on the left, read the real article on the right.
//
// Reuses the shipped wiki client (web/src/api/wiki.ts) unchanged — the same
// endpoints the main Wiki app reads — so there is one source of truth.

import { useMemo, useState } from "react";
import ReactMarkdown from "react-markdown";
import { useQuery } from "@tanstack/react-query";

import { fetchArticle, fetchCatalog } from "../../api/wiki";
import { EmptyState } from "../components/EmptyState";
import { Eyebrow } from "../components/primitives";

export function AppKnowledgeTab() {
  const catalogQuery = useQuery({
    queryKey: ["operator-knowledge-catalog"],
    queryFn: fetchCatalog,
  });
  const entries = useMemo(
    () =>
      [...(catalogQuery.data ?? [])].sort((a, b) =>
        a.title.localeCompare(b.title),
      ),
    [catalogQuery.data],
  );

  const [activePath, setActivePath] = useState<string | null>(null);
  // Default to the first page once the catalog lands.
  const selectedPath = activePath ?? entries[0]?.path ?? null;

  const articleQuery = useQuery({
    queryKey: ["operator-knowledge-article", selectedPath],
    queryFn: () => fetchArticle(selectedPath ?? ""),
    enabled: Boolean(selectedPath),
  });

  if (catalogQuery.isLoading) {
    return (
      <div className="opr-app-building" role="status">
        <span className="opr-work-dots" aria-hidden={true}>
          <span />
          <span />
          <span />
        </span>
        <div className="opr-empty-title">Opening the company brain…</div>
      </div>
    );
  }

  if (catalogQuery.isError) {
    return (
      <EmptyState
        glyph="✦"
        title="Knowledge is offline"
        hint="The workspace brain could not be reached. It will be here once the workspace is running."
      />
    );
  }

  if (entries.length === 0) {
    return (
      <EmptyState
        glyph="✦"
        title="No knowledge yet"
        hint="As your team works, the company brain fills with pages your apps can draw on. None have been written yet."
      />
    );
  }

  const article = articleQuery.data;

  return (
    <div className="opr-tool-scoped opr-app-knowledge">
      <div className="opr-data-intro">
        <Eyebrow>Workspace knowledge</Eyebrow>
        <p className="opr-scoped-note">
          The company brain, owned by your workspace and inherited by every app.
          These are real pages your team and AI have written.
        </p>
      </div>

      <div className="opr-knowledge-split">
        <nav className="opr-kn-list" aria-label="Knowledge pages">
          {entries.map((entry) => (
            <button
              key={entry.path}
              type="button"
              className={`opr-kn-item${
                entry.path === selectedPath ? " is-active" : ""
              }`}
              onClick={() => setActivePath(entry.path)}
            >
              {entry.title}
            </button>
          ))}
        </nav>

        <article className="opr-kn-article">
          {articleQuery.isLoading ? (
            <p className="opr-scoped-note">Reading…</p>
          ) : article ? (
            <>
              <h1 className="opr-kn-article-title">{article.title}</h1>
              <div className="opr-article-meta">
                From the company brain · last edited by{" "}
                {article.last_edited_by || "the team"}
              </div>
              <div className="opr-kn-markdown">
                <ReactMarkdown skipHtml={true}>{article.content}</ReactMarkdown>
              </div>
            </>
          ) : (
            <p className="opr-scoped-note">
              This page could not be loaded. Pick another from the list.
            </p>
          )}
        </article>
      </div>
    </div>
  );
}
