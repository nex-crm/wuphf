import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  type ExtractedTrigger,
  type ExtractedWorkflow,
  freezeExtractedWorkflow,
  getExtractedWorkflows,
} from "../../api/workflows";
import { router } from "../../lib/router";

/**
 * Detected Workflows feed. As tasks complete, the office extracts a real,
 * parameterized workflow from the task's trace (the completion sweep) and the
 * model judges whether it is worth automating. This surface shows those
 * proactive detections, loudest when a shape recurs across tasks.
 */
export default function ExtractedWorkflows() {
  const { data } = useQuery({
    queryKey: ["workflows", "extracted"],
    queryFn: getExtractedWorkflows,
    refetchInterval: 15_000,
  });
  const workflows = data?.workflows ?? [];
  if (workflows.length === 0) {
    return null;
  }
  return (
    <section style={{ marginBottom: 22 }}>
      <div
        style={{
          fontSize: 11.5,
          fontWeight: 700,
          letterSpacing: ".08em",
          textTransform: "uppercase",
          color: "var(--text-secondary)",
          marginBottom: 10,
        }}
      >
        <span style={{ color: "var(--accent)" }}>✨</span> Detected from your
        completed work
      </div>
      <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        {workflows.map((wf) => (
          <ExtractedCard key={wf.fingerprint} wf={wf} />
        ))}
      </div>
    </section>
  );
}

const TRIGGER_ICON: Record<ExtractedTrigger["kind"], string> = {
  manual: "▶",
  schedule: "⏱",
  webhook: "🔗",
  context: "🔄",
};

function triggerLabel(t: ExtractedTrigger): string {
  if (t.kind === "schedule" && t.interval_minutes) {
    const m = t.interval_minutes;
    const every =
      m % 1440 === 0
        ? `${m / 1440 === 1 ? "day" : `${m / 1440} days`}`
        : m % 60 === 0
          ? `${m / 60 === 1 ? "hour" : `${m / 60} hours`}`
          : `${m} min`;
    return `Schedule · every ${every}`;
  }
  return t.kind.charAt(0).toUpperCase() + t.kind.slice(1);
}

function ExtractedCard({ wf }: { wf: ExtractedWorkflow }) {
  const queryClient = useQueryClient();
  const [showContract, setShowContract] = useState(false);
  const steps = wf.spec?.actions ?? [];

  const freeze = useMutation({
    mutationFn: () => freezeExtractedWorkflow(wf.fingerprint),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["workflows", "spotted"] });
      queryClient.invalidateQueries({ queryKey: ["skills", "all"] });
    },
  });
  const created = freeze.data?.created || freeze.isSuccess;
  return (
    <div
      style={{
        background: "var(--bg-card)",
        border: "1px solid var(--border)",
        borderRadius: 14,
        padding: "16px 18px",
      }}
    >
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 10,
          marginBottom: 8,
        }}
      >
        <div style={{ fontSize: 16, fontWeight: 650, flex: 1 }}>{wf.name}</div>
        {wf.recurrence > 1 && (
          <span
            style={{
              fontSize: 11.5,
              fontWeight: 700,
              color: "var(--accent)",
              background: "var(--bg-warm)",
              border: "1px solid var(--border)",
              borderRadius: 999,
              padding: "2px 9px",
            }}
          >
            done {wf.recurrence}×
          </span>
        )}
      </div>

      {wf.description && (
        <p
          style={{
            margin: "0 0 10px",
            fontSize: 13.5,
            lineHeight: 1.5,
            color: "var(--text-secondary)",
          }}
        >
          {wf.description}
        </p>
      )}

      <div
        style={{ display: "flex", flexWrap: "wrap", gap: 6, marginBottom: 12 }}
      >
        <Chip>
          {TRIGGER_ICON[wf.trigger.kind] ?? "▶"} {triggerLabel(wf.trigger)}
        </Chip>
        <Chip>{Math.round(wf.confidence * 100)}% confident</Chip>
      </div>

      <div
        style={{ display: "flex", flexWrap: "wrap", gap: 6, marginBottom: 12 }}
      >
        {steps.map((a, i) => (
          <span
            key={a.id}
            style={{
              fontSize: 12,
              fontFamily: "var(--font-mono, monospace)",
              background: "var(--bg-warm)",
              border: "1px solid var(--border)",
              borderRadius: 6,
              padding: "2px 7px",
            }}
          >
            {i + 1}. {a.id}
          </span>
        ))}
      </div>

      <Provenance wf={wf} />

      <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
        {created ? (
          <span
            style={{ fontSize: 13, color: "var(--green)", fontWeight: 600 }}
          >
            ✓ Workflow created · contract shipchecked
          </span>
        ) : (
          <button
            type="button"
            onClick={() => freeze.mutate()}
            disabled={freeze.isPending}
            style={{
              background: "var(--accent)",
              color: "#fff",
              border: "none",
              borderRadius: 8,
              padding: "7px 14px",
              fontSize: 13,
              fontWeight: 600,
              cursor: freeze.isPending ? "default" : "pointer",
              opacity: freeze.isPending ? 0.7 : 1,
            }}
          >
            {freeze.isPending ? "Creating…" : "Create workflow"}
          </button>
        )}
        <button
          type="button"
          onClick={() => setShowContract((v) => !v)}
          style={{
            background: "transparent",
            color: "var(--text-secondary)",
            border: "1px solid var(--border)",
            borderRadius: 8,
            padding: "6px 12px",
            fontSize: 12.5,
            fontWeight: 550,
            cursor: "pointer",
          }}
        >
          {showContract ? "Hide contract" : "View contract"}
        </button>
      </div>
      {freeze.error && (
        <div style={{ fontSize: 12.5, color: "var(--red)", marginTop: 8 }}>
          {freeze.error instanceof Error
            ? freeze.error.message
            : "Couldn't create the workflow"}
        </div>
      )}

      {showContract && wf.spec && (
        <pre
          style={{
            marginTop: 10,
            marginBottom: 0,
            maxHeight: 280,
            overflow: "auto",
            whiteSpace: "pre-wrap",
            fontFamily: "var(--font-mono, monospace)",
            fontSize: 11.5,
            lineHeight: 1.5,
            color: "var(--text)",
            background: "var(--bg-warm)",
            border: "1px solid var(--border)",
            borderRadius: 8,
            padding: 12,
          }}
        >
          {JSON.stringify(wf.spec, null, 2)}
        </pre>
      )}
    </div>
  );
}

function wikiArticleTitle(path: string): string {
  const leaf = path.split("/").pop() ?? path;
  return leaf.replace(/\.(md|html|json)$/i, "");
}

/**
 * Provenance trail for a detected workflow: why it was generated, which
 * task(s) it came from (linkable), and what wiki context informed the work
 * (linkable). Everything is traceable back to its source.
 */
function Provenance({ wf }: { wf: ExtractedWorkflow }) {
  const tasks = wf.task_ids ?? [];
  const wiki = wf.wiki_context ?? [];
  if (!wf.why && tasks.length === 0 && wiki.length === 0) {
    return null;
  }
  return (
    <div
      style={{
        borderTop: "1px solid var(--border)",
        paddingTop: 10,
        marginBottom: 12,
        display: "flex",
        flexDirection: "column",
        gap: 6,
        fontSize: 12.5,
        color: "var(--text-secondary)",
      }}
    >
      {wf.why && (
        <div>
          <b style={{ color: "var(--text)" }}>Why we spotted this:</b> {wf.why}
        </div>
      )}
      {tasks.length > 0 && (
        <div
          style={{
            display: "flex",
            flexWrap: "wrap",
            gap: 6,
            alignItems: "center",
          }}
        >
          <b style={{ color: "var(--text)" }}>From:</b>
          {tasks.map((id) => (
            <button
              key={id}
              type="button"
              onClick={() =>
                void router.navigate({
                  to: "/tasks/$taskId",
                  params: { taskId: id },
                })
              }
              style={linkChip}
            >
              {id}
            </button>
          ))}
        </div>
      )}
      {wiki.length > 0 && (
        <div
          style={{
            display: "flex",
            flexWrap: "wrap",
            gap: 6,
            alignItems: "center",
          }}
        >
          <b style={{ color: "var(--text)" }}>Wiki context:</b>
          {wiki.map((path) => (
            <a
              key={path}
              href={`#/wiki/${encodeURI(path)}`}
              style={linkChip}
              title={path}
            >
              📄 {wikiArticleTitle(path)}
            </a>
          ))}
        </div>
      )}
    </div>
  );
}

const linkChip: React.CSSProperties = {
  fontSize: 12,
  fontFamily: "var(--font-mono, monospace)",
  background: "var(--bg-warm)",
  border: "1px solid var(--border)",
  borderRadius: 6,
  padding: "2px 8px",
  color: "var(--accent)",
  textDecoration: "none",
  cursor: "pointer",
};

function Chip({ children }: { children: React.ReactNode }) {
  return (
    <span
      style={{
        fontSize: 12,
        padding: "3px 9px",
        borderRadius: 999,
        border: "1px solid var(--border)",
        background: "var(--bg-warm)",
        color: "var(--text)",
      }}
    >
      {children}
    </span>
  );
}
