import type { AgentRequest } from "../../api/client";
import { formatRelativeTime } from "../../lib/format";
import { RedactedBadge } from "../ui/RedactedBadge";

interface RequestItemProps {
  request: AgentRequest;
  isPending: boolean;
  onAnswer?: (choiceId: string) => void;
}

/**
 * Renders one agent request row inside the unified Inbox detail pane.
 * Originally lived in apps/RequestsApp.tsx; Phase 2b retired that
 * surface so the renderer moved here next to DecisionInbox.
 */
export function RequestItem({
  request,
  isPending,
  onAnswer,
}: RequestItemProps) {
  // Broker uses `options`; legacy used `choices`. Accept either.
  const options = request.options ?? request.choices ?? [];
  const ts = request.updated_at ?? request.created_at ?? request.timestamp;

  return (
    <div className="app-card" style={{ marginBottom: 8 }}>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 8,
          marginBottom: 4,
        }}
      >
        <span style={{ fontWeight: 600, fontSize: 13 }}>
          {request.from || "Unknown"}
        </span>
        {request.status ? (
          <span className="badge badge-accent">
            {request.status.toUpperCase()}
          </span>
        ) : null}
        {request.blocking ? (
          <span className="badge badge-yellow">BLOCKING</span>
        ) : null}
        {request.redacted ? (
          <RedactedBadge reasons={request.redaction_reasons} />
        ) : null}
      </div>

      {request.title && request.title !== "Request" && (
        <div style={{ fontSize: 13, fontWeight: 600, marginBottom: 4 }}>
          {request.title}
        </div>
      )}

      <div style={{ fontSize: 14, marginBottom: 8 }}>
        {request.question || ""}
      </div>

      {request.context ? (
        <div
          style={{
            fontSize: 12,
            color: "var(--text-secondary)",
            marginBottom: 8,
            whiteSpace: "pre-wrap",
          }}
        >
          {request.context}
        </div>
      ) : null}

      {ts ? (
        <div className="app-card-meta" style={{ marginBottom: 6 }}>
          {formatRelativeTime(ts)}
        </div>
      ) : null}

      {isPending && options.length > 0 && (
        <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
          {options.map((opt) => (
            <button
              type="button"
              key={opt.id}
              className={`btn btn-sm ${opt.id === request.recommended_id ? "btn-primary" : "btn-ghost"}`}
              title={opt.description}
              onClick={() => onAnswer?.(opt.id)}
            >
              {opt.label}
            </button>
          ))}
        </div>
      )}

      {!isPending && (
        <div style={{ fontSize: 12, color: "var(--green)", fontWeight: 500 }}>
          Answered
        </div>
      )}
    </div>
  );
}
