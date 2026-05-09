import type { CSSProperties } from "react";

import { formatRelativeTime } from "../../lib/format";
import { type ReviewerGrade, SEVERITY_TOKENS } from "../../lib/types/lifecycle";

interface SeverityGradeCardProps {
  grade: ReviewerGrade;
}

/**
 * One reviewer grade card on the Decision Packet center column.
 *
 * Severity is encoded by:
 *  - Border-left color (line-quick scan).
 *  - sev-pill background + text (high-contrast pill).
 *  - The plain-English severity tier label inside the pill (a11y).
 *
 * Color is NEVER the only signal — the label text always renders.
 */
export function SeverityGradeCard({ grade }: SeverityGradeCardProps) {
  const tokens = SEVERITY_TOKENS[grade.severity];
  const containerStyle: CSSProperties = {
    borderLeftColor: tokens.border,
    background: tokens.bg,
  };
  const pillStyle: CSSProperties = {
    background: tokens.pillBg,
    color: tokens.pillText,
  };
  const submittedAt = formatRelativeTime(grade.submittedAt);

  return (
    <article
      className={`packet-grade ${grade.severity === "skipped" ? "skipped" : ""}`}
      style={containerStyle}
      data-severity={grade.severity}
      aria-label={`${tokens.label} grade from ${grade.reviewerSlug}`}
    >
      <span className="sev-pill" style={pillStyle}>
        {tokens.label}
      </span>
      <div className="body">
        <div className="sugg">{grade.suggestion}</div>
        <div className="reason">{grade.reasoning}</div>
        {grade.filePath ? (
          <div className="file">
            {grade.filePath}
            {grade.line ? `:${grade.line}` : ""}
          </div>
        ) : null}
      </div>
      <div className="reviewer">
        {grade.reviewerSlug}
        <br />
        {grade.severity === "skipped" ? "—" : submittedAt || ""}
      </div>
    </article>
  );
}
