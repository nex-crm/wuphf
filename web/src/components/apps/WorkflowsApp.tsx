import { type CSSProperties, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  draftWorkflow,
  freezeWorkflow,
  getSpottedWorkflows,
  type ShipcheckReport,
  type SpottedWorkflow,
} from "../../api/workflows";

/**
 * Spotted Workflows panel. Discovery -> review -> creation:
 * GET /workflows/spotted runs the detection miner; "Review draft" previews the
 * drafted workflow-spec contract + shipcheck (POST /workflows/draft) which the
 * operator can edit; "Create workflow" binds the reviewed contract
 * (POST /workflows/freeze). A contract that fails shipcheck never ships.
 */
export default function WorkflowsApp() {
  const queryClient = useQueryClient();
  const { data, isLoading, error } = useQuery({
    queryKey: ["workflows", "spotted"],
    queryFn: getSpottedWorkflows,
    refetchInterval: 15_000,
  });

  // Which workflow is being reviewed, plus the editable draft + its shipcheck.
  const [reviewFp, setReviewFp] = useState<string | null>(null);
  const [draftText, setDraftText] = useState("");
  const [draftCheck, setDraftCheck] = useState<ShipcheckReport | null>(null);
  const [formError, setFormError] = useState<string | null>(null);

  const closeReview = () => {
    setReviewFp(null);
    setDraftText("");
    setDraftCheck(null);
    setFormError(null);
  };

  const review = useMutation({
    mutationFn: (fingerprint: string) => draftWorkflow(fingerprint),
    onSuccess: (result, fingerprint) => {
      setReviewFp(fingerprint);
      setDraftText(JSON.stringify(result.spec, null, 2));
      setDraftCheck(result.shipcheck);
      setFormError(null);
    },
    onError: (err: unknown) => {
      setFormError(
        err instanceof Error ? err.message : "Couldn't draft the contract",
      );
    },
  });

  const create = useMutation({
    mutationFn: ({
      fingerprint,
      spec,
    }: {
      fingerprint: string;
      spec: unknown;
    }) => freezeWorkflow(fingerprint, spec),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["workflows", "spotted"] });
      queryClient.invalidateQueries({ queryKey: ["skills", "all"] });
      closeReview();
    },
    onError: (err: unknown) => {
      setFormError(
        err instanceof Error ? err.message : "Couldn't create the workflow",
      );
    },
  });

  const onCreate = (fingerprint: string) => {
    let spec: unknown;
    try {
      spec = JSON.parse(draftText);
    } catch {
      setFormError("The contract is not valid JSON — fix it before creating.");
      return;
    }
    create.mutate({ fingerprint, spec });
  };

  const workflows = data?.workflows ?? [];

  return (
    <div
      style={{
        padding: 24,
        maxWidth: 820,
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
          Repeated multi-step work the office's agents kept doing. Review the
          drafted contract, then freeze it into a reusable workflow.
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
      {!(isLoading || error) && workflows.length === 0 && (
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
            reviewing={reviewFp === wf.fingerprint}
            draftText={draftText}
            draftCheck={draftCheck}
            formError={reviewFp === wf.fingerprint ? formError : null}
            reviewBusy={review.isPending && review.variables === wf.fingerprint}
            createBusy={create.isPending}
            onReview={() => review.mutate(wf.fingerprint)}
            onDraftChange={setDraftText}
            onCreate={() => onCreate(wf.fingerprint)}
            onCancel={closeReview}
          />
        ))}
      </div>
    </div>
  );
}

interface WorkflowCardProps {
  wf: SpottedWorkflow;
  reviewing: boolean;
  draftText: string;
  draftCheck: ShipcheckReport | null;
  formError: string | null;
  reviewBusy: boolean;
  createBusy: boolean;
  onReview: () => void;
  onDraftChange: (text: string) => void;
  onCreate: () => void;
  onCancel: () => void;
}

function WorkflowCard({
  wf,
  reviewing,
  draftText,
  draftCheck,
  formError,
  reviewBusy,
  createBusy,
  onReview,
  onDraftChange,
  onCreate,
  onCancel,
}: WorkflowCardProps) {
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
        {wf.shape.length} steps{" "}
        <b style={{ color: "var(--text)" }}>{wf.count} times</b>.
      </div>
      <div
        style={{ display: "flex", flexWrap: "wrap", gap: 6, marginBottom: 14 }}
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

      {wf.frozen ? (
        <span style={{ fontSize: 13, color: "var(--green)", fontWeight: 600 }}>
          ✓ Workflow created · contract shipchecked
        </span>
      ) : reviewing ? (
        <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <div
            style={{
              fontSize: 12.5,
              fontWeight: 600,
              color: "var(--text-secondary)",
            }}
          >
            Review the contract — edit anything, then create:
          </div>
          <textarea
            value={draftText}
            onChange={(e) => onDraftChange(e.target.value)}
            spellCheck={false}
            style={{
              width: "100%",
              minHeight: 220,
              fontFamily: "var(--font-mono, monospace)",
              fontSize: 12,
              lineHeight: 1.5,
              color: "var(--text)",
              background: "var(--bg-warm)",
              border: "1px solid var(--border)",
              borderRadius: 8,
              padding: 12,
              resize: "vertical",
            }}
          />
          {draftCheck && <ShipcheckList report={draftCheck} />}
          {formError && (
            <div style={{ fontSize: 12.5, color: "var(--red)" }}>
              {formError}
            </div>
          )}
          <div style={{ display: "flex", gap: 10 }}>
            <button
              type="button"
              onClick={onCreate}
              disabled={createBusy}
              style={primaryBtn(createBusy)}
            >
              {createBusy ? "Creating…" : "Create workflow"}
            </button>
            <button type="button" onClick={onCancel} style={ghostBtn}>
              Cancel
            </button>
          </div>
        </div>
      ) : (
        <button
          type="button"
          onClick={onReview}
          disabled={reviewBusy}
          style={primaryBtn(reviewBusy)}
        >
          {reviewBusy ? "Drafting…" : "Review draft"}
        </button>
      )}
    </div>
  );
}

function ShipcheckList({ report }: { report: ShipcheckReport }) {
  return (
    <div
      style={{
        background: report.passed
          ? "var(--success-100, #e9fbef)"
          : "var(--error-100, #ffeeeb)",
        border: "1px solid var(--border)",
        borderRadius: 8,
        padding: "10px 12px",
        fontSize: 12,
      }}
    >
      <div
        style={{
          fontWeight: 700,
          marginBottom: 6,
          color: report.passed ? "var(--green)" : "var(--red)",
        }}
      >
        shipcheck {report.passed ? "PASS" : "FAIL"}
      </div>
      <div style={{ display: "flex", flexDirection: "column", gap: 3 }}>
        {report.checks.map((c) => (
          <div key={c.name} style={{ color: "var(--text-secondary)" }}>
            <span
              style={{
                color: c.pass ? "var(--green)" : "var(--red)",
                fontWeight: 700,
              }}
            >
              {c.pass ? "✓" : "✗"}
            </span>{" "}
            {c.name}
            {c.detail ? ` — ${c.detail}` : ""}
          </div>
        ))}
      </div>
    </div>
  );
}

function primaryBtn(busy: boolean): CSSProperties {
  return {
    background: "var(--accent)",
    color: "#fff",
    border: "none",
    borderRadius: 8,
    padding: "9px 16px",
    fontSize: 13.5,
    fontWeight: 600,
    cursor: busy ? "default" : "pointer",
    opacity: busy ? 0.7 : 1,
  };
}

const ghostBtn: CSSProperties = {
  background: "transparent",
  color: "var(--text-secondary)",
  border: "1px solid var(--border)",
  borderRadius: 8,
  padding: "9px 16px",
  fontSize: 13.5,
  fontWeight: 550,
  cursor: "pointer",
};
