import type { NotebookEntryStatus } from "../../api/notebook";
import { formatAgentName } from "../../lib/agentName";
import { formatRelativeTime } from "../../lib/format";
import { PixelAvatar } from "../ui/PixelAvatar";

/**
 * Sticky byline strip below the entry title. Shows pixel avatar + status pill
 * + agent name + last-edited meta. Sticks below the app bar so the DRAFT
 * signal stays visible after the user scrolls past the big red stamp.
 */

interface ByLineStripProps {
  authorSlug: string;
  status: NotebookEntryStatus;
  lastEditedTs: string;
  revisions: number;
  reviewerSlug?: string;
}

function statusMeta(status: NotebookEntryStatus): {
  label: string;
  className: string;
  aria: string;
} {
  switch (status) {
    case "promoted":
      return {
        label: "Promoted",
        className: "nb-promoted-pill",
        aria: "Promoted to wiki",
      };
    case "in-review":
      return {
        label: "In review",
        className: "nb-review-pill",
        aria: "Awaiting review",
      };
    case "changes-requested":
      return {
        label: "Changes",
        className: "nb-review-pill",
        aria: "Changes requested",
      };
    case "discarded":
      return {
        label: "Discarded",
        className: "nb-draft-pill",
        aria: "Discarded draft",
      };
    default:
      return {
        label: "Draft",
        className: "nb-draft-pill",
        aria: "Draft, not yet reviewed",
      };
  }
}

export default function ByLineStrip({
  authorSlug,
  status,
  lastEditedTs,
  revisions,
  reviewerSlug,
}: ByLineStripProps) {
  const meta = statusMeta(status);
  const aria = `Entry by ${formatAgentName(authorSlug)}, ${meta.aria}`;
  return (
    <div className="nb-byline" aria-label={aria}>
      <PixelAvatar slug={authorSlug} size={14} />
      <span className={meta.className} aria-hidden="true">
        {meta.label}
      </span>
      <strong>{formatAgentName(authorSlug)}</strong>
      <span>·</span>
      <span>
        last edited {formatRelativeTime(lastEditedTs)} · {revisions} revision
        {revisions === 1 ? "" : "s"}
      </span>
      <span className="nb-byline-meta">
        {status === "promoted"
          ? "Promoted"
          : status === "in-review"
            ? `Reviewing: ${formatAgentName(reviewerSlug ?? "ceo")}`
            : status === "changes-requested"
              ? "Changes requested"
              : "Not yet reviewed"}
      </span>
    </div>
  );
}
