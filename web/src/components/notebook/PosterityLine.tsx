import { formatAgentName } from "../../lib/agentName";

/**
 * Italic, muted footer line at the bottom of the entry article. Calls out
 * that this file is private to the author until promotion and names the
 * reviewer, matching DESIGN-NOTEBOOK.md Posterity-line section.
 */

interface PosterityLineProps {
  authorSlug: string;
  reviewerSlug: string;
  filePath: string;
}

export default function PosterityLine({
  authorSlug,
  reviewerSlug,
  filePath,
}: PosterityLineProps) {
  const reviewerLabel =
    reviewerSlug === "human-only" ? "a human" : formatAgentName(reviewerSlug);
  return (
    <p className="nb-posterity">
      This entry is private to <strong>{formatAgentName(authorSlug)}</strong>{" "}
      until it is promoted. Reviewer for promotion:{" "}
      <strong>{reviewerLabel}</strong>. File lives at{" "}
      <span className="nb-posterity-path">{filePath}</span>.
    </p>
  );
}
