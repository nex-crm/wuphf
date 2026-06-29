// WorkflowBuilder — the spine of the product: describe a workflow in plain
// language and watch your AI build it out in front of you.
//
// Left: the conversation. Right: the workflow, which assembles step-by-step as
// the AI understands the description, then refines one detail in place when the
// AI asks its single clarifying question. On finish it hands off a draft tool.
//
// FRONTEND-FIRST RULE: all mock. The "understanding" is planWorkflow(); the
// staged reveal and the clarify-and-refine loop are the shape to react to
// before the agentic build phase exists.

import { useEffect, useRef, useState } from "react";
import ReactMarkdown from "react-markdown";
import { ArrowRight, Check, Send, Square, X } from "lucide-react";

import { useCyclingPhrase } from "../../hooks/useCyclingPhrase";
import { OFFICE_LOADING_PHRASES } from "../../lib/officeLoadingPhrases";
import {
  type BuildActivity,
  buildPlanSmart,
  isAbortError,
  type OperatorRunResult,
  runOperatorPlan,
} from "../builder/agentClient";
import {
  type ReferencedIntegration,
  resolveReferencedIntegrations,
} from "../builder/integrationStatus";
import {
  applyClarify,
  type ClarifyQuestion,
  type WorkflowPlan,
} from "../builder/planWorkflow";
import { Eyebrow, ToolStatusBadge } from "../components/primitives";
import type { WorkflowStep } from "../mock/data";
import { ConnectionsCard } from "./ConnectionsCard";

const STEP_GLYPH: Record<WorkflowStep["kind"], string> = {
  trigger: "TR",
  enrich: "EN",
  ai: "AI",
  decision: "IF",
  action: "DO",
  branch: "EL",
};

type Phase = "intro" | "thinking" | "assembling" | "refining" | "done";

// Each streamed activity carries an id so the trace has stable React keys.
type TracedActivity = BuildActivity & { id: number };

const ACTIVE_PHASES: ReadonlySet<Phase> = new Set<Phase>([
  "thinking",
  "assembling",
]);

// One non-thinking line of the agent's workings, rendered by kind in pi's
// grammar and marked Claude-Code style (⏺ a tool call, ⎿ its result):
//  - text:        the model's prose, as markdown
//  - tool:        a tool call, mono, e.g. `$ ls` / `read path` (pi's tool title)
//  - tool_result: the tool's output, mono + dimmed, nested under the call
//  - submitted:   the milestone where the plan is registered
function ActivityLine({ activity }: { activity: TracedActivity }) {
  const { kind, text } = activity;
  if (kind === "text") {
    return (
      <div className="opr-act-line opr-act-text">
        <ReactMarkdown skipHtml={true}>{text}</ReactMarkdown>
      </div>
    );
  }
  if (kind === "tool_result") {
    return (
      <pre className="opr-act-line opr-act-result">
        <span className="opr-act-marker" aria-hidden={true}>
          ⎿
        </span>
        <span className="opr-act-result-text">{text}</span>
      </pre>
    );
  }
  if (kind === "tool" || kind === "submitted") {
    return (
      <div className={`opr-act-line opr-act-${kind}`}>
        <span className="opr-act-marker" aria-hidden={true}>
          {kind === "submitted" ? "✓" : "⏺"}
        </span>
        {text}
      </div>
    );
  }
  return null;
}

// One agentic turn, woven inline in the thread (the Claude-cowork layout): a
// collapsible Thinking block on top, then tool calls + their results and the
// model's prose in order. Open while the turn is live, collapsed once settled so
// the thread stays a clean conversation, not a wall of reasoning.
function BuildTurn({
  trace,
  active,
  meta,
}: {
  trace: TracedActivity[];
  active: boolean;
  meta?: { seconds: number; tokens: number };
}) {
  const thinking = trace.filter((t) => t.kind === "thinking");
  const steps = trace.filter(
    (t) => t.kind !== "thinking" && t.kind !== "status",
  );
  // Render the turn even before any content has streamed, so the moment the
  // agent starts working the operator sees a live "Thinking…" block (pi opens
  // its thinking block on the first reasoning signal, not when text arrives).
  if (thinking.length === 0 && steps.length === 0 && !active) return null;
  // Header: live shows "Thinking…"; once settled it carries the telemetry,
  // Claude-style: "Thinking · 12s · 10.8k tokens".
  const summary = active
    ? "Thinking…"
    : meta
      ? `Thinking · ${meta.seconds}s${meta.tokens > 0 ? ` · ${formatTokens(meta.tokens)}` : ""}`
      : "Thinking";
  const showThinkBlock = thinking.length > 0 || active;
  return (
    <div className="opr-turn">
      {showThinkBlock ? (
        <details
          className={`opr-think-block${active ? " opr-think-active" : ""}`}
          open={active}
        >
          <summary className="opr-think-summary">{summary}</summary>
          <div className="opr-think-body">
            {thinking.map((t) => (
              <div key={t.id} className="opr-act-think">
                <ReactMarkdown skipHtml={true}>{t.text}</ReactMarkdown>
              </div>
            ))}
          </div>
        </details>
      ) : null}
      {steps.map((a) => (
        <ActivityLine key={a.id} activity={a} />
      ))}
    </div>
  );
}

function formatTokens(n: number): string {
  return n >= 1000 ? `${(n / 1000).toFixed(1)}k tokens` : `${n} tokens`;
}

// The live "still working" indicator, trace-native (not the chat-bubble loader):
// soft wave dots, a cycling Office gerund, and Claude-Code-style telemetry
// (elapsed seconds · cumulative tokens). Decorative text is aria-hidden behind a
// stable status label.
function BuildingIndicator({
  elapsedMs,
  tokens,
  interruptible,
}: {
  elapsedMs: number;
  tokens: number;
  interruptible: boolean;
}) {
  const phrase = useCyclingPhrase(OFFICE_LOADING_PHRASES, 2400, true);
  const seconds = Math.floor(elapsedMs / 1000);
  return (
    <div
      className="opr-act-working"
      role="status"
      aria-label="Your AI is building the workflow"
    >
      <span className="opr-work-dots" aria-hidden={true}>
        <span />
        <span />
        <span />
      </span>
      {phrase ? (
        <span key={phrase} className="opr-work-phrase" aria-hidden={true}>
          {phrase}…
        </span>
      ) : null}
      <span className="opr-work-meta" aria-hidden={true}>
        {seconds}s{tokens > 0 ? ` · ${formatTokens(tokens)}` : ""}
        {interruptible ? " · esc to interrupt" : ""}
      </span>
    </div>
  );
}

interface FinishCard {
  name: string;
  toolId: string;
  // The step list is frozen INTO the card at finish time, so a later build in
  // the same chat can't make an older finish card reopen the newest steps.
  steps: WorkflowStep[];
}

interface BuilderMessage {
  id: string;
  from: "you" | "ai";
  body: string;
  finish?: FinishCard;
  // When present, this message is the agent's workings for a turn (thinking +
  // tool calls + prose), rendered inline by BuildTurn instead of a bubble.
  trace?: TracedActivity[];
  // Final telemetry, snapshot when the turn settles, shown on the collapsed
  // Thinking header ("Thinking · 12s · 10.8k tokens"), like Claude's summary.
  meta?: { seconds: number; tokens: number };
  // Integrations the built tool references that the operator has not connected
  // yet — rendered inline as a Connect card after the plan lands.
  connections?: ReferencedIntegration[];
}

const STARTERS: readonly string[] = [
  "When a demo request comes in, look up the company, score how good a fit they are, and send the strong ones to an AE in Slack. Nurture the rest.",
  "When a support ticket is tagged urgent, classify its severity and page the on-call engineer for the worst ones.",
  "When an expense over $5k comes in, check it against policy and route the exceptions to me to approve.",
];

const GREETING =
  "Tell me what you want to automate. Walk me through how you would do it by hand, start to finish, and I will build the workflow as you talk.";

// The workflow the builder hands off on finish. Carries the clarified steps
// (threshold/channel answers are applied into targetSteps), not just an id, so
// nothing the operator confirmed is lost when the tool opens.
export interface BuiltWorkflow {
  name: string;
  toolId: string;
  steps: WorkflowStep[];
}

export type FinishMode = "open" | "run";

/** A one-line, honest summary of a test run for the chat thread. */
function summarizeRun(finish: FinishCard, result: OperatorRunResult): string {
  let ran = 0;
  let skipped = 0;
  for (const step of finish.steps) {
    if (step.kind === "trigger") continue;
    const out = result.steps?.[step.id];
    if (!out) continue;
    if (out.skipped) skipped += 1;
    else ran += 1;
  }
  const verb = result.dry_run ? "Dry run" : "Run";
  const skippedNote = skipped > 0 ? `, ${skipped} skipped by its rules` : "";
  const tail = result.dry_run ? " Nothing was sent for real." : "";
  return `${verb} complete — ${ran} step${ran === 1 ? "" : "s"} ran${skippedNote}.${tail}`;
}

interface WorkflowBuilderProps {
  onClose: () => void;
  // mode distinguishes "open the tool" from "run on test data" so the two
  // finish actions stay distinct end to end.
  onFinish: (draft: BuiltWorkflow, mode: FinishMode) => void;
  // When set, this is the SAME build chat scoped to an existing tool — opened
  // from inside a Work Tool to change it, rather than building from scratch. It
  // only reframes the greeting/header; the engine and flow are identical.
  scopeToolName?: string;
  // Panel mode: render the chat alone (no attached workflow canvas), so it can
  // dock as a side panel. The workflow it produces is shown on the tool's own
  // Workflow screen instead, via onFinish.
  panelMode?: boolean;
  // Each operator message, so a scoped chat can navigate to the screen the
  // change is about (UI vs Workflow vs Data) before the AI even answers.
  onUserMessage?: (text: string) => void;
}

export function WorkflowBuilder({
  onClose,
  onFinish,
  scopeToolName,
  panelMode,
  onUserMessage,
}: WorkflowBuilderProps) {
  const [phase, setPhase] = useState<Phase>("intro");
  const [draft, setDraft] = useState("");
  const [messages, setMessages] = useState<BuilderMessage[]>([
    {
      id: "greet",
      from: "ai",
      body: scopeToolName
        ? `I'm your AI for ${scopeToolName}. Ask me anything about it, or tell me what to change — a step, a threshold, who gets routed, the message — and I'll rework it as you talk.`
        : GREETING,
    },
  ]);
  const [plan, setPlan] = useState<WorkflowPlan | null>(null);
  const [targetSteps, setTargetSteps] = useState<WorkflowStep[]>([]);
  const [revealCount, setRevealCount] = useState(0);
  const [pendingClarify, setPendingClarify] = useState<ClarifyQuestion | null>(
    null,
  );
  const [flashStepId, setFlashStepId] = useState<string | null>(null);
  // Live telemetry for the working indicator (elapsed seconds + cumulative
  // tokens), Claude-Code style, plus the controller that the Stop button trips.
  const [tokens, setTokens] = useState(0);
  const [elapsedMs, setElapsedMs] = useState(0);

  const timers = useRef<number[]>([]);
  const scrollRef = useRef<HTMLDivElement>(null);
  const seq = useRef(0);
  const startedAt = useRef(0);
  const abortRef = useRef<AbortController | null>(null);
  // Latest token count, mirrored to a ref so the completion handler can snapshot
  // the final telemetry onto the settled turn ("Thinking · 12s · 10.8k tokens").
  const tokensRef = useRef(0);

  function track(id: number) {
    timers.current.push(id);
  }
  useEffect(() => {
    return () => {
      timers.current.forEach((t) => clearTimeout(t));
      timers.current = [];
    };
  }, []);

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight });
  }, [messages, phase]);

  // Tick the elapsed counter while the agent is actively working.
  useEffect(() => {
    if (!ACTIVE_PHASES.has(phase)) return;
    const id = window.setInterval(() => {
      setElapsedMs(startedAt.current ? Date.now() - startedAt.current : 0);
    }, 250);
    return () => window.clearInterval(id);
  }, [phase]);

  // Esc interrupts the live build, like pi and Claude Cowork. The composer is
  // disabled while building, so the listener is global.
  useEffect(() => {
    if (phase !== "thinking") return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") abortRef.current?.abort();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [phase]);

  function nextId(prefix: string) {
    seq.current += 1;
    return `${prefix}-${seq.current}`;
  }
  function pushAI(body: string, finish?: FinishCard) {
    setMessages((prev) => [
      ...prev,
      { id: nextId("ai"), from: "ai", body, finish },
    ]);
  }
  function pushYou(body: string) {
    setMessages((prev) => [...prev, { id: nextId("you"), from: "you", body }]);
  }

  // The finish card already holds the frozen snapshot, so the handoff carries
  // exactly what was built — not just an id to a seeded mock, and not whatever
  // targetSteps happens to hold now.
  function draftFrom(finish: FinishCard): BuiltWorkflow {
    return { name: finish.name, toolId: finish.toolId, steps: finish.steps };
  }

  // Run the built plan through the real Composio executor (dry run) and report
  // the outcome inline, so the operator sees build->run close in the thread.
  async function runOnTestData(finish: FinishCard) {
    pushAI("Running it on test data…");
    try {
      const result = await runOperatorPlan(
        { name: finish.name, tool_id: finish.toolId, steps: finish.steps },
        {},
        true,
      );
      pushAI(summarizeRun(finish, result));
    } catch (err) {
      pushAI(
        `That test run did not go through: ${
          err instanceof Error ? err.message : "unknown error"
        }`,
      );
    }
  }

  function presentFinish(finish: FinishCard) {
    const t = window.setTimeout(() => {
      pushAI(
        `That is a complete workflow. I have saved it as a draft so nothing runs until you say so. Run it on a few real ones to see how it does, then publish.`,
        finish,
      );
      setPhase("done");
    }, 520);
    track(t);
  }

  function runBuild(text: string) {
    // Clear any prior draft so a retry/new build doesn't show a stale name or a
    // ghost preview from the previous workflow until the new response lands.
    setPlan(null);
    setTargetSteps([]);
    setFlashStepId(null);
    setRevealCount(0);
    setPendingClarify(null);
    setTokens(0);
    setElapsedMs(0);
    tokensRef.current = 0;
    startedAt.current = Date.now();
    const controller = new AbortController();
    abortRef.current = controller;
    setPhase("thinking");

    // Anchor the agent's workings as a message in the thread so its reasoning and
    // tool calls render INLINE, in turn order, where they happen — not in a
    // trailing box. Activity streams into this anchor's trace as it arrives.
    const traceId = nextId("trace");
    setMessages((prev) => [
      ...prev,
      { id: traceId, from: "ai", body: "", trace: [] },
    ]);
    const appendActivity = (a: BuildActivity) => {
      // Usage is telemetry, not a line: the authoritative token count from the
      // provider (includes hidden reasoning tokens), shown live and snapshot onto
      // the settled turn.
      if (a.kind === "usage") {
        tokensRef.current = a.tokens ?? 0;
        setTokens(tokensRef.current);
        return;
      }
      setMessages((prev) =>
        prev.map((m) => {
          if (m.id !== traceId) return m;
          const trace = m.trace ?? [];
          // A streaming block updates its line in place (typewriter); everything
          // else appends a new line.
          if (a.streamId) {
            const at = trace.findIndex((t) => t.streamId === a.streamId);
            if (at !== -1) {
              const next = trace.slice();
              next[at] = { ...next[at], ...a };
              return { ...m, trace: next };
            }
          }
          return { ...m, trace: [...trace, { ...a, id: trace.length }] };
        }),
      );
    };

    // Real pi-mono engine when the agent service is reachable; deterministic mock
    // otherwise (frontend-first graceful degrade). Activity streams in live; the
    // build is interruptible via the controller's signal.
    void buildPlanSmart(text, appendActivity, controller.signal)
      .then((built) => {
        setPlan(built);
        setTargetSteps(built.steps);
        // Stream is done: freeze this turn's telemetry onto its anchor so the
        // settled Thinking header carries the real elapsed + token count.
        const meta = {
          seconds: Math.round((Date.now() - startedAt.current) / 1000),
          tokens: tokensRef.current,
        };
        setMessages((prev) =>
          prev.map((m) => (m.id === traceId ? { ...m, meta } : m)),
        );

        // Classify the integrations the tool references against what the
        // operator has connected; surface a Connect card for any that are not
        // ready. Independent of the reveal timeline — lands inline near the
        // steps. Degrades silently (no card) if the catalog cannot be reached.
        const connId = nextId("conn");
        void resolveReferencedIntegrations(built.steps).then((refs) => {
          const pending = refs.filter((r) => r.readiness !== "connected");
          if (pending.length === 0) return;
          setMessages((prev) => [
            ...prev,
            { id: connId, from: "ai", body: "", connections: pending },
          ]);
        });

        const start = window.setTimeout(() => {
          pushAI(built.narration);
          setPhase("assembling");
          // The agent already built these steps; reveal them together as a
          // presentation of the real result (CSS fades them in), not a fake
          // multi-second "construction" that claims work it is not doing.
          setRevealCount(built.steps.length);
          const done = window.setTimeout(
            () => {
              if (built.clarify) {
                pushAI(built.clarify.prompt);
                setPendingClarify(built.clarify);
                setPhase("refining");
              } else {
                presentFinish({
                  name: built.name,
                  toolId: built.toolId,
                  steps: built.steps,
                });
              }
            },
            // A brief beat to let the steps settle in before the AI's follow-up.
            700,
          );
          track(done);
        }, 360);
        track(start);
      })
      .catch((err: unknown) => {
        // Distinguish an operator Stop from a real failure: the former is a
        // choice, not a problem.
        pushAI(
          isAbortError(err)
            ? "Stopped. Tell me what to change, or describe it again."
            : "I hit a problem building that one. Give it another try, or rephrase the steps.",
        );
        setPhase("intro");
      })
      .finally(() => {
        if (abortRef.current === controller) abortRef.current = null;
      });
  }

  function stopBuild() {
    abortRef.current?.abort();
  }

  function handleAnswer(
    text: string,
    clarify: ClarifyQuestion,
    p: WorkflowPlan,
  ) {
    setPhase("thinking");
    const t = window.setTimeout(() => {
      const updated = applyClarify(targetSteps, clarify.field, text);
      setTargetSteps(updated);
      setFlashStepId(clarify.stepId);
      const clearFlash = window.setTimeout(() => setFlashStepId(null), 1100);
      track(clearFlash);
      pushAI(
        clarify.field === "threshold"
          ? "Locked in. I updated the decision step to use that cutoff."
          : "Got it. I pointed the handoff at that channel.",
      );
      setPendingClarify(null);
      presentFinish({ name: p.name, toolId: p.toolId, steps: updated });
    }, 640);
    track(t);
  }

  function send(raw?: string) {
    const text = (raw ?? draft).trim();
    if (!text || phase === "thinking" || phase === "assembling") return;
    pushYou(text);
    onUserMessage?.(text);
    setDraft("");
    if (pendingClarify && plan) {
      handleAnswer(text, pendingClarify, plan);
    } else {
      runBuild(text);
    }
  }

  const visibleSteps = targetSteps.slice(0, revealCount);
  const canvasState =
    phase === "thinking"
      ? "Reading your description"
      : phase === "assembling"
        ? "Building the workflow"
        : phase === "refining"
          ? "One detail to confirm"
          : phase === "done"
            ? "Draft ready"
            : "Waiting for your description";
  const showGhost =
    (phase === "thinking" || phase === "assembling") &&
    revealCount < targetSteps.length;
  const composerLocked = phase === "thinking" || phase === "assembling";
  // The turn whose workings are still live (so only it stays expanded).
  const lastTraceId = messages.findLast((m) => m.trace)?.id;

  return (
    <div className={`opr-builder${panelMode ? " opr-builder-panel" : ""}`}>
      <div className="opr-builder-chat">
        {panelMode ? null : (
          <header className="opr-builder-head">
            <div>
              <Eyebrow>{scopeToolName ? "Ask AI" : "Build a tool"}</Eyebrow>
              <div className="opr-builder-title">
                {scopeToolName ? scopeToolName : "Describe it, I will build it"}
              </div>
            </div>
            <button
              type="button"
              className="opr-btn opr-btn-ghost opr-btn-sm"
              onClick={onClose}
              aria-label="Close builder"
            >
              <X size={13} strokeWidth={1.9} aria-hidden={true} />
              Close
            </button>
          </header>
        )}

        <div className="opr-builder-scroll" ref={scrollRef}>
          {messages.map((m) =>
            m.trace ? (
              <BuildTurn
                key={m.id}
                trace={m.trace}
                active={ACTIVE_PHASES.has(phase) && m.id === lastTraceId}
                meta={m.meta}
              />
            ) : (
              <div key={m.id} className="opr-edit-msgwrap">
                <div
                  className={`opr-msg ${
                    m.from === "ai" ? "opr-msg-ai" : "opr-msg-you"
                  }`}
                >
                  {m.body}
                </div>
                {m.connections ? (
                  <ConnectionsCard integrations={m.connections} />
                ) : null}
                {m.finish ? (
                  <div className="opr-finish-card">
                    <div className="opr-finish-row">
                      <span className="opr-finish-glyph" aria-hidden={true}>
                        <Check size={15} strokeWidth={2.2} />
                      </span>
                      <div style={{ flex: 1, minWidth: 0 }}>
                        <div className="opr-finish-name">{m.finish.name}</div>
                        <div className="opr-finish-sub">
                          <ToolStatusBadge status="draft" />
                          <span>{m.finish.steps.length}-step workflow</span>
                        </div>
                      </div>
                    </div>
                    <div className="opr-finish-actions">
                      <button
                        type="button"
                        className="opr-btn opr-btn-primary opr-btn-sm"
                        onClick={() => onFinish(draftFrom(m.finish!), "open")}
                      >
                        Open the tool
                        <ArrowRight
                          size={13}
                          strokeWidth={1.9}
                          aria-hidden={true}
                        />
                      </button>
                      <button
                        type="button"
                        className="opr-btn opr-btn-sm"
                        onClick={() => void runOnTestData(m.finish!)}
                      >
                        Run on test data
                      </button>
                    </div>
                  </div>
                ) : null}
              </div>
            ),
          )}
          {ACTIVE_PHASES.has(phase) ? (
            <BuildingIndicator
              elapsedMs={elapsedMs}
              tokens={tokens}
              interruptible={phase === "thinking"}
            />
          ) : null}
        </div>

        {phase === "intro" ? (
          <div className="opr-starters">
            <div className="opr-starters-label">Or start from one of these</div>
            {STARTERS.map((s) => (
              <button
                key={s}
                type="button"
                className="opr-starter-chip"
                onClick={() => send(s)}
              >
                {s}
              </button>
            ))}
          </div>
        ) : null}

        <div className="opr-composer">
          <input
            className="opr-composer-input"
            aria-label="Describe the workflow you want to build"
            placeholder={
              pendingClarify
                ? "Type your answer..."
                : "Describe what should happen, step by step..."
            }
            value={draft}
            disabled={composerLocked}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") send();
            }}
          />
          {phase === "thinking" ? (
            <button
              type="button"
              className="opr-btn opr-btn-stop"
              onClick={stopBuild}
              aria-label="Stop building"
            >
              <Square size={12} strokeWidth={2.4} aria-hidden={true} />
              Stop
            </button>
          ) : (
            <button
              type="button"
              className="opr-btn opr-btn-primary"
              onClick={() => send()}
              disabled={composerLocked}
            >
              <Send size={14} strokeWidth={1.9} aria-hidden={true} />
              Send
            </button>
          )}
        </div>
      </div>

      {panelMode ? null : (
      <aside className="opr-builder-canvas" aria-label="Workflow preview">
        <div className="opr-canvas-head">
          <Eyebrow>{plan ? plan.name : "Your workflow"}</Eyebrow>
          <span className="opr-canvas-state">
            <span
              className={`opr-led ${
                phase === "done"
                  ? "opr-led-live"
                  : phase === "intro"
                    ? "opr-led-draft"
                    : "opr-led-suggested"
              }`}
            />
            {canvasState}
          </span>
        </div>

        {visibleSteps.length === 0 && !showGhost ? (
          <div className="opr-canvas-empty">
            <div className="opr-canvas-empty-glyph" aria-hidden={true}>
              ◇
            </div>
            <div className="opr-canvas-empty-title">
              Your workflow takes shape here
            </div>
            <p className="opr-canvas-empty-hint">
              As you describe what should happen, each step appears on this
              side, wired up and scripted, so you can see exactly what your AI
              is building.
            </p>
          </div>
        ) : (
          <div className="opr-flow opr-flow-building">
            {visibleSteps.map((step, i) => (
              <div
                className={`opr-step opr-step-reveal${
                  flashStepId === step.id ? " opr-step-flash" : ""
                }`}
                key={step.id}
              >
                <div className="opr-step-rail">
                  <div
                    className={`opr-step-node opr-step-node-${step.kind}`}
                    aria-hidden={true}
                  >
                    {STEP_GLYPH[step.kind]}
                  </div>
                  {i < visibleSteps.length - 1 || showGhost ? (
                    <div className="opr-step-line" />
                  ) : null}
                </div>
                <div className="opr-step-body">
                  <div className="opr-step-kind">{step.kind}</div>
                  <div className="opr-step-title">
                    {step.title}
                    {step.integration ? (
                      <span className="opr-step-chip">{step.integration}</span>
                    ) : null}
                  </div>
                  <div className="opr-step-detail">{step.detail}</div>
                  {step.gated ? (
                    <div className="opr-step-gate">
                      Approval required before it sends
                    </div>
                  ) : null}
                </div>
              </div>
            ))}
            {showGhost ? (
              <div className="opr-step opr-step-ghost">
                <div className="opr-step-rail">
                  <div
                    className="opr-step-node opr-step-node-ghost"
                    aria-hidden={true}
                  >
                    <span className="opr-think-dot" />
                    <span className="opr-think-dot" />
                    <span className="opr-think-dot" />
                  </div>
                </div>
                <div className="opr-step-body">
                  <div className="opr-step-detail">
                    working out the next step...
                  </div>
                </div>
              </div>
            ) : null}
          </div>
        )}
      </aside>
      )}
    </div>
  );
}
