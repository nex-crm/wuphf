import { useEffect, useMemo, useState } from "react";

import {
  fetchRichArtifact,
  fetchRichArtifacts,
  promoteRichArtifact,
  type RichArtifact,
  type RichArtifactDetail,
} from "../../api/richArtifacts";
import RichArtifactEmbed from "../rich-artifacts/RichArtifactEmbed";

interface NotebookVisualArtifactsProps {
  agentSlug: string;
  entrySlug: string;
  sourcePath: string;
  onNavigateWiki?: (wikiPath: string) => void;
}

interface LoadedArtifact {
  artifact: RichArtifact;
  detail?: RichArtifactDetail;
}

// NotebookVisualArtifacts renders every visual artifact attached to a
// notebook entry inline, in document flow, as part of the entry itself.
// No iframe, no modal, no Expand button. The artifact IS the content;
// chrome around it would be lying about that.
export default function NotebookVisualArtifacts({
  agentSlug,
  entrySlug,
  sourcePath,
  onNavigateWiki,
}: NotebookVisualArtifactsProps) {
  const canonicalSourcePath = useMemo(
    () => normalizeNotebookSourcePath(sourcePath),
    [sourcePath],
  );
  const [loaded, setLoaded] = useState<LoadedArtifact[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [promotingId, setPromotingId] = useState<string | null>(null);
  const defaultTarget = useMemo(
    () => `team/drafts/${agentSlug}-${entrySlug}-visual.md`,
    [agentSlug, entrySlug],
  );

  useEffect(() => {
    let cancelled = false;
    setError(null);
    setLoaded([]);
    if (!canonicalSourcePath) {
      return () => {
        cancelled = true;
      };
    }
    fetchRichArtifacts({ sourceMarkdownPath: canonicalSourcePath })
      .then(async (items) => {
        if (cancelled) return;
        // Fetch each artifact's HTML up-front. Notebook entries usually
        // have one (sometimes two) attached artifacts; eager-loading lets
        // the embed render in the same paint as the rest of the entry.
        const details = await Promise.all(
          items.map(async (artifact) => {
            try {
              const detail = await fetchRichArtifact(artifact.id);
              return { artifact, detail } satisfies LoadedArtifact;
            } catch {
              return { artifact } satisfies LoadedArtifact;
            }
          }),
        );
        if (!cancelled) setLoaded(details);
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setError(
            err instanceof Error ? err.message : "Failed to load artifacts",
          );
        }
      });
    return () => {
      cancelled = true;
    };
  }, [canonicalSourcePath]);

  async function promote(artifact: RichArtifact) {
    setPromotingId(artifact.id);
    setError(null);
    try {
      const promoted = await promoteRichArtifact(artifact.id, {
        targetWikiPath: defaultTarget,
        markdownSummary: `# ${artifact.title}\n\n${artifact.summary || "Promoted visual artifact."}\n`,
        mode: "replace",
      });
      setLoaded((items) =>
        items.map((item) =>
          item.artifact.id === promoted.id
            ? {
                artifact: promoted,
                detail: item.detail
                  ? { ...item.detail, artifact: promoted }
                  : undefined,
              }
            : item,
        ),
      );
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Promotion failed");
    } finally {
      setPromotingId(null);
    }
  }

  if (loaded.length === 0 && !error) return null;

  return (
    <section className="nb-visual-artifacts" aria-label="Visual artifacts">
      {loaded.map(({ artifact, detail }) => (
        <article
          key={artifact.id}
          className="nb-visual-artifact"
          data-testid={`nb-visual-artifact-${artifact.id}`}
        >
          <header className="nb-visual-artifact-meta">
            <span className="rich-artifact-trust">{artifact.trustLevel}</span>
            {artifact.promotedWikiPath ? (
              <button
                type="button"
                className="nb-visual-artifact-action"
                onClick={() =>
                  onNavigateWiki?.(artifact.promotedWikiPath ?? "")
                }
              >
                Open in wiki
              </button>
            ) : (
              <button
                type="button"
                className="nb-visual-artifact-action"
                disabled={promotingId === artifact.id}
                onClick={() => {
                  void promote(artifact);
                }}
              >
                {promotingId === artifact.id ? "Promoting…" : "Promote to wiki"}
              </button>
            )}
          </header>
          {detail ? (
            <RichArtifactEmbed title={artifact.title} html={detail.html} />
          ) : (
            <div className="nb-loading">Loading visual…</div>
          )}
        </article>
      ))}
      {error ? (
        <p className="nb-error" role="alert">
          {error}
        </p>
      ) : null}
    </section>
  );
}

function normalizeNotebookSourcePath(path: string): string | null {
  const trimmed = path.trim();
  const match = trimmed.match(/agents\/[^/]+\/notebook\/[^/]+\.md$/);
  return match?.[0] ?? null;
}
