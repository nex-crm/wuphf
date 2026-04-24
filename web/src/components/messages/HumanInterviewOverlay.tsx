import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { type AgentRequest, answerRequest } from "../../api/client";
import { useRequests } from "../../hooks/useRequests";
import { showNotice } from "../ui/Toast";

/**
 * Global blocking-interview overlay. Always renders the first blocking pending
 * request from the broker, regardless of which app/channel the user is viewing.
 * Non-blocking requests get a one-time toast and stay in the Requests panel.
 */
export function HumanInterviewOverlay() {
  const { blockingPending, pending } = useRequests();
  const queryClient = useQueryClient();
  const [submitting, setSubmitting] = useState(false);
  const seenNonBlockingIds = useRef<Set<string>>(new Set());

  // Toast non-blocking requests once each
  useEffect(() => {
    for (const req of pending) {
      if (req.blocking) continue;
      if (!req.id || seenNonBlockingIds.current.has(req.id)) continue;
      seenNonBlockingIds.current.add(req.id);
      showNotice(`@${req.from || "someone"} asked: ${req.question}`, "info");
    }
  }, [pending]);

  if (!blockingPending) return null;

  return (
    <BlockingInterview
      request={blockingPending}
      submitting={submitting}
      onAnswer={async (choiceId) => {
        if (submitting) return;
        setSubmitting(true);
        try {
          await answerRequest(blockingPending.id, choiceId);
          await queryClient.invalidateQueries({ queryKey: ["requests"] });
          await queryClient.invalidateQueries({ queryKey: ["requests-badge"] });
        } catch (err: unknown) {
          const message =
            err instanceof Error ? err.message : "Failed to answer";
          showNotice(message, "error");
        } finally {
          setSubmitting(false);
        }
      }}
    />
  );
}

interface BlockingInterviewProps {
  request: AgentRequest;
  submitting: boolean;
  onAnswer: (choiceId: string) => void;
}

function BlockingInterview({
  request,
  submitting,
  onAnswer,
}: BlockingInterviewProps) {
  const options = request.options ?? request.choices ?? [];

  return (
    <div
      className="interview-overlay"
      role="dialog"
      aria-modal="true"
      aria-labelledby="interview-title"
    >
      <div className="interview-card">
        <div className="interview-meta">
          <span className="badge badge-yellow">BLOCKING</span>
          <span className="interview-from">@{request.from || "agent"}</span>
          {request.channel && (
            <span className="interview-channel">in #{request.channel}</span>
          )}
        </div>
        <h2 id="interview-title" className="interview-title">
          {request.title && request.title !== "Request"
            ? request.title
            : "Human input required"}
        </h2>
        <p className="interview-question">{request.question}</p>
        {request.context && (
          <p className="interview-context">{request.context}</p>
        )}
        {options.length > 0 ? (
          <div className="interview-actions">
            {options.map((opt) => (
              <button
                key={opt.id}
                type="button"
                className={`btn btn-sm ${opt.id === request.recommended_id ? "btn-primary" : "btn-ghost"}`}
                onClick={() => onAnswer(opt.id)}
                disabled={submitting}
                title={opt.description}
              >
                {opt.label}
              </button>
            ))}
          </div>
        ) : (
          <div className="interview-empty">
            No choices provided. Open the Requests app to respond.
          </div>
        )}
      </div>
    </div>
  );
}
