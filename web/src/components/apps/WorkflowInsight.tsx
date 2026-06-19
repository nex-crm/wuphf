import { useState } from "react";
import { useQuery } from "@tanstack/react-query";

import {
  getWorkflowRuns,
  getWorkflowSpec,
  type RunRecord,
  type WorkflowTrigger,
} from "../../api/workflows";
import CreateTaskFromRun from "./CreateTaskFromRun";
import WorkflowGraph from "./WorkflowGraph";

/**
 * WorkflowInsight is the "see the workflow" surface for a frozen contract:
 * its triggers, the steps as a node graph (last run highlighted), and the run
 * history. Read-only; execution lives in RunWorkflow.
 */
export default function WorkflowInsight({ specId }: { specId: string }) {
  const spec = useQuery({
    queryKey: ["workflows", "spec", specId],
    queryFn: () => getWorkflowSpec(specId),
  });
  const runs = useQuery({
    queryKey: ["workflows", "runs", specId],
    queryFn: () => getWorkflowRuns(specId),
    refetchInterval: 10_000,
  });

  const history = runs.data?.runs ?? [];

  return (
    <div
      style={{
        borderTop: "1px solid var(--border)",
        paddingTop: 10,
        display: "flex",
        flexDirection: "column",
        gap: 10,
      }}
    >
      <SectionLabel>Triggers</SectionLabel>
      <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
        {(spec.data?.triggers ?? []).map((t) => (
          <TriggerChip key={`${t.kind}-${t.label}`} t={t} />
        ))}
      </div>

      <SectionLabel>Steps</SectionLabel>
      {spec.data ? (
        <WorkflowGraph
          spec={spec.data.spec}
          triggers={spec.data.triggers}
          lastRun={history[0]}
        />
      ) : (
        <span style={{ fontSize: 12.5, color: "var(--text-secondary)" }}>
          Loading graph…
        </span>
      )}

      <SectionLabel>Run history ({history.length})</SectionLabel>
      {history.length === 0 ? (
        <span style={{ fontSize: 12.5, color: "var(--text-secondary)" }}>
          No runs yet — hit Run now above.
        </span>
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
          {history.slice(0, 8).map((rec, i) => (
            <RunRow key={`${rec.at}-${i}`} rec={rec} specId={specId} />
          ))}
        </div>
      )}
    </div>
  );
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div
      style={{
        fontSize: 11,
        fontWeight: 700,
        letterSpacing: ".06em",
        textTransform: "uppercase",
        color: "var(--text-secondary)",
      }}
    >
      {children}
    </div>
  );
}

const TRIGGER_ICON: Record<WorkflowTrigger["kind"], string> = {
  manual: "▶",
  schedule: "⏱",
  webhook: "🔗",
  context: "🔄",
};

function TriggerChip({ t }: { t: WorkflowTrigger }) {
  const icon = TRIGGER_ICON[t.kind] ?? "▶";
  const detail =
    t.kind === "schedule" && t.interval_minutes
      ? ` · every ${formatInterval(t.interval_minutes)}${t.enabled === false ? " (off)" : ""}`
      : "";
  return (
    <span
      style={{
        fontSize: 12,
        padding: "3px 9px",
        borderRadius: 999,
        border: "1px solid var(--border)",
        background: "var(--bg-card)",
        color: "var(--text)",
      }}
    >
      {icon} {t.label}
      {detail}
    </span>
  );
}

function RunRow({ rec, specId }: { rec: RunRecord; specId: string }) {
  const [open, setOpen] = useState(false);
  const r = rec.result;
  const audit = r.audit ?? [];
  const blocked = audit.some((a) => a.skipped === "action_failed");
  const emailCount =
    typeof r.outputs?.email_count === "number" ? r.outputs.email_count : null;
  const digest =
    typeof r.outputs?.digest === "string" ? r.outputs.digest : null;
  const ok = r.final_state && !blocked;

  return (
    <div
      style={{
        border: "1px solid var(--border)",
        borderRadius: 8,
        background: "var(--bg-card)",
        fontSize: 12.5,
      }}
    >
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        style={{
          display: "flex",
          alignItems: "center",
          gap: 8,
          width: "100%",
          padding: "7px 10px",
          background: "transparent",
          border: "none",
          cursor: "pointer",
          color: "var(--text)",
          textAlign: "left",
        }}
      >
        <span style={{ color: ok ? "var(--green)" : "var(--amber, #b26b00)" }}>
          {ok ? "✓" : blocked ? "⏸" : "•"}
        </span>
        <span style={{ color: "var(--text-secondary)" }}>{rec.trigger}</span>
        <span style={{ flex: 1 }}>→ {r.final_state || "—"}</span>
        {emailCount !== null && (
          <span style={{ color: "var(--text-secondary)" }}>
            {emailCount} emails
          </span>
        )}
        <span style={{ color: "var(--text-secondary)", fontSize: 11 }}>
          {formatTime(rec.at)}
        </span>
      </button>
      {open && (
        <div
          style={{
            borderTop: "1px solid var(--border)",
            padding: "8px 10px",
            display: "flex",
            flexDirection: "column",
            gap: 6,
          }}
        >
          {digest && (
            <pre
              style={{
                margin: 0,
                whiteSpace: "pre-wrap",
                fontFamily: "var(--font-mono, monospace)",
                fontSize: 11.5,
                lineHeight: 1.5,
                color: "var(--text)",
              }}
            >
              {digest}
            </pre>
          )}
          <div
            style={{
              fontFamily: "var(--font-mono, monospace)",
              fontSize: 11,
              color: "var(--text-secondary)",
            }}
          >
            {audit.map((a, i) => (
              <div key={`${a.event}-${a.from}-${i}`}>
                {a.skipped ? "•" : "✓"} {a.event}: {a.from}
                {a.to ? ` → ${a.to}` : ""}
                {a.skipped ? ` — ${a.skipped}` : ""}
              </div>
            ))}
          </div>
          <div style={{ borderTop: "1px solid var(--border)", paddingTop: 6 }}>
            <CreateTaskFromRun specId={specId} rec={rec} />
          </div>
        </div>
      )}
    </div>
  );
}

function formatInterval(minutes: number): string {
  if (minutes % 1440 === 0) {
    const d = minutes / 1440;
    return d === 1 ? "day" : `${d} days`;
  }
  if (minutes % 60 === 0) {
    const h = minutes / 60;
    return h === 1 ? "hour" : `${h} hours`;
  }
  return `${minutes} min`;
}

function formatTime(at?: string): string {
  if (!at) return "";
  const d = new Date(at);
  if (Number.isNaN(d.getTime())) return at;
  return d.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}
