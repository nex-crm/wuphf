import ReactMarkdown from "react-markdown";

import {
  messageMarkdownComponents,
  messageRemarkPlugins,
} from "../../lib/messageMarkdown";

// ── Linear-style description ──────────────────────────────────────────

interface IssueDescriptionProps {
  description: string;
  isDrafting: boolean;
}

export function IssueDescription({ description, isDrafting }: IssueDescriptionProps) {
  const body = description.trim();
  if (!body) {
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
    <section
      className="issue-doc-description"
      aria-label="Description"
    >
      <div className="issue-doc-description-body">
        <ReactMarkdown
          remarkPlugins={messageRemarkPlugins}
          components={messageMarkdownComponents}
        >
          {body}
        </ReactMarkdown>
      </div>
    </section>
  );
}
