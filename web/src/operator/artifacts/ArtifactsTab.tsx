// ArtifactsTab — the agent's outcomes, plural. A chip strip of everything the
// agent produced (its app, PDFs, HTML pages, markdown docs) with a viewer per
// type below. The app is one artifact among many, rendered by the host detail
// via `renderApp` (the real detail mounts the live sandboxed app; the mock detail
// mounts its request table). See web/src/operator/artifacts/artifacts.ts.

import { type ReactNode, useState } from "react";
import { Download, FileText } from "lucide-react";

import { ARTIFACT_BADGE, type Artifact } from "./artifacts";

interface ArtifactsTabProps {
  agentName: string;
  artifacts: Artifact[];
  /** Render the live app for an `app`-type artifact (host-owned). */
  renderApp?: (artifact: Artifact) => ReactNode;
}

export function ArtifactsTab({
  agentName,
  artifacts,
  renderApp,
}: ArtifactsTabProps) {
  const [selectedId, setSelectedId] = useState<string | null>(
    artifacts[0]?.id ?? null,
  );
  const selected = artifacts.find((a) => a.id === selectedId) ?? artifacts[0];

  if (artifacts.length === 0) {
    return (
      <div className="opr-empty-hint">
        Everything {agentName} produces collects here — apps, PDFs, pages, docs.
        Nothing yet.
      </div>
    );
  }

  return (
    <div className="opr-artifacts">
      <div
        className="opr-artifact-strip"
        role="tablist"
        aria-label="Artifacts this agent produced"
      >
        {artifacts.map((a) => (
          <button
            key={a.id}
            type="button"
            role="tab"
            aria-selected={selected?.id === a.id}
            className={`opr-artifact-chip${selected?.id === a.id ? " is-active" : ""}`}
            onClick={() => setSelectedId(a.id)}
          >
            <span className={`opr-artifact-badge is-${a.type}`}>
              {ARTIFACT_BADGE[a.type]}
            </span>
            <span className="opr-artifact-chip-body">
              <span className="opr-artifact-title">{a.title}</span>
              <span className="opr-artifact-meta">
                {a.producedBy} · {a.at}
              </span>
            </span>
          </button>
        ))}
      </div>

      {selected ? (
        <div className="opr-artifact-viewer">
          <ArtifactViewer artifact={selected} renderApp={renderApp} />
        </div>
      ) : null}
    </div>
  );
}

function ArtifactViewer({
  artifact,
  renderApp,
}: {
  artifact: Artifact;
  renderApp?: (artifact: Artifact) => ReactNode;
}) {
  switch (artifact.type) {
    case "app":
      return (
        <>
          {renderApp?.(artifact) ?? (
            <div className="opr-empty-hint">
              This app has no live preview here.
            </div>
          )}
        </>
      );
    case "md":
      return (
        <pre className="opr-artifact-md">
          <code>{artifact.content ?? ""}</code>
        </pre>
      );
    case "html":
      // Fully sandboxed: produced HTML renders inert (no scripts, no navigation).
      return (
        <iframe
          className="opr-artifact-html"
          title={artifact.title}
          sandbox=""
          srcDoc={artifact.content ?? ""}
        />
      );
    case "pdf":
      return (
        <div className="opr-artifact-file">
          <span className="opr-artifact-file-glyph" aria-hidden={true}>
            <FileText size={22} strokeWidth={1.6} />
          </span>
          <div className="opr-artifact-file-body">
            <div className="opr-artifact-title">{artifact.title}</div>
            <div className="opr-artifact-meta">
              {artifact.size ?? "PDF"} · {artifact.producedBy} · {artifact.at}
            </div>
          </div>
          <button type="button" className="opr-btn opr-btn-sm">
            <Download size={13} strokeWidth={1.9} aria-hidden={true} />
            Download
          </button>
        </div>
      );
    default:
      return null;
  }
}
