import type { Severity } from "../../lib/types/lifecycle";

interface SeveritySummaryChipProps {
  counts: Record<Severity, number>;
}

const SEV_CLASS: Record<Exclude<Severity, "skipped">, string> = {
  critical: "crit",
  major: "maj",
  minor: "min",
  nitpick: "nit",
};

const SEV_LABEL: Record<Exclude<Severity, "skipped">, string> = {
  critical: "critical",
  major: "major",
  minor: "minor",
  nitpick: "nitpick",
};

const SEV_ORDER: ReadonlyArray<Exclude<Severity, "skipped">> = [
  "critical",
  "major",
  "minor",
  "nitpick",
];

/**
 * Compact chip rendered on each Inbox row showing how many grades land
 * in each severity tier. Skipped grades are omitted from the chip — the
 * Decision Packet view surfaces the timeout/skipped reviewer banner
 * instead, where the slot is action-relevant.
 */
export function SeveritySummaryChip({ counts }: SeveritySummaryChipProps) {
  const present = SEV_ORDER.filter((sev) => counts[sev] > 0);
  if (present.length === 0) {
    return (
      <output className="severity-summary" aria-label="No reviewer grades yet">
        no grades
      </output>
    );
  }
  const ariaParts = present.map((sev) => `${counts[sev]} ${SEV_LABEL[sev]}`);
  return (
    <output
      className="severity-summary"
      aria-label={`Grades: ${ariaParts.join(", ")}`}
    >
      {present.map((sev) => (
        <span key={sev} className={`sev ${SEV_CLASS[sev]}`}>
          <span className="dot" aria-hidden="true" />
          {counts[sev]}
        </span>
      ))}
    </output>
  );
}
