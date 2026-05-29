import type { CSSProperties } from "react";

import type { SchedulerJob } from "../../../api/client";
import { formatRelativeTime } from "../../../lib/format";
import { resolveObjectRoute } from "../../../lib/objectRoutes";
import type { routineOwner } from "./routineModel";

/**
 * Bits of the Routines detail surface shared between the page shell and
 * each tab module. Lives in a tiny module instead of being re-exported
 * from `RoutineDetailRoute.tsx` so the lint file-size cap doesn't fight
 * us as the page grows.
 */

export const detailButtonStyle: CSSProperties = {
  padding: "5px var(--space-3)",
  fontSize: "var(--text-sm)",
  fontWeight: 500,
  border: "1px solid var(--border)",
  borderRadius: "var(--radius-sm)",
  background: "var(--bg-card)",
  color: "var(--text-secondary)",
  cursor: "pointer",
  transition: "background 120ms ease, border-color 120ms ease",
};

interface SectionProps {
  title: string;
  children: React.ReactNode;
}

/** Tab-level section with the small uppercase eyebrow heading. */
export function Section({ title, children }: SectionProps) {
  return (
    <section
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--space-2)",
      }}
    >
      <h2
        style={{
          fontSize: "var(--text-2xs)",
          fontWeight: 600,
          textTransform: "uppercase",
          letterSpacing: "0.12em",
          color: "var(--text-tertiary)",
          margin: 0,
          fontFamily: "var(--font-mono)",
        }}
      >
        {title}
      </h2>
      {children}
    </section>
  );
}

export function scheduleSubtext(routine: SchedulerJob): string {
  const next = routine.next_run || routine.due_at;
  if (!next) return "No next run scheduled";
  return `Next run ${formatRelativeTime(next)} · ${new Date(next).toLocaleString()}`;
}

interface OwnerSummaryProps {
  owner: ReturnType<typeof routineOwner>;
}

export function OwnerSummary({ owner }: OwnerSummaryProps) {
  if (owner.kind === "system") {
    return (
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: "var(--space-1)",
        }}
      >
        <span
          className="badge badge-neutral"
          style={{ alignSelf: "flex-start" }}
        >
          system
        </span>
        <span
          style={{ fontSize: "var(--text-sm)", color: "var(--text-tertiary)" }}
        >
          Managed by the broker run-loop. Disable to pause it.
        </span>
      </div>
    );
  }
  if (owner.kind === "workflow") {
    return (
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: "var(--space-1)",
        }}
      >
        <span
          className="badge badge-yellow"
          style={{ alignSelf: "flex-start" }}
        >
          workflow
        </span>
        <span
          style={{ fontSize: "var(--text-sm)", color: "var(--text-tertiary)" }}
        >
          Executed by the workflow runner.
        </span>
      </div>
    );
  }
  if (owner.kind === "unassigned" || !owner.slug) {
    return (
      <span
        style={{ fontSize: "var(--text-sm)", color: "var(--text-tertiary)" }}
      >
        Unassigned
      </span>
    );
  }
  const route = resolveObjectRoute({ kind: "agent", slug: owner.slug });
  return (
    <a
      href={route.href}
      style={{
        fontSize: "var(--text-lg)",
        color: "var(--accent)",
        textDecoration: "none",
        fontWeight: 500,
      }}
    >
      {owner.slug}
    </a>
  );
}

interface InstructionsProps {
  routine: SchedulerJob;
}

export function Instructions({ routine }: InstructionsProps) {
  const rows: Array<{ label: string; value: string | undefined }> = [
    { label: "Kind", value: routine.kind },
    { label: "Target type", value: routine.target_type },
    { label: "Target id", value: routine.target_id },
    { label: "Provider", value: routine.provider },
  ];
  const populated = rows.filter(
    (r) => typeof r.value === "string" && r.value.trim() !== "",
  );
  const payload = routine.payload?.trim();

  if (populated.length === 0 && !payload) {
    return (
      <div
        style={{ fontSize: "var(--text-sm)", color: "var(--text-tertiary)" }}
      >
        No additional instructions on file. The agent infers what to do from the
        routine slug.
      </div>
    );
  }

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--space-2)",
        padding: "var(--space-4)",
        background: "var(--bg-card)",
        border: "1px solid var(--border-light)",
        borderRadius: "var(--radius-sm)",
      }}
    >
      {populated.map((row) => (
        <div
          key={row.label}
          style={{
            display: "grid",
            gridTemplateColumns: "140px 1fr",
            gap: "var(--space-3)",
            fontSize: "var(--text-sm)",
          }}
        >
          <span
            style={{
              color: "var(--text-tertiary)",
              fontFamily: "var(--font-mono)",
              fontSize: "var(--text-xs)",
            }}
          >
            {row.label}
          </span>
          <span
            style={{ color: "var(--text)", fontFamily: "var(--font-mono)" }}
          >
            {row.value}
          </span>
        </div>
      ))}
      {payload && (
        <details style={{ marginTop: "var(--space-2)" }}>
          <summary
            style={{
              fontSize: "var(--text-xs)",
              color: "var(--text-tertiary)",
              cursor: "pointer",
              fontFamily: "var(--font-mono)",
            }}
          >
            Payload
          </summary>
          <pre
            style={{
              margin: "var(--space-2) 0 0",
              padding: "var(--space-3)",
              background: "var(--bg)",
              border: "1px solid var(--border-light)",
              borderRadius: "var(--radius-sm)",
              fontSize: "var(--text-xs)",
              fontFamily: "var(--font-mono)",
              whiteSpace: "pre-wrap",
              wordBreak: "break-word",
            }}
          >
            {prettyPayload(payload)}
          </pre>
        </details>
      )}
    </div>
  );
}

export function prettyPayload(raw: string): string {
  try {
    const parsed: unknown = JSON.parse(raw);
    return JSON.stringify(parsed, null, 2);
  } catch {
    return raw;
  }
}
