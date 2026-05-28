import { useQueries } from "@tanstack/react-query";

import {
  fetchRichArtifact,
  type RichArtifactDetail,
} from "../../api/richArtifacts";
import RichArtifactEmbed from "../rich-artifacts/RichArtifactEmbed";

interface MessageArtifactReferencesProps {
  artifactIds: string[];
}

// MessageArtifactReferences renders each artifact referenced by a chat
// message inline, in the same bubble, with no separator chrome. The
// visual IS the message; a NOTEBOOK VISUAL kicker or Expand button would
// frame it as a separate thing and break the flow.
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
      aria-label="Rich artifacts"
    >
      {artifactIds.map((id, index) => {
        const result = artifactQueries[index];
        return (
          <MessageArtifactReference
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

interface MessageArtifactReferenceProps {
  id: string;
  detail?: RichArtifactDetail;
  error?: string;
}

function MessageArtifactReference({
  id,
  detail,
  error,
}: MessageArtifactReferenceProps) {
  if (error) {
    return (
      <div className="message-artifact-error" role="alert">
        Could not load artifact {id}: {error}
      </div>
    );
  }
  if (!detail) {
    return (
      <div className="message-artifact-loading" aria-busy="true">
        Loading artifact…
      </div>
    );
  }
  return <RichArtifactEmbed title={detail.artifact.title} html={detail.html} />;
}

function queryErrorMessage(error: unknown): string | undefined {
  if (!error) return undefined;
  return error instanceof Error ? error.message : "Failed to load artifact";
}
