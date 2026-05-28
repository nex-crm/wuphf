import { useState } from "react";

import type { SchedulerRun } from "../../../api/client";
import { formatRelativeTime } from "../../../lib/format";
import { resolveObjectRoute } from "../../../lib/objectRoutes";

/**
 * Runs tab body: expandable rows showing status / timing on the
 * surface and a rich "what happened" panel (events, summary, target
 * link, error) on click.
 */

interface PreviousRunsProps {
  runs: SchedulerRun[] | undefined;
  loading: boolean;
  error?: boolean;
  fallbackLastRun?: string;
  fallbackStatus?: string;
}

export function PreviousRuns({
  runs,
  loading,
  error,
  fallbackLastRun,
  fallbackStatus,
}: PreviousRunsProps) {
  if (loading) {
    return (
      <div
        style={{ fontSize: "var(--text-sm)", color: "var(--text-tertiary)" }}
      >
        Loading run history…
      </div>
    );
  }

  if (error) {
    return (
      <div
        role="alert"
        style={{ fontSize: "var(--text-sm)", color: "var(--red)" }}
      >
        Could not load run history. The broker may be unreachable; the empty
        state here would otherwise look like real data.
      </div>
    );
  }

  if ((!runs || runs.length === 0) && fallbackLastRun) {
    const fallback: SchedulerRun = {
      slug: "",
      started_at: fallbackLastRun,
      status: fallbackStatus || "ok",
    };
    return <RunRows runs={[fallback]} />;
  }

  if (!runs || runs.length === 0) {
    return (
      <div
        style={{ fontSize: "var(--text-sm)", color: "var(--text-tertiary)" }}
      >
        No runs recorded yet.
      </div>
    );
  }

  return <RunRows runs={runs} />;
}

function runRowKey(run: SchedulerRun): string {
  return `${run.started_at}-${run.status}-${run.triggered_by ?? ""}`;
}

function RunRows({ runs }: { runs: SchedulerRun[] }) {
  return (
    <div
      data-testid="routine-runs"
      style={{
        display: "flex",
        flexDirection: "column",
        border: "1px solid var(--border-light)",
        borderRadius: "var(--radius-sm)",
        overflow: "hidden",
        background: "var(--bg-card)",
      }}
    >
      {runs.map((run, idx) => (
        <RunRow key={runRowKey(run)} run={run} isFirst={idx === 0} />
      ))}
    </div>
  );
}

interface RunStatusVisual {
  glyph: string;
  label: string;
  tone: "green" | "red" | "neutral" | "yellow";
}

function describeRunStatus(raw: string): RunStatusVisual {
  const s = (raw || "").toLowerCase();
  if (s === "ok" || s === "success" || s === "completed" || s === "done") {
    return { glyph: "✓", label: "Completed", tone: "green" };
  }
  if (s === "failed" || s === "error") {
    return { glyph: "✗", label: "Failed", tone: "red" };
  }
  if (s === "triggered") {
    return { glyph: "⟳", label: "Triggered", tone: "yellow" };
  }
  if (s === "running" || s === "in_progress") {
    return { glyph: "•", label: "Running", tone: "yellow" };
  }
  return { glyph: "—", label: raw || "Unknown", tone: "neutral" };
}

function RunRow({ run, isFirst }: { run: SchedulerRun; isFirst: boolean }) {
  const [open, setOpen] = useState(isFirst);
  const status = describeRunStatus(run.status);
  const start = new Date(run.started_at);
  const finished = run.finished_at ? new Date(run.finished_at) : null;
  const duration =
    finished && !Number.isNaN(start.getTime())
      ? formatDuration(start, finished)
      : null;

  return (
    <div
      data-testid="routine-run-row"
      data-run-status={status.tone}
      style={{ borderBottom: "1px solid var(--border-light)" }}
    >
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        style={{
          display: "grid",
          gridTemplateColumns: "120px 1fr auto auto",
          gap: "var(--space-3)",
          alignItems: "center",
          padding: "var(--space-3) var(--space-4)",
          fontSize: "var(--text-sm)",
          width: "100%",
          background: "transparent",
          border: "none",
          cursor: "pointer",
          color: "inherit",
          textAlign: "left",
        }}
      >
        <span
          className={`badge badge-${status.tone}`}
          style={{
            display: "inline-flex",
            alignItems: "center",
            gap: 4,
            justifySelf: "start",
          }}
        >
          <span aria-hidden="true">{status.glyph}</span> {status.label}
        </span>
        <div style={{ minWidth: 0 }}>
          <div style={{ color: "var(--text)" }}>
            {Number.isNaN(start.getTime())
              ? run.started_at
              : formatRelativeTime(run.started_at)}
          </div>
          {(run.output_summary || run.message) && (
            <div
              style={{
                fontSize: "var(--text-xs)",
                color: "var(--text-tertiary)",
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
              }}
              title={run.output_summary || run.message}
            >
              {run.output_summary || run.message}
            </div>
          )}
        </div>
        <span
          style={{
            fontSize: "var(--text-xs)",
            color: "var(--text-tertiary)",
            fontFamily: "var(--font-mono)",
          }}
        >
          {duration ?? "—"}
        </span>
        <span
          aria-hidden="true"
          style={{
            fontSize: "var(--text-xs)",
            color: "var(--text-tertiary)",
            transform: open ? "rotate(90deg)" : "none",
            transition: "transform 120ms ease",
            display: "inline-block",
            width: 10,
          }}
        >
          ▸
        </span>
      </button>

      {open && <RunDetailPanel run={run} />}
    </div>
  );
}

function RunDetailPanel({ run }: { run: SchedulerRun }) {
  const start = new Date(run.started_at);
  const finished = run.finished_at ? new Date(run.finished_at) : null;
  const targetHref = targetLinkFor(run);

  return (
    <div
      data-testid="routine-run-detail"
      style={{
        padding: "var(--space-4)",
        background: "var(--bg)",
        borderTop: "1px solid var(--border-light)",
        display: "flex",
        flexDirection: "column",
        gap: "var(--space-3)",
        fontSize: "var(--text-sm)",
      }}
    >
      <div
        style={{
          display: "grid",
          gridTemplateColumns: "140px 1fr",
          gap: "var(--space-3) var(--space-4)",
          rowGap: "var(--space-2)",
        }}
      >
        <DetailRow label="Started">
          {Number.isNaN(start.getTime())
            ? run.started_at
            : start.toLocaleString()}
        </DetailRow>
        {finished && (
          <DetailRow label="Finished">{finished.toLocaleString()}</DetailRow>
        )}
        {run.triggered_by && (
          <DetailRow label="Triggered by">
            <span style={{ fontFamily: "var(--font-mono)" }}>
              {run.triggered_by}
            </span>
          </DetailRow>
        )}
        {(run.target_type || run.target_id) && (
          <DetailRow label="Target">
            {targetHref ? (
              <a
                href={targetHref}
                style={{
                  color: "var(--accent)",
                  textDecoration: "none",
                  fontFamily: "var(--font-mono)",
                }}
              >
                {run.target_type}:{run.target_id}
              </a>
            ) : (
              <span style={{ fontFamily: "var(--font-mono)" }}>
                {[run.target_type, run.target_id].filter(Boolean).join(":")}
              </span>
            )}
          </DetailRow>
        )}
      </div>

      {run.output_summary && (
        <DetailBlock label="Summary">
          <div style={{ color: "var(--text)", lineHeight: 1.55 }}>
            {run.output_summary}
          </div>
        </DetailBlock>
      )}

      {run.events && run.events.length > 0 && (
        <DetailBlock label="Events">
          <ol
            style={{
              margin: 0,
              padding: 0,
              listStyle: "none",
              display: "flex",
              flexDirection: "column",
              gap: 2,
            }}
          >
            {run.events.map((ev, i) => (
              <li
                key={`${i}-${ev.slice(0, 24)}`}
                style={{
                  display: "grid",
                  gridTemplateColumns: "24px 1fr",
                  gap: "var(--space-2)",
                  fontFamily: "var(--font-mono)",
                  fontSize: "var(--text-xs)",
                  color: "var(--text-secondary)",
                }}
              >
                <span style={{ color: "var(--text-tertiary)" }}>
                  {String(i + 1).padStart(2, "0")}
                </span>
                <span>{ev}</span>
              </li>
            ))}
          </ol>
        </DetailBlock>
      )}

      {run.error && (
        <DetailBlock label="Error" tone="red">
          <pre
            style={{
              margin: 0,
              padding: "var(--space-3)",
              background: "var(--red-bg)",
              color: "var(--red)",
              border: "1px solid var(--red)",
              borderRadius: "var(--radius-sm)",
              fontSize: "var(--text-xs)",
              fontFamily: "var(--font-mono)",
              whiteSpace: "pre-wrap",
              wordBreak: "break-word",
            }}
          >
            {run.error}
          </pre>
        </DetailBlock>
      )}

      {run.message && !run.output_summary && !run.error && (
        <DetailBlock label="Message">
          <div style={{ color: "var(--text-secondary)" }}>{run.message}</div>
        </DetailBlock>
      )}

      {!(run.output_summary || run.error || run.message) &&
        (!run.events || run.events.length === 0) && (
          <div
            style={{
              padding: "var(--space-3) 0",
              fontSize: "var(--text-xs)",
              color: "var(--text-tertiary)",
              fontStyle: "italic",
            }}
          >
            The runner didn't emit a detail trace for this fire. Status and
            timing above are all we have on file.
          </div>
        )}
    </div>
  );
}

function DetailRow({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <>
      <span
        style={{
          color: "var(--text-tertiary)",
          fontFamily: "var(--font-mono)",
          fontSize: "var(--text-xs)",
        }}
      >
        {label}
      </span>
      <span style={{ color: "var(--text)" }}>{children}</span>
    </>
  );
}

function DetailBlock({
  label,
  tone,
  children,
}: {
  label: string;
  tone?: "red";
  children: React.ReactNode;
}) {
  return (
    <section style={{ display: "flex", flexDirection: "column", gap: 4 }}>
      <h4
        style={{
          margin: 0,
          fontSize: "var(--text-2xs)",
          fontWeight: 600,
          textTransform: "uppercase",
          letterSpacing: "0.12em",
          color: tone === "red" ? "var(--red)" : "var(--text-tertiary)",
          fontFamily: "var(--font-mono)",
        }}
      >
        {label}
      </h4>
      {children}
    </section>
  );
}

/**
 * Resolve a click-through link for the run's target. Workflow runs
 * deep-link into the workflow registry; agent runs into the agent
 * profile. Everything else falls back to a plain text rendering.
 */
function targetLinkFor(run: SchedulerRun): string | null {
  if (!(run.target_id && run.target_type)) return null;
  if (run.target_type === "agent") {
    const route = resolveObjectRoute({ kind: "agent", slug: run.target_id });
    return route.href;
  }
  if (run.target_type === "workflow") {
    return `#/apps/skills?workflow=${encodeURIComponent(run.target_id)}`;
  }
  return null;
}

function formatDuration(start: Date, end: Date): string {
  const ms = end.getTime() - start.getTime();
  if (ms < 0) return "—";
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  // Round-then-split so 119.6s renders as "2m 0s" instead of "1m 60s".
  // Flooring minutes first leaves rounded seconds free to hit 60.
  const totalSeconds = Math.round(ms / 1000);
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return `${minutes}m ${seconds}s`;
}
