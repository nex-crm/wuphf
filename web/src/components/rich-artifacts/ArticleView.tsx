import { useEffect, useState } from "react";

import {
  fetchRichArtifact,
  type RichArtifactDetail,
} from "../../api/richArtifacts";
import { router } from "../../lib/router";
import RichArtifactEmbed from "./RichArtifactEmbed";

// ArticleView is the full-screen reader for a single HTML article (a rich
// artifact). Linked from chat artifact cards. The article body is rendered
// at full page size via the shadow-DOM RichArtifactEmbed; the title bar
// surfaces the human-readable title + trust pill + a back-to-chat link.

interface ArticleViewProps {
  articleId: string;
}

export function ArticleView({ articleId }: ArticleViewProps) {
  const [detail, setDetail] = useState<RichArtifactDetail | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setDetail(null);
    setError(null);
    if (!articleId) {
      setError("No article id provided");
      return () => {
        cancelled = true;
      };
    }
    fetchRichArtifact(articleId)
      .then((d) => {
        if (!cancelled) setDetail(d);
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setError(
            err instanceof Error ? err.message : "Failed to load article",
          );
        }
      });
    return () => {
      cancelled = true;
    };
  }, [articleId]);

  if (error) {
    return (
      <div className="article-view-shell">
        <div className="article-view-error" role="alert">
          Could not load article {articleId}: {error}
        </div>
      </div>
    );
  }

  if (!detail) {
    return (
      <div className="article-view-shell" aria-busy="true">
        <div className="article-view-loading">Loading article…</div>
      </div>
    );
  }

  const { artifact, html } = detail;
  return (
    <article className="article-view-shell">
      <header className="article-view-head">
        <button
          type="button"
          className="article-view-back"
          onClick={() => {
            router.history.back();
          }}
          aria-label="Back"
        >
          ← Back
        </button>
        <div className="article-view-title-block">
          <h1 className="article-view-title">{artifact.title}</h1>
          {artifact.summary ? (
            <p className="article-view-summary">{artifact.summary}</p>
          ) : null}
          <div className="article-view-meta">
            <span className="rich-artifact-trust">{artifact.trustLevel}</span>
            <span className="article-view-author">by @{artifact.createdBy}</span>
          </div>
        </div>
      </header>
      <div className="article-view-body">
        <RichArtifactEmbed title={artifact.title} html={html} />
      </div>
    </article>
  );
}
