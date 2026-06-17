import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  type SpottedWorkflow,
  freezeWorkflow,
  getSpottedWorkflows,
} from "../../api/workflows";

/**
 * Spotted Workflows panel. Calls GET /workflows/spotted (the detection miner
 * over the persisted turn-manifest corpus) and renders each repeated workflow
 * as a card with a "Create workflow" button that POSTs /workflows/freeze. This
 * is the thin discovery -> creation vertical slice for the browse demo.
 */
export default function WorkflowsApp() {
  const queryClient = useQueryClient();
  const { data, isLoading, error } = useQuery({
    queryKey: ["workflows", "spotted"],
    queryFn: getSpottedWorkflows,
    refetchInterval: 15_000,
  });

  const freeze = useMutation({
    mutationFn: (fingerprint: string) => freezeWorkflow(fingerprint),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["workflows", "spotted"] });
      queryClient.invalidateQueries({ queryKey: ["skills", "all"] });
    },
  });

  const workflows = data?.workflows ?? [];

  return (
    <div
      style={{
        padding: 24,
        maxWidth: 760,
        margin: "0 auto",
        color: "var(--text)",
      }}
    >
      <header style={{ marginBottom: 16 }}>
        <h1 style={{ fontSize: 20, fontWeight: 650, margin: 0 }}>
          Spotted Workflows
        </h1>
        <p
          style={{
            color: "var(--text-secondary)",
            fontSize: 13.5,
            marginTop: 6,
          }}
        >
          Repeated multi-step work the office's agents kept doing. Freeze one
          into a reusable workflow.
        </p>
      </header>

      {isLoading && (
        <p style={{ color: "var(--text-secondary)" }}>
          Scanning office activity…
        </p>
      )}
      {error && (
        <p style={{ color: "var(--red)" }}>
          Couldn't load detection:{" "}
          {error instanceof Error ? error.message : "unknown error"}
        </p>
      )}
      {!isLoading && !error && workflows.length === 0 && (
        <div
          style={{
            border: "1px solid var(--border)",
            borderRadius: 12,
            padding: 24,
            textAlign: "center",
            color: "var(--text-secondary)",
          }}
        >
          <div style={{ fontSize: 28, marginBottom: 8 }}>🔎</div>
          Nothing spotted yet. Once an agent repeats the same multi-step
          workflow a few times, it shows up here.
        </div>
      )}

      <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        {workflows.map((wf) => (
          <WorkflowCard
            key={wf.fingerprint}
            wf={wf}
            busy={freeze.isPending && freeze.variables === wf.fingerprint}
            onFreeze={() => freeze.mutate(wf.fingerprint)}
          />
        ))}
      </div>
    </div>
  );
}

interface WorkflowCardProps {
  wf: SpottedWorkflow;
  busy: boolean;
  onFreeze: () => void;
}

function WorkflowCard({ wf, busy, onFreeze }: WorkflowCardProps) {
  return (
    <div
      style={{
        background: "var(--bg-card)",
        border: "1px solid var(--border)",
        borderRadius: 14,
        padding: "18px 20px",
      }}
    >
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 7,
          fontSize: 11.5,
          fontWeight: 700,
          letterSpacing: ".08em",
          textTransform: "uppercase",
          color: "var(--text-secondary)",
          marginBottom: 8,
        }}
      >
        <span style={{ color: "var(--accent)" }}>✦</span> Spotted a workflow you
        repeat
      </div>
      <div style={{ fontSize: 17, fontWeight: 650, marginBottom: 6 }}>
        {wf.title}
      </div>
      <div
        style={{
          fontSize: 13,
          color: "var(--text-secondary)",
          marginBottom: 12,
        }}
      >
        <b style={{ color: "var(--text)" }}>{wf.agent}</b> ran these{" "}
        {wf.shape.length} steps <b style={{ color: "var(--text)" }}>
          {wf.count} times
        </b>
        .
      </div>
      <div
        style={{
          display: "flex",
          flexWrap: "wrap",
          gap: 6,
          marginBottom: 14,
        }}
      >
        {wf.shape.map((tool, i) => (
          <span
            key={tool}
            style={{
              fontSize: 12,
              fontFamily: "var(--font-mono, monospace)",
              background: "var(--bg-warm)",
              border: "1px solid var(--border)",
              borderRadius: 6,
              padding: "2px 7px",
            }}
          >
            {i + 1}. {tool}
          </span>
        ))}
      </div>
      <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
        {wf.frozen ? (
          <span
            style={{ fontSize: 13, color: "var(--green)", fontWeight: 600 }}
          >
            ✓ Workflow created
          </span>
        ) : (
          <button
            type="button"
            onClick={onFreeze}
            disabled={busy}
            style={{
              background: "var(--accent)",
              color: "#fff",
              border: "none",
              borderRadius: 8,
              padding: "9px 16px",
              fontSize: 13.5,
              fontWeight: 600,
              cursor: busy ? "default" : "pointer",
              opacity: busy ? 0.7 : 1,
            }}
          >
            {busy ? "Creating…" : "Create workflow"}
          </button>
        )}
      </div>
    </div>
  );
}
