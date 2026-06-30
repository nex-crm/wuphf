// BrowserRunModal — the EXECUTE surface: watch the AI run an app's workflow by
// driving the REAL browser. The operator first grants browser control, then the
// live run streams from the broker's /execute/browser SSE (the cua runner:
// OpenAI plans, cua-driver drives). If the host has no key/runner the endpoint
// 503s and we fall back to the scripted mock so the surface still demonstrates
// the shape. See docs/specs/operator-cua-migration.md.

import { useEffect, useMemo, useRef, useState } from "react";
import {
  Check,
  Globe,
  Loader,
  Pause,
  Play,
  Send,
  Square,
  X,
} from "lucide-react";

import {
  actionLabel,
  buildMockRun,
  type ExecStatus,
  flattenRun,
} from "../exec/browserExec";
import {
  EXEC_UNAVAILABLE,
  type RunnerEvent,
  runBrowserExec,
  runBrowserReplay,
} from "../exec/browserExecClient";
import { loadTrajectory, saveTrajectory } from "../exec/trajectoryStore";

interface BrowserRunModalProps {
  toolName: string;
  onClose: () => void;
  // The natural-language goal handed to the runner. Defaults to "Run <tool>".
  goal?: string;
  // The app whose window to drive (cua-driver picks its largest on-screen
  // window). Defaults to Chrome.
  app?: string;
  // Target a specific window instead of the largest one.
  windowId?: number;
}

// The modal owns the up-front browser-control consent, then delegates to the
// live run — which falls back to the mock when the backend can't run.
export function BrowserRunModal({
  toolName,
  onClose,
  goal: goalProp,
  app,
  windowId,
}: BrowserRunModalProps) {
  const goal = goalProp ?? `Run ${toolName}`;
  const [permitted, setPermitted] = useState(false);
  const [useMock, setUseMock] = useState(false);

  if (!permitted) {
    return (
      <RunScrim toolName={toolName} onClose={onClose}>
        <PermissionGate onAllow={() => setPermitted(true)} onClose={onClose} />
      </RunScrim>
    );
  }
  if (useMock) {
    return <MockBrowserRun toolName={toolName} goal={goal} onClose={onClose} />;
  }
  return (
    <LiveBrowserRun
      toolName={toolName}
      goal={goal}
      app={app}
      windowId={windowId}
      onClose={onClose}
      onUnavailable={() => setUseMock(true)}
    />
  );
}

// Shared modal scrim + frame so every phase looks the same.
function RunScrim({
  toolName,
  onClose,
  children,
}: {
  toolName: string;
  onClose: () => void;
  children: React.ReactNode;
}) {
  return (
    <div
      className="opr-modal-scrim"
      role="dialog"
      aria-modal="true"
      aria-label={`Run ${toolName} in the browser`}
      onClick={onClose}
    >
      <div className="opr-exec" onClick={(e) => e.stopPropagation()}>
        {children}
      </div>
    </div>
  );
}

// The one consent the run cannot proceed without: letting Nex drive the browser.
function PermissionGate({
  onAllow,
  onClose,
}: {
  onAllow: () => void;
  onClose: () => void;
}) {
  return (
    <div className="opr-exec-body">
      <div className="opr-exec-head">
        <div>
          <div className="opr-eyebrow">Run in your browser</div>
          <div className="opr-exec-goal">
            Nex will drive your browser for you
          </div>
        </div>
      </div>
      <div className="opr-exec-approval">
        <div className="opr-exec-approval-text">
          <Globe size={13} strokeWidth={1.9} aria-hidden={true} />
          Let Nex control your browser to run this? You can stop it at any time.
        </div>
        <div className="opr-exec-approval-actions">
          <button
            type="button"
            className="opr-btn opr-btn-sm"
            onClick={onClose}
          >
            Not now
          </button>
          <button
            type="button"
            className="opr-btn opr-btn-primary opr-btn-sm"
            onClick={onAllow}
          >
            <Check size={13} strokeWidth={1.9} aria-hidden={true} />
            Allow
          </button>
        </div>
      </div>
    </div>
  );
}

interface LiveEntry {
  label: string;
  reasoning?: string;
  refused?: boolean;
  replayed?: boolean;
  healed?: boolean;
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : "The run failed.";
}

// The live run: stream the runner's events into a narrated timeline.
function LiveBrowserRun({
  toolName,
  goal,
  app,
  windowId,
  onClose,
  onUnavailable,
}: {
  toolName: string;
  goal: string;
  app?: string;
  windowId?: number;
  onClose: () => void;
  onUnavailable: () => void;
}) {
  const [entries, setEntries] = useState<LiveEntry[]>([]);
  const [thinking, setThinking] = useState(false);
  const [phase, setPhase] = useState<ExecStatus>("running");
  const [result, setResult] = useState("");
  const [context, setContext] = useState("");
  // A saved trajectory from a prior run → replay it deterministically (fast),
  // healing only the steps whose elements moved. None → drive live + record one.
  const saved = useMemo(() => loadTrajectory(toolName, goal), [toolName, goal]);
  const [replaying] = useState(Boolean(saved));
  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    const ctrl = new AbortController();
    abortRef.current = ctrl;
    const onEvent = (e: RunnerEvent) => {
      if (e.type === "status") {
        setThinking(e.status === "thinking");
        if (e.detail) setContext(e.detail);
      } else if (e.type === "action") {
        setThinking(false);
        setEntries((prev) => [
          ...prev,
          {
            label: e.label ?? "",
            reasoning: e.reasoning,
            refused: e.refused,
            replayed: e.replayed,
            healed: e.healed,
          },
        ]);
      } else if (e.type === "trajectory") {
        // Persist the recorded (or healed) trajectory so the next run replays it.
        if (e.steps && e.steps.length > 0) {
          saveTrajectory(toolName, goal, {
            goal: e.goal ?? goal,
            app: e.app ?? app ?? "Google Chrome",
            steps: e.steps,
          });
        }
      } else if (e.type === "done") {
        setThinking(false);
        setPhase("done");
        setResult(e.result ?? "Run complete.");
      } else if (e.type === "error") {
        setThinking(false);
        setPhase("error");
        setResult(e.message ?? "The run failed.");
      }
    };

    const run = saved
      ? runBrowserReplay({
          trajectory: saved,
          windowId,
          signal: ctrl.signal,
          onEvent,
        })
      : runBrowserExec({ goal, app, windowId, signal: ctrl.signal, onEvent });

    run.catch((err) => {
      if (ctrl.signal.aborted) return;
      if (err instanceof Error && err.message === EXEC_UNAVAILABLE) {
        onUnavailable();
        return;
      }
      setPhase("error");
      setResult(errorMessage(err));
    });
    return () => ctrl.abort();
    // Run once for the lifetime of this run.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const timelineRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = timelineRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [entries.length, thinking]);

  const done = phase === "done" || phase === "error";
  const statusText =
    phase === "done"
      ? "done"
      : phase === "error"
        ? "error"
        : thinking
          ? replaying
            ? "healing"
            : "thinking"
          : replaying
            ? "replaying"
            : "running";
  const live = statusText === "running" || statusText === "replaying";

  function stop() {
    abortRef.current?.abort();
    onClose();
  }

  return (
    <RunScrim toolName={toolName} onClose={onClose}>
      <div className="opr-exec-stage">
        <div className="opr-exec-chrome">
          <span className="opr-exec-dot" />
          <span className="opr-exec-dot" />
          <span className="opr-exec-dot" />
          <div className="opr-exec-omnibox">
            <Globe size={12} strokeWidth={1.9} aria-hidden={true} />
            {context || app || "your browser"}
          </div>
          <span className={`opr-exec-live${live ? " is-live" : ""}`}>
            {statusText}
          </span>
        </div>
        <div className="opr-exec-screen">
          <div className="opr-exec-screen-name">
            {done
              ? phase === "error"
                ? "Run stopped"
                : "Run complete"
              : "Your browser"}
          </div>
          <div className="opr-exec-screen-action">
            {entries.length > 0
              ? entries[entries.length - 1].label
              : thinking
                ? "Looking at the page…"
                : "Connecting to your browser…"}
          </div>
        </div>
      </div>

      <div className="opr-exec-body">
        <div className="opr-exec-head">
          <div>
            <div className="opr-eyebrow">
              {replaying ? "Replaying a saved run" : "Running in your browser"}
            </div>
            <div className="opr-exec-goal">{goal}</div>
          </div>
          <div className="opr-exec-controls">
            <button
              type="button"
              className="opr-btn opr-btn-sm"
              onClick={done ? onClose : stop}
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

        <div className="opr-exec-timeline" ref={timelineRef}>
          {entries.map((entry, i) => (
            <div
              key={i}
              className={`opr-exec-act${entry.refused || entry.healed ? " is-gated" : ""}`}
            >
              <span className="opr-exec-act-label">{entry.label}</span>
              {entry.healed ? (
                <span className="opr-exec-act-why">
                  Healed — the page changed since the saved run.
                </span>
              ) : entry.reasoning ? (
                <span className="opr-exec-act-why">{entry.reasoning}</span>
              ) : null}
            </div>
          ))}
          {thinking ? (
            <div className="opr-exec-act">
              <span className="opr-exec-act-label opr-exec-thinking">
                <Loader size={12} strokeWidth={2} aria-hidden={true} />
                Thinking…
              </span>
            </div>
          ) : null}
        </div>

        <div
          className={`opr-exec-result${done ? " is-done" : ""}`}
          role="status"
        >
          {phase === "done" ? (
            <Check size={14} strokeWidth={2} aria-hidden={true} />
          ) : null}
          {result ||
            (thinking
              ? replaying
                ? "A step moved — re-finding it…"
                : "Working out the next step…"
              : replaying
                ? "Replaying your saved run…"
                : "Driving your browser…")}
        </div>
      </div>
    </RunScrim>
  );
}

// How long each action lingers in the mock timeline before the next one runs.
const STEP_MS = 1100;

const STEP_GLYPH: Record<string, string> = {
  trigger: "TR",
  enrich: "EN",
  ai: "AI",
  decision: "IF",
  action: "DO",
  branch: "EL",
};

// The scripted fallback: a realistic run for when the backend can't drive a real
// browser (no key/runner). Browser control is already granted by the shell, so
// this only carries the external-send approval gate.
function MockBrowserRun({
  toolName,
  goal,
  onClose,
}: {
  toolName: string;
  goal: string;
  onClose: () => void;
}) {
  const steps = useMemo(
    () => buildMockRun({ goal, toolName }),
    [goal, toolName],
  );
  const flat = useMemo(() => flattenRun(steps), [steps]);

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
  const url =
    [...shown].reverse().find((f) => f.action.kind === "navigate")?.action
      .value ?? "your browser";
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
    <RunScrim toolName={toolName} onClose={onClose}>
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
    </RunScrim>
  );
}
