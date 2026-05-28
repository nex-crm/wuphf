import { useQueries } from "@tanstack/react-query";

import {
  fetchRichArtifact,
  type RichArtifactDetail,
} from "../../api/richArtifacts";
import { router } from "../../lib/router";

interface MessageArtifactReferencesProps {
  artifactIds: string[];
}

// MessageArtifactReferences renders a clickable preview card for each
// artifact referenced by the message. The card carries the article title,
// summary, trust pill, and an "Open article →" action that navigates to
// the full-screen ArticleView. The article body is NOT inlined in the
// chat bubble — that breaks the chat reading rhythm and made it look
// like the agent had given the answer in chat when the actual article
// (with embedded figures) lives elsewhere.
export default function MessageArtifactReferences({
  artifactIds,
}: MessageArtifactReferencesProps) {
  const artifactQueries = useQueries({
    queries: artifactIds.map((id) => ({
      queryKey: ["rich-artifact-reference", id],
      queryFn: () => fetchRichArtifact(id),
      staleTime: 60_000,
    })),
  });

  if (artifactIds.length === 0) return null;

  return (
    <section
      className="message-artifact-references"
      aria-label="Article references"
    >
      {artifactIds.map((id, index) => {
        const result = artifactQueries[index];
        return (
          <ArticleCard
            key={id}
            id={id}
            detail={result?.data}
            error={queryErrorMessage(result?.error)}
          />
        );
      })}
    </section>
  );
}

interface ArticleCardProps {
  id: string;
  detail?: RichArtifactDetail;
  error?: string;
}

function ArticleCard({ id, detail, error }: ArticleCardProps) {
  if (error) {
    return (
      <div className="message-artifact-card message-artifact-error" role="alert">
        <span className="message-artifact-kicker">Article</span>
        <p>Could not load article {id}: {error}</p>
      </div>
    );
  }
  if (!detail) {
    return (
      <div className="message-artifact-card" aria-busy="true">
        <span className="message-artifact-kicker">Article</span>
        <p>Loading…</p>
      </div>
    );
  }
  const { artifact } = detail;
  return (
    <button
      type="button"
      className="message-artifact-card message-artifact-card-clickable"
      onClick={() => {
        void router.navigate({
          to: "/articles/$articleId",
          params: { articleId: artifact.id },
        });
      }}
      aria-label={`Open article: ${artifact.title}`}
    >
      <div className="message-artifact-card-head">
        <span className="message-artifact-kicker">Article</span>
        <span className="rich-artifact-trust">{artifact.trustLevel}</span>
      </div>
      <h4 className="message-artifact-card-title">{artifact.title}</h4>
      {artifact.summary ? (
        <p className="message-artifact-card-summary">{artifact.summary}</p>
      ) : null}
      <span className="message-artifact-card-action">Open article →</span>
    </button>
  );
}

function queryErrorMessage(error: unknown): string | undefined {
  if (!error) return undefined;
  return error instanceof Error ? error.message : "Failed to load artifact";
}
