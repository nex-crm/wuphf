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
import { ArrowRight, Check, Send, X } from "lucide-react";

import { useCyclingPhrase } from "../../hooks/useCyclingPhrase";
import { OFFICE_LOADING_PHRASES } from "../../lib/officeLoadingPhrases";
import { type BuildActivity, buildPlanSmart } from "../builder/agentClient";
import {
  applyClarify,
  type ClarifyQuestion,
  type WorkflowPlan,
} from "../builder/planWorkflow";
import { Eyebrow, ToolStatusBadge } from "../components/primitives";
import type { WorkflowStep } from "../mock/data";

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
}: {
  trace: TracedActivity[];
  active: boolean;
}) {
  const thinking = trace.filter((t) => t.kind === "thinking");
  const steps = trace.filter(
    (t) => t.kind !== "thinking" && t.kind !== "status",
  );
  if (thinking.length === 0 && steps.length === 0) return null;
  return (
    <div className="opr-turn">
      {thinking.length > 0 ? (
        <details className="opr-think-block" open={active}>
          <summary className="opr-think-summary">Thinking</summary>
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

// The live "still working" indicator, trace-native (not the chat-bubble loader):
// soft wave dots plus a cycling Office gerund, in the builder's own type so it
// reads as part of the workings, not a message. Decorative text is aria-hidden
// behind a stable status label.
function BuildingIndicator() {
  const phrase = useCyclingPhrase(OFFICE_LOADING_PHRASES, 2400, true);
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

interface WorkflowBuilderProps {
  onClose: () => void;
  // mode distinguishes "open the tool" from "run on test data" so the two
  // finish actions stay distinct end to end.
  onFinish: (draft: BuiltWorkflow, mode: FinishMode) => void;
}

export function WorkflowBuilder({ onClose, onFinish }: WorkflowBuilderProps) {
  const [phase, setPhase] = useState<Phase>("intro");
  const [draft, setDraft] = useState("");
  const [messages, setMessages] = useState<BuilderMessage[]>([
    { id: "greet", from: "ai", body: GREETING },
  ]);
  const [plan, setPlan] = useState<WorkflowPlan | null>(null);
  const [targetSteps, setTargetSteps] = useState<WorkflowStep[]>([]);
  const [revealCount, setRevealCount] = useState(0);
  const [pendingClarify, setPendingClarify] = useState<ClarifyQuestion | null>(
    null,
  );
  const [flashStepId, setFlashStepId] = useState<string | null>(null);

  const timers = useRef<number[]>([]);
  const scrollRef = useRef<HTMLDivElement>(null);
  const seq = useRef(0);

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
    setPhase("thinking");

    // Anchor the agent's workings as a message in the thread so its reasoning and
    // tool calls render INLINE, in turn order, where they happen — not in a
    // trailing box. Activity streams into this anchor's trace as it arrives.
    const traceId = nextId("trace");
    setMessages((prev) => [
      ...prev,
      { id: traceId, from: "ai", body: "", trace: [] },
    ]);
    const appendActivity = (a: BuildActivity) =>
      setMessages((prev) =>
        prev.map((m) =>
          m.id === traceId
            ? {
                ...m,
                trace: [...(m.trace ?? []), { ...a, id: m.trace?.length ?? 0 }],
              }
            : m,
        ),
      );

    // Real pi-mono engine when the agent service is reachable; deterministic mock
    // otherwise (frontend-first graceful degrade). The thinking phase covers the
    // round-trip; activity streams in live; the staggered reveal below is unchanged.
    void buildPlanSmart(text, appendActivity)
      .then((built) => {
        setPlan(built);
        setTargetSteps(built.steps);

        const start = window.setTimeout(() => {
          pushAI(built.narration);
          setPhase("assembling");
          built.steps.forEach((_, i) => {
            const reveal = window.setTimeout(
              () => setRevealCount((c) => Math.max(c, i + 1)),
              280 + i * 440,
            );
            track(reveal);
          });
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
            280 + built.steps.length * 440 + 240,
          );
          track(done);
        }, 720);
        track(start);
      })
      .catch(() => {
        // A reachable service that errored (the mock only covers unreachable).
        // Tell the operator plainly and let them retry, rather than hang.
        pushAI(
          "I hit a problem building that one. Give it another try, or rephrase the steps.",
        );
        setPhase("intro");
      });
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
    <div className="opr-builder">
      <div className="opr-builder-chat">
        <header className="opr-builder-head">
          <div>
            <Eyebrow>Build a tool</Eyebrow>
            <div className="opr-builder-title">
              Describe it, I will build it
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

        <div className="opr-builder-scroll" ref={scrollRef}>
          {messages.map((m) =>
            m.trace ? (
              <BuildTurn
                key={m.id}
                trace={m.trace}
                active={ACTIVE_PHASES.has(phase) && m.id === lastTraceId}
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
                        onClick={() => onFinish(draftFrom(m.finish!), "run")}
                      >
                        Run on test data
                      </button>
                    </div>
                  </div>
                ) : null}
              </div>
            ),
          )}
          {ACTIVE_PHASES.has(phase) ? <BuildingIndicator /> : null}
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
          <button
            type="button"
            className="opr-btn opr-btn-primary"
            onClick={() => send()}
            disabled={composerLocked}
          >
            <Send size={14} strokeWidth={1.9} aria-hidden={true} />
            Send
          </button>
        </div>
      </div>

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
    </div>
  );
}
