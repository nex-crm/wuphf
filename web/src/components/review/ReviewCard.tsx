import type { ReviewItem } from "../../api/notebook";
import { formatAgentName } from "../../lib/agentName";
import { formatRelativeTime } from "../../lib/format";
import { PixelAvatar } from "../ui/PixelAvatar";

/** One promotion card in the `/reviews` Kanban. */

interface ReviewCardProps {
  review: ReviewItem;
  active?: boolean;
  onOpen: (id: string) => void;
}

type DemandGrade = "urgent" | "waiting" | "fresh";

/** Hours a card must sit in a pending/in-review column to reach each tier. */
const GRADE_THRESHOLDS = { urgent: 48, waiting: 12 } as const;

function computeGrade(submittedTs: string, state: ReviewItem["state"]): DemandGrade | null {
  if (state === "approved" || state === "archived" || state === "rejected" || state === "expired") {
    return null;
  }
  const ageHours = (Date.now() - new Date(submittedTs).getTime()) / 3_600_000;
  if (ageHours >= GRADE_THRESHOLDS.urgent) return "urgent";
  if (ageHours >= GRADE_THRESHOLDS.waiting) return "waiting";
  return "fresh";
}

/**
 * Returns a fill ratio 0–1 representing how "stale" the card is, capped at
 * GRADE_THRESHOLDS.urgent * 2h. Used to render the timeout-fill bar.
 */
function computeTimeoutFill(submittedTs: string, state: ReviewItem["state"]): number {
  if (state === "approved" || state === "archived" || state === "rejected" || state === "expired") {
    return 0;
  }
  const ageHours = (Date.now() - new Date(submittedTs).getTime()) / 3_600_000;
  return Math.min(1, ageHours / (GRADE_THRESHOLDS.urgent * 2));
}

const GRADE_LABEL: Record<DemandGrade, string> = {
  urgent: "Urgent",
  waiting: "Waiting",
  fresh: "Fresh",
};

export default function ReviewCard({
  review,
  active,
  onOpen,
}: ReviewCardProps) {
  const grade = computeGrade(review.submitted_ts, review.state);
  const fillRatio = computeTimeoutFill(review.submitted_ts, review.state);
  const fillPct = `${Math.round(fillRatio * 100)}%`;

  return (
    <button
      type="button"
      className={`nb-review-card${active ? " is-active" : ""}${grade === "urgent" ? " is-timeout-urgent" : ""}`}
      onClick={() => onOpen(review.id)}
      aria-label={`Open review for ${review.entry_title}`}
      data-testid="nb-review-card"
      style={fillRatio > 0 ? ({ "--nb-timeout-fill": fillPct } as React.CSSProperties) : undefined}
    >
      {/* Timeout fill bar — grows left-to-right as the card ages. */}
      {fillRatio > 0 && (
        <span
          className="nb-review-card-timeout-bar"
          aria-hidden="true"
          style={{ width: fillPct }}
        />
      )}

      <div className="nb-review-card-title">{review.entry_title}</div>
      <div className="nb-review-card-excerpt">{review.excerpt}</div>
      <div className="nb-review-card-meta">
        <span className="nb-review-card-avatars" aria-hidden="true">
          <PixelAvatar slug={review.agent_slug} size={14} />
          <span>→</span>
          <PixelAvatar slug={review.reviewer_slug} size={14} />
        </span>
        <span className="nb-review-card-path">{review.proposed_wiki_path}</span>
        <span style={{ marginLeft: "auto" }}>
          {formatAgentName(review.agent_slug)} ·{" "}
          {formatRelativeTime(review.updated_ts)}
        </span>
      </div>

      {grade && grade !== "fresh" && (
        <div className={`nb-review-card-grade nb-review-card-grade--${grade}`}>
          {GRADE_LABEL[grade]}
        </div>
      )}
    </button>
  );
}
