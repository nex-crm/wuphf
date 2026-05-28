import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  getSchedulerRevisions,
  restoreSchedulerRevision,
  type SchedulerRevision,
} from "../../../api/client";
import { formatRelativeTime } from "../../../lib/format";
import { Section } from "./routineDetailShared";

interface RoutineRevisionsTabProps {
  slug: string;
}

/**
 * Revisions tab: paginated list of saved snapshots with view + restore
 * + diff against current. Restoring a non-current row PATCHes the
 * broker, which records a clean copy of the restored state as the new
 * top revision and emits an activity event.
 */
export function RoutineRevisionsTab({ slug }: RoutineRevisionsTabProps) {
  const queryClient = useQueryClient();
  const [selected, setSelected] = useState<number | null>(null);
  const query = useQuery({
    queryKey: ["scheduler-revisions", slug],
    queryFn: () => getSchedulerRevisions(slug),
    enabled: slug !== "",
    refetchInterval: 30_000,
  });
  const restoreMutation = useMutation({
    mutationFn: (version: number) => restoreSchedulerRevision(slug, version),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["scheduler"] });
      void queryClient.invalidateQueries({
        queryKey: ["scheduler-revisions", slug],
      });
      void queryClient.invalidateQueries({
        queryKey: ["scheduler-activity", slug],
      });
    },
  });

  if (query.isLoading) {
    return (
      <Section title="Revisions">
        <div
          style={{ fontSize: "var(--text-sm)", color: "var(--text-tertiary)" }}
        >
          Loading revisions…
        </div>
      </Section>
    );
  }
  const revisions = query.data ?? [];
  if (revisions.length === 0) {
    return (
      <Section title="Revisions">
        <div
          style={{ fontSize: "var(--text-sm)", color: "var(--text-tertiary)" }}
        >
          No revisions saved yet. A revision is created the first time the
          routine is saved and on every subsequent edit.
        </div>
      </Section>
    );
  }
  const current = revisions[0];
  const selectedRev =
    selected !== null
      ? (revisions.find((r) => r.version === selected) ?? null)
      : null;
  return (
    <Section title="Revisions">
      <div
        data-testid="routine-revisions-list"
        style={{
          display: "flex",
          flexDirection: "column",
          border: "1px solid var(--border-light)",
          borderRadius: "var(--radius-sm)",
          overflow: "hidden",
          background: "var(--bg-card)",
        }}
      >
        {revisions.map((rev) => {
          const isCurrent = rev.version === current.version;
          const isOpen = selected === rev.version;
          return (
            <RevisionRow
              key={rev.version}
              rev={rev}
              isCurrent={isCurrent}
              isOpen={isOpen}
              onToggle={() => setSelected(isOpen ? null : rev.version)}
              onRestore={() => restoreMutation.mutate(rev.version)}
              restorePending={restoreMutation.isPending}
              current={current}
            />
          );
        })}
      </div>
      {selectedRev && selectedRev.version !== current.version && (
        <div
          style={{
            fontSize: "var(--text-xs)",
            color: "var(--text-tertiary)",
            marginTop: "var(--space-2)",
          }}
        >
          Comparing v{selectedRev.version} against v{current.version} (current).
          Restoring saves the restored state as a new top revision.
        </div>
      )}
    </Section>
  );
}

interface RevisionRowProps {
  rev: SchedulerRevision;
  isCurrent: boolean;
  isOpen: boolean;
  onToggle: () => void;
  onRestore: () => void;
  restorePending: boolean;
  current: SchedulerRevision;
}

function RevisionRow({
  rev,
  isCurrent,
  isOpen,
  onToggle,
  onRestore,
  restorePending,
  current,
}: RevisionRowProps) {
  return (
    <div style={{ borderBottom: "1px solid var(--border-light)" }}>
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={isOpen}
        style={{
          display: "grid",
          gridTemplateColumns: "80px 1fr auto auto",
          gap: "var(--space-3)",
          alignItems: "center",
          padding: "var(--space-3) var(--space-4)",
          width: "100%",
          background: "transparent",
          border: "none",
          cursor: "pointer",
          textAlign: "left",
        }}
      >
        <span className={`badge badge-${isCurrent ? "green" : "neutral"}`}>
          v{rev.version}
        </span>
        <div style={{ minWidth: 0 }}>
          <div style={{ color: "var(--text)", fontSize: "var(--text-sm)" }}>
            {rev.change_note || "(no change note)"}
          </div>
          <div
            style={{
              fontSize: "var(--text-xs)",
              color: "var(--text-tertiary)",
            }}
          >
            {formatRelativeTime(rev.created_at)}
            {rev.author && ` · ${rev.author}`}
            {isCurrent && " · current"}
          </div>
        </div>
        {!isCurrent && (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              onRestore();
            }}
            disabled={restorePending}
            style={{
              padding: "3px var(--space-2)",
              fontSize: "var(--text-xs)",
              fontWeight: 500,
              border: "1px solid var(--border)",
              borderRadius: "var(--radius-sm)",
              background: "var(--bg)",
              color: "var(--text-secondary)",
              cursor: "pointer",
            }}
          >
            Restore
          </button>
        )}
        <span
          aria-hidden="true"
          style={{
            fontSize: "var(--text-xs)",
            color: "var(--text-tertiary)",
            transform: isOpen ? "rotate(90deg)" : "none",
            transition: "transform 120ms ease",
            width: 10,
          }}
        >
          ▸
        </span>
      </button>
      {isOpen && <RevisionDiff current={current} target={rev} />}
    </div>
  );
}

interface RevisionDiffProps {
  current: SchedulerRevision;
  target: SchedulerRevision;
}

function RevisionDiff({ current, target }: RevisionDiffProps) {
  const rows: Array<{
    label: string;
    current: string;
    target: string;
    changed: boolean;
  }> = [
    diffRow("Label", current.label, target.label),
    diffRow(
      "Schedule expr",
      current.schedule_expr ?? "",
      target.schedule_expr ?? "",
    ),
    diffRow(
      "Interval (min)",
      String(current.interval_minutes ?? 0),
      String(target.interval_minutes ?? 0),
    ),
    diffRow(
      "Target",
      `${current.target_type ?? ""}:${current.target_id ?? ""}`,
      `${target.target_type ?? ""}:${target.target_id ?? ""}`,
    ),
    diffRow("Payload", current.payload ?? "", target.payload ?? ""),
    diffRow(
      "Enabled",
      current.enabled ? "true" : "false",
      target.enabled ? "true" : "false",
    ),
  ];
  return (
    <div
      data-testid="routine-revision-diff"
      style={{
        padding: "var(--space-3) var(--space-4)",
        background: "var(--bg)",
        borderTop: "1px solid var(--border-light)",
        display: "flex",
        flexDirection: "column",
        gap: "var(--space-2)",
        fontSize: "var(--text-sm)",
      }}
    >
      {rows.map((row) => (
        <DiffRowView key={row.label} row={row} />
      ))}
    </div>
  );
}

interface DiffRow {
  label: string;
  current: string;
  target: string;
  changed: boolean;
}

function DiffRowView({ row }: { row: DiffRow }) {
  return (
    <div
      style={{
        display: "grid",
        gridTemplateColumns: "140px 1fr 1fr",
        gap: "var(--space-2)",
        alignItems: "start",
        opacity: row.changed ? 1 : 0.5,
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
      <DiffValue
        value={row.target}
        eyebrow="this revision"
        changed={row.changed}
      />
      <DiffValue value={row.current} eyebrow="current" changed={row.changed} />
    </div>
  );
}

interface DiffValueProps {
  value: string;
  eyebrow: string;
  changed: boolean;
}

function DiffValue({ value, eyebrow, changed }: DiffValueProps) {
  return (
    <span
      style={{
        color: changed ? "var(--text)" : "var(--text-tertiary)",
        fontFamily: "var(--font-mono)",
        fontSize: "var(--text-xs)",
        whiteSpace: "pre-wrap",
        wordBreak: "break-word",
      }}
    >
      <em
        style={{
          color: "var(--text-tertiary)",
          fontFamily: "var(--font-sans)",
          marginRight: 6,
          fontStyle: "normal",
          fontSize: "var(--text-2xs)",
          textTransform: "uppercase",
          letterSpacing: "0.08em",
        }}
      >
        {eyebrow}
      </em>
      {value || "—"}
    </span>
  );
}

function diffRow(label: string, current: string, target: string): DiffRow {
  return { label, current, target, changed: current !== target };
}
