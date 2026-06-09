import { useMemo } from "react";
import ReactMarkdown from "react-markdown";

import { useInlineArtifacts } from "../../hooks/useInlineArtifacts";
import {
  messageMarkdownComponents,
  messageRemarkPlugins,
} from "../../lib/messageMarkdown";
import { keyedByOccurrence } from "../../lib/reactKeys";
import { stripStandaloneRichArtifactReferenceLines } from "../../lib/richArtifactReferences";
import RichArtifactEmbed from "../rich-artifacts/RichArtifactEmbed";

// ── Linear-style description ──────────────────────────────────────────

interface TaskDescriptionProps {
  description: string;
  isDrafting: boolean;
}

/**
 * TaskDescription renders the Issue's spec body. When the agent has
 * dropped a `visual-artifact:<id>` marker into the description (the same
 * way wiki articles and notebook entries reference rich HTML companions),
 * the marker is stripped from the markdown body and the underlying
 * RichArtifactEmbed renders inline ABOVE the remaining prose, in
 * document order.
 *
 * Mirrors the wiki + notebook surfaces' embed pattern via the shared
 * useInlineArtifacts hook so the Making-Software / technical-manual
 * aesthetic looks identical wherever the agent emitted an HTML spec.
 * A 404 (or any fetch failure) for a referenced artifact degrades to
 * nothing visible — the stripped marker keeps the raw `visual-artifact:`
 * text out of the body either way.
 */
export function TaskDescription({
  description,
  isDrafting,
}: TaskDescriptionProps) {
  const body = description.trim();
  const inlineArtifacts = useInlineArtifacts(body || null);
  const renderedBody = useMemo(
    () => stripStandaloneRichArtifactReferenceLines(body),
    [body],
  );
  const hasMarkdown = renderedBody.length > 0;
  const hasArtifacts = inlineArtifacts.length > 0;

  if (!(body && (hasMarkdown || hasArtifacts))) {
    return (
      <section
        className="issue-doc-description issue-doc-description--empty"
        aria-label="Description"
      >
        <p className="issue-doc-description-empty-line">
          {isDrafting
            ? "No description yet. Add one in chat — the CEO will fill this out as the spec firms up."
            : "No description."}
        </p>
      </section>
    );
  }

  return (
    <section className="issue-doc-description" aria-label="Description">
      <div
        className="issue-doc-description-body"
        data-testid="issue-doc-description-body"
      >
        {keyedByOccurrence(inlineArtifacts, (detail) => detail.artifact.id).map(
          ({ key, value: detail }) => (
            <RichArtifactEmbed
              key={key}
              title={detail.artifact.title}
              html={detail.html}
            />
          ),
        )}
        {hasMarkdown ? (
          <ReactMarkdown
            remarkPlugins={messageRemarkPlugins}
            components={messageMarkdownComponents}
          >
            {renderedBody}
          </ReactMarkdown>
        ) : null}
      </div>
    </section>
  );
}
