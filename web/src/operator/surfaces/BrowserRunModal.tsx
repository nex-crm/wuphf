// BrowserRunModal — the EXECUTE surface: watch the AI run an app's workflow by
// driving a browser (the computer-use loop). Phase 1 plays a realistic mock run
// so the shape is real before the backend exists; E2 swaps in the live
// /execute/browser SSE stream. See docs/specs/operator-browser-execution.md.

import { useEffect, useMemo, useRef, useState } from "react";
import { Check, Globe, Pause, Play, Send, Square, X } from "lucide-react";

import {
  actionLabel,
  buildMockRun,
  type ExecStatus,
  flattenRun,
} from "../exec/browserExec";

interface BrowserRunModalProps {
  toolName: string;
  onClose: () => void;
}

// How long each action lingers in the timeline before the next one runs.
const STEP_MS = 1100;

const STEP_GLYPH: Record<string, string> = {
  trigger: "TR",
  enrich: "EN",
  ai: "AI",
  decision: "IF",
  action: "DO",
  branch: "EL",
};

export function BrowserRunModal({ toolName, onClose }: BrowserRunModalProps) {
  const goal = `Run ${toolName}`;
  const steps = useMemo(
    () => buildMockRun({ goal, toolName }),
    [goal, toolName],
  );
  const flat = useMemo(() => flattenRun(steps), [steps]);

  // How many actions have run, whether the operator paused, and which gated
  // actions they have approved (external sends pause for approval).
  const [ran, setRan] = useState(0);
  const [paused, setPaused] = useState(false);
  const [approved, setApproved] = useState<ReadonlySet<number>>(new Set());

  const next = flat[ran];
  const awaitingApproval = Boolean(next?.action.gated && !approved.has(ran));
  const done = ran >= flat.length;

  const status: ExecStatus = done
    ? "done"
    : awaitingApproval
      ? "needs-you"
      : paused
        ? "paused"
        : "running";

  // Advance the run on a timer unless it is done, paused, or waiting on the
  // operator to approve an external send.
  const blocked = done || paused || awaitingApproval;
  useEffect(() => {
    if (blocked) return;
    const t = window.setTimeout(() => setRan((n) => n + 1), STEP_MS);
    return () => window.clearTimeout(t);
  }, [blocked, ran]);

  const timelineRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = timelineRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [ran]);

  const shown = flat.slice(0, ran);
  const current = shown[shown.length - 1] ?? null;
  // Track the address bar from the last navigate.
  const url =
    [...shown].reverse().find((f) => f.action.kind === "navigate")?.action
      .value ?? "your browser";
  // Keep the last real screen on the viewport (read/done actions have none).
  const screen =
    [...shown].reverse().find((f) => f.action.screen)?.action.screen ??
    (done ? "Run complete" : "Starting…");
  const result = done
    ? (flat.at(-1)?.action.value ?? "Run complete.")
    : awaitingApproval
      ? "Waiting for you to approve sending to Slack."
      : (next?.action.reasoning ?? "Working…");

  function approveGate() {
    setApproved((prev) => new Set(prev).add(ran));
  }

  return (
    <div
      className="opr-modal-scrim"
      role="dialog"
      aria-modal="true"
      aria-label={`Run ${toolName} in the browser`}
      onClick={onClose}
    >
      <div className="opr-exec" onClick={(e) => e.stopPropagation()}>
        {/* Live browser viewport (mock screens now, real screenshots in E2). */}
        <div className="opr-exec-stage">
          <div className="opr-exec-chrome">
            <span className="opr-exec-dot" />
            <span className="opr-exec-dot" />
            <span className="opr-exec-dot" />
            <div className="opr-exec-omnibox">
              <Globe size={12} strokeWidth={1.9} aria-hidden={true} />
              {url}
            </div>
            <span
              className={`opr-exec-live${status === "running" ? " is-live" : ""}`}
            >
              {status === "running" ? "running" : status}
            </span>
          </div>
          <div className="opr-exec-screen">
            <div className="opr-exec-screen-name">{screen}</div>
            <div className="opr-exec-screen-action">
              {current
                ? actionLabel(current.action)
                : "Connecting to the browser…"}
            </div>
          </div>
        </div>

        <div className="opr-exec-body">
          <div className="opr-exec-head">
            <div>
              <div className="opr-eyebrow">Running in your browser</div>
              <div className="opr-exec-goal">{goal}</div>
            </div>
            <div className="opr-exec-controls">
              {!done ? (
                <button
                  type="button"
                  className="opr-btn opr-btn-sm"
                  onClick={() => setPaused((p) => !p)}
                  disabled={awaitingApproval}
                >
                  {paused ? (
                    <Play size={13} strokeWidth={1.9} aria-hidden={true} />
                  ) : (
                    <Pause size={13} strokeWidth={1.9} aria-hidden={true} />
                  )}
                  {paused ? "Resume" : "Pause"}
                </button>
              ) : null}
              <button
                type="button"
                className="opr-btn opr-btn-sm"
                onClick={onClose}
              >
                {done ? (
                  <X size={13} strokeWidth={1.9} aria-hidden={true} />
                ) : (
                  <Square size={12} strokeWidth={1.9} aria-hidden={true} />
                )}
                {done ? "Close" : "Stop"}
              </button>
            </div>
          </div>

          {/* The narrated action timeline, grouped under their workflow steps. */}
          <div className="opr-exec-timeline" ref={timelineRef}>
            {shown.map((f, i) => {
              const firstOfStep =
                i === 0 || shown[i - 1].stepIndex !== f.stepIndex;
              return (
                <div key={i}>
                  {firstOfStep ? (
                    <div className="opr-exec-step">
                      <span
                        className={`opr-step-node opr-step-node-${f.stepKind}`}
                        aria-hidden={true}
                      >
                        {STEP_GLYPH[f.stepKind]}
                      </span>
                      {f.stepTitle}
                    </div>
                  ) : null}
                  <div
                    className={`opr-exec-act${f.action.gated ? " is-gated" : ""}`}
                  >
                    <span className="opr-exec-act-label">
                      {actionLabel(f.action)}
                    </span>
                    {f.action.reasoning ? (
                      <span className="opr-exec-act-why">
                        {f.action.reasoning}
                      </span>
                    ) : null}
                  </div>
                </div>
              );
            })}
          </div>

          {/* External send pauses for explicit approval — execution never
              bypasses the human gate. */}
          {awaitingApproval ? (
            <div className="opr-exec-approval">
              <div className="opr-exec-approval-text">
                <Send size={13} strokeWidth={1.9} aria-hidden={true} />
                Send this to Slack #ae-handoffs?
              </div>
              <div className="opr-exec-approval-actions">
                <button
                  type="button"
                  className="opr-btn opr-btn-sm"
                  onClick={onClose}
                >
                  Stop
                </button>
                <button
                  type="button"
                  className="opr-btn opr-btn-primary opr-btn-sm"
                  onClick={approveGate}
                >
                  <Check size={13} strokeWidth={1.9} aria-hidden={true} />
                  Approve & send
                </button>
              </div>
            </div>
          ) : (
            <div
              className={`opr-exec-result${done ? " is-done" : ""}`}
              role="status"
            >
              {done ? (
                <Check size={14} strokeWidth={2} aria-hidden={true} />
              ) : null}
              {result}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
