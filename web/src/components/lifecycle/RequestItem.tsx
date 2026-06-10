import { useState } from "react";

import type { AgentRequest, InterviewOption } from "../../api/client";
import { formatRelativeTime } from "../../lib/format";
import {
  requestOptionNeedsText,
  requestOptionTextHint,
} from "../../lib/requestOptions";

interface RequestItemProps {
  request: AgentRequest;
  isPending: boolean;
  onAnswer?: (choiceId: string, customText?: string) => void;
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

      {isPending && options.length > 0 ? (
        <RequestActions
          key={request.id}
          request={request}
          options={options}
          onAnswer={onAnswer}
        />
      ) : null}

      {!isPending && (
        <div style={{ fontSize: 12, color: "var(--green)", fontWeight: 500 }}>
          Answered
        </div>
      )}
    </div>
  );
}

interface RequestActionsProps {
  request: AgentRequest;
  options: InterviewOption[];
  onAnswer?: (choiceId: string, customText?: string) => void;
}

function RequestActions({ request, options, onAnswer }: RequestActionsProps) {
  // Caller passes `key={request.id}` so the subtree fully remounts on
  // request switch, so text-entry state can't leak from one request to the
  // next without us needing an explicit reset effect here.
  const [textOption, setTextOption] = useState<InterviewOption | null>(null);
  const [customText, setCustomText] = useState("");

  const submitText = () => {
    const text = customText.trim();
    if (!(textOption && text)) return;
    onAnswer?.(textOption.id, text);
    setTextOption(null);
    setCustomText("");
  };

  if (textOption) {
    return (
      <div style={{ display: "grid", gap: 8 }}>
        <textarea
          className="interview-bar-textarea"
          placeholder={requestOptionTextHint(request, textOption)}
          value={customText}
          onChange={(e) => setCustomText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              e.preventDefault();
              setTextOption(null);
              setCustomText("");
            }
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
              e.preventDefault();
              submitText();
            }
          }}
          rows={3}
        />
        <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
          <button
            type="button"
            className="btn btn-sm btn-ghost"
            onClick={() => {
              setTextOption(null);
              setCustomText("");
            }}
          >
            Back
          </button>
          <button
            type="button"
            className="btn btn-sm btn-primary"
            disabled={!customText.trim()}
            onClick={submitText}
          >
            Send as {textOption.label}
          </button>
        </div>
      </div>
    );
  }

  return (
    <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
      {options.map((opt) => {
        const needsText = requestOptionNeedsText(request, opt);
        return (
          <button
            type="button"
            key={opt.id}
            className={`btn btn-sm ${opt.id === request.recommended_id ? "btn-primary" : "btn-ghost"}`}
            title={opt.description}
            onClick={() => {
              if (needsText) {
                setTextOption(opt);
                setCustomText("");
                return;
              }
              onAnswer?.(opt.id);
            }}
          >
            {opt.label}
            {needsText ? " · type" : ""}
          </button>
        );
      })}
    </div>
  );
}
