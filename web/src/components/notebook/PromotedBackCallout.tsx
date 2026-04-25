import type { PromotedBackLink } from "../../api/notebook";
import { formatAgentName } from "../../lib/agentName";
import { formatRelativeTime } from "../../lib/format";

/**
 * Inline muted-green callout shown where content from this entry was
 * previously promoted to the wiki. Click navigates cross-surface to
 * `/wiki/{promoted_to_path}`.
 */

interface PromotedBackCalloutProps {
  link: PromotedBackLink;
  onNavigate?: (wikiPath: string) => void;
}

export default function PromotedBackCallout({
  link,
  onNavigate,
}: PromotedBackCalloutProps) {
  return (
    <aside
      className="nb-promoted-back"
      role="note"
      aria-label={`Promoted to wiki: ${link.section}`}
    >
      <span className="nb-promoted-icon" aria-hidden="true">
        →
      </span>
      The <strong>{link.section}</strong> thread from these notes was promoted{" "}
      {formatRelativeTime(link.promoted_ts)} to{" "}
      <a
        href={`#/wiki/${encodeURI(link.promoted_to_path)}`}
        onClick={(e) => {
          if (onNavigate) {
            e.preventDefault();
            onNavigate(link.promoted_to_path);
          }
        }}
      >
        Team Wiki · {link.promoted_to_path}
      </a>{" "}
      by {formatAgentName(link.promoted_by_slug)}.
    </aside>
  );
}
