import { useEffect, useMemo, useState } from "react";

import {
  fetchRichArtifact,
  fetchRichArtifacts,
  promoteRichArtifact,
  type RichArtifact,
  type RichArtifactDetail,
} from "../../api/richArtifacts";
import RichArtifactFrame from "../rich-artifacts/RichArtifactFrame";

interface NotebookVisualArtifactsProps {
  agentSlug: string;
  entrySlug: string;
  sourcePath: string;
  onNavigateWiki?: (wikiPath: string) => void;
}

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
  const [artifacts, setArtifacts] = useState<RichArtifact[]>([]);
  const [detail, setDetail] = useState<RichArtifactDetail | null>(null);
  const [inlineDetail, setInlineDetail] = useState<RichArtifactDetail | null>(
    null,
  );
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [promoting, setPromoting] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setError(null);
    setInlineDetail(null);
    if (!canonicalSourcePath) {
      setArtifacts([]);
      return () => {
        cancelled = true;
      };
    }
    fetchRichArtifacts({ sourceMarkdownPath: canonicalSourcePath })
      .then(async (items) => {
        if (cancelled) return;
        setArtifacts(items);
        const [first] = items;
        if (!first) return;
        const firstDetail = await fetchRichArtifact(first.id);
        if (!cancelled) setInlineDetail(firstDetail);
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

  const activeArtifact = detail?.artifact ?? null;
  const defaultTarget = useMemo(
    () => `team/drafts/${agentSlug}-${entrySlug}-visual.md`,
    [agentSlug, entrySlug],
  );

  if (artifacts.length === 0 && !error) return null;

  async function openArtifact(artifact: RichArtifact) {
    setLoading(true);
    setError(null);
    try {
      setDetail(await fetchRichArtifact(artifact.id));
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to open artifact");
    } finally {
      setLoading(false);
    }
  }

  async function promoteActiveArtifact() {
    if (!activeArtifact) return;
    setPromoting(true);
    setError(null);
    try {
      const promoted = await promoteRichArtifact(activeArtifact.id, {
        targetWikiPath: defaultTarget,
        markdownSummary: `# ${activeArtifact.title}\n\n${activeArtifact.summary || "Promoted visual artifact."}\n`,
      });
      setArtifacts((items) =>
        items.map((item) => (item.id === promoted.id ? promoted : item)),
      );
      setDetail((current) =>
        current && current.artifact.id === promoted.id
          ? { ...current, artifact: promoted }
          : current,
      );
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Promotion failed");
    } finally {
      setPromoting(false);
    }
  }

  return (
    <section className="nb-visual-artifacts" aria-label="Visual artifacts">
      <div className="nb-visual-artifacts-head">
        <h2>Visual artifacts</h2>
        <span>{artifacts.length}</span>
      </div>
      <div className="nb-visual-artifact-list">
        {artifacts.map((artifact) => (
          <article key={artifact.id} className="nb-visual-artifact-card">
            <div>
              <h3>{artifact.title}</h3>
              {artifact.summary ? <p>{artifact.summary}</p> : null}
              <div className="rich-artifact-meta">
                <span>{artifact.trustLevel}</span>
                <span>{artifact.createdBy}</span>
              </div>
            </div>
            <div className="nb-visual-artifact-actions">
              {artifact.promotedWikiPath ? (
                <button
                  type="button"
                  onClick={() =>
                    onNavigateWiki?.(artifact.promotedWikiPath ?? "")
                  }
                >
                  Open wiki
                </button>
              ) : null}
              <button
                type="button"
                onClick={() => {
                  void openArtifact(artifact);
                }}
              >
                Open
              </button>
            </div>
          </article>
        ))}
      </div>
      {inlineDetail ? (
        <div
          className="rich-artifact-inline"
          data-testid="nb-visual-artifact-inline"
        >
          <div className="rich-artifact-inline-head">
            <h3>{inlineDetail.artifact.title}</h3>
            <div className="rich-artifact-meta">
              <span>{inlineDetail.artifact.trustLevel}</span>
              <span>{inlineDetail.artifact.htmlPath}</span>
            </div>
          </div>
          <RichArtifactFrame
            title={inlineDetail.artifact.title}
            html={inlineDetail.html}
          />
        </div>
      ) : null}
      {error ? (
        <p className="nb-error" role="alert">
          {error}
        </p>
      ) : null}
      {detail ? (
        <div className="rich-artifact-modal" role="dialog" aria-modal="true">
          <div className="rich-artifact-modal-bar">
            <div>
              <h2>{detail.artifact.title}</h2>
              <div className="rich-artifact-meta">
                <span>{detail.artifact.trustLevel}</span>
                <span>{detail.artifact.htmlPath}</span>
              </div>
            </div>
            <div className="rich-artifact-modal-actions">
              {detail.artifact.promotedWikiPath ? (
                <button
                  type="button"
                  onClick={() =>
                    onNavigateWiki?.(detail.artifact.promotedWikiPath ?? "")
                  }
                >
                  Open wiki
                </button>
              ) : (
                <button
                  type="button"
                  disabled={promoting}
                  onClick={() => {
                    void promoteActiveArtifact();
                  }}
                >
                  {promoting ? "Promoting..." : "Promote"}
                </button>
              )}
              <button type="button" onClick={() => setDetail(null)}>
                Close
              </button>
            </div>
          </div>
          {loading ? (
            <div className="nb-loading">Loading artifact...</div>
          ) : (
            <RichArtifactFrame
              title={detail.artifact.title}
              html={detail.html}
            />
          )}
        </div>
      ) : null}
    </section>
  );
}

function normalizeNotebookSourcePath(path: string): string | null {
  const trimmed = path.trim();
  const match = trimmed.match(/agents\/[^/]+\/notebook\/[^/]+\.md$/);
  return match?.[0] ?? null;
}
