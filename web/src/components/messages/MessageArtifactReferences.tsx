import { type KeyboardEvent, useEffect, useRef, useState } from "react";
import { useQueries } from "@tanstack/react-query";

import {
  fetchRichArtifact,
  type RichArtifact,
  type RichArtifactDetail,
} from "../../api/richArtifacts";
import RichArtifactFrame from "../rich-artifacts/RichArtifactFrame";

const MODAL_FOCUSABLE_SELECTOR =
  'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])';

interface MessageArtifactReferencesProps {
  artifactIds: string[];
}

interface ArtifactReferenceItem {
  id: string;
  detail?: RichArtifactDetail;
  error?: string;
}

export default function MessageArtifactReferences({
  artifactIds,
}: MessageArtifactReferencesProps) {
  const [activeDetail, setActiveDetail] = useState<RichArtifactDetail | null>(
    null,
  );
  const dialogRef = useRef<HTMLDivElement | null>(null);
  const closeButtonRef = useRef<HTMLButtonElement | null>(null);
  const previousFocusRef = useRef<HTMLElement | null>(null);

  const artifactQueries = useQueries({
    queries: artifactIds.map((id) => ({
      queryKey: ["rich-artifact-reference", id],
      queryFn: () => fetchRichArtifact(id),
      staleTime: 60_000,
    })),
  });

  useEffect(() => {
    if (!activeDetail) return;
    previousFocusRef.current =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null;
    closeButtonRef.current?.focus();
    return () => {
      previousFocusRef.current?.focus();
      previousFocusRef.current = null;
    };
  }, [activeDetail]);

  if (artifactIds.length === 0) return null;

  function closeDialog() {
    setActiveDetail(null);
  }

  function handleDialogKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key === "Escape") {
      event.preventDefault();
      closeDialog();
      return;
    }
    if (event.key === "Tab") trapModalFocus(event, dialogRef.current);
  }

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
            item={{
              id,
              detail: result?.data,
              error: queryErrorMessage(result?.error),
            }}
            onOpen={(detail) => setActiveDetail(detail)}
          />
        );
      })}
      {activeDetail ? (
        <div
          ref={dialogRef}
          className="rich-artifact-modal message-artifact-modal"
          role="dialog"
          aria-modal="true"
          aria-labelledby={`message-artifact-modal-title-${activeDetail.artifact.id}`}
          onKeyDown={handleDialogKeyDown}
        >
          <div className="rich-artifact-modal-bar">
            <div>
              <h2
                id={`message-artifact-modal-title-${activeDetail.artifact.id}`}
              >
                {activeDetail.artifact.title}
              </h2>
              <div className="rich-artifact-meta">
                <span>{activeDetail.artifact.trustLevel}</span>
                <span>{activeDetail.artifact.htmlPath}</span>
              </div>
            </div>
            <div className="rich-artifact-modal-actions">
              {activeDetail.artifact.promotedWikiPath ? (
                <a href={wikiHref(activeDetail.artifact.promotedWikiPath)}>
                  Open wiki
                </a>
              ) : null}
              <button ref={closeButtonRef} type="button" onClick={closeDialog}>
                Close
              </button>
            </div>
          </div>
          <RichArtifactFrame
            title={activeDetail.artifact.title}
            html={activeDetail.html}
          />
        </div>
      ) : null}
    </section>
  );
}

function MessageArtifactReference({
  item,
  onOpen,
}: {
  item: ArtifactReferenceItem;
  onOpen: (detail: RichArtifactDetail) => void;
}) {
  if (item.error) {
    return (
      <article className="message-artifact-reference message-artifact-error">
        <div>
          <span className="message-artifact-kicker">HTML artifact</span>
          <h4>{item.id}</h4>
          <p>{item.error}</p>
        </div>
      </article>
    );
  }
  const { detail } = item;
  if (!detail) {
    return (
      <article className="message-artifact-reference" aria-busy="true">
        <div>
          <span className="message-artifact-kicker">HTML artifact</span>
          <h4>{item.id}</h4>
          <p>Loading artifact...</p>
        </div>
      </article>
    );
  }

  const { artifact } = detail;
  return (
    <article
      className="message-artifact-reference"
      aria-label={`Rich artifact: ${artifact.title}`}
    >
      <div className="message-artifact-main">
        <span className="message-artifact-kicker">
          {artifact.kind === "wiki_visual" ? "Wiki visual" : "Notebook visual"}
        </span>
        <h4>{artifact.title}</h4>
        {artifact.summary ? <p>{artifact.summary}</p> : null}
        <div className="message-artifact-meta">
          <span>{artifact.trustLevel}</span>
          <span>{artifactSource(artifact)}</span>
        </div>
      </div>
      <div className="message-artifact-actions">
        {artifact.promotedWikiPath ? (
          <a href={wikiHref(artifact.promotedWikiPath)}>Open wiki</a>
        ) : null}
        <button type="button" onClick={() => onOpen(detail)}>
          Open
        </button>
      </div>
    </article>
  );
}

function artifactSource(artifact: RichArtifact): string {
  if (artifact.promotedWikiPath) return artifact.promotedWikiPath;
  if (artifact.sourceMarkdownPath) return artifact.sourceMarkdownPath;
  return `@${artifact.createdBy}`;
}

function queryErrorMessage(error: unknown): string | undefined {
  if (!error) return undefined;
  return error instanceof Error ? error.message : "Failed to load artifact";
}

function wikiHref(path: string): string {
  return `#/wiki/${encodeURI(path)}`;
}

function trapModalFocus(
  event: KeyboardEvent<HTMLDivElement>,
  dialog: HTMLDivElement | null,
) {
  const focusables = Array.from(
    dialog?.querySelectorAll<HTMLElement>(MODAL_FOCUSABLE_SELECTOR) ?? [],
  ).filter((element) => !element.hasAttribute("disabled"));
  if (focusables.length === 0) {
    event.preventDefault();
    return;
  }
  const [first] = focusables;
  const last = focusables.at(-1);
  if (!(first && last)) return;
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first.focus();
  }
}
