import {
  SEV_ORDER,
  SEVERITY_TOKENS,
  type Severity,
} from "../../lib/types/lifecycle";

interface SeveritySummaryChipProps {
  counts: Record<Severity, number>;
}

/**
 * Compact chip rendered on each Inbox row showing how many grades land
 * in each severity tier. Skipped grades are omitted from the chip — the
 * Decision Packet view surfaces the timeout/skipped reviewer banner
 * instead, where the slot is action-relevant.
 *
 * Reads class/label/order from SEVERITY_TOKENS + SEV_ORDER so a future
 * tier rename or visual reshuffle happens in exactly one place.
 */
export function SeveritySummaryChip({ counts }: SeveritySummaryChipProps) {
  const present = SEV_ORDER.filter((sev) => counts[sev] > 0);
  if (present.length === 0) {
    return (
      <span className="severity-summary" aria-label="No reviewer grades yet">
        no grades
      </span>
    );
  }
  const ariaParts = present.map(
    (sev) => `${counts[sev]} ${SEVERITY_TOKENS[sev].label}`,
  );
  return (
    <span
      className="severity-summary"
      aria-label={`Grades: ${ariaParts.join(", ")}`}
    >
      {present.map((sev) => (
        <span key={sev} className={`sev ${SEVERITY_TOKENS[sev].cssClass}`}>
          <span className="dot" aria-hidden="true" />
          {counts[sev]}
        </span>
      ))}
    </span>
  );
}
