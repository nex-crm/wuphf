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
import { ArrowRight, Check, Send, X } from "lucide-react";

import { Eyebrow, ToolStatusBadge } from "../components/primitives";
import { buildPlanSmart } from "../builder/agentClient";
import {
  applyClarify,
  type ClarifyQuestion,
  type WorkflowPlan,
} from "../builder/planWorkflow";
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

interface FinishCard {
  name: string;
  toolId: string;
}

interface BuilderMessage {
  id: string;
  from: "you" | "ai";
  body: string;
  finish?: FinishCard;
}

const STARTERS: readonly string[] = [
  "When a demo request comes in, look up the company, score how good a fit they are, and send the strong ones to an AE in Slack. Nurture the rest.",
  "When a support ticket is tagged urgent, classify its severity and page the on-call engineer for the worst ones.",
  "When an expense over $5k comes in, check it against policy and route the exceptions to me to approve.",
];

const GREETING =
  "Tell me what you want to automate. Walk me through how you would do it by hand, start to finish, and I will build the workflow as you talk.";

interface WorkflowBuilderProps {
  onClose: () => void;
  onFinish: (toolId: string) => void;
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

  function presentFinish(p: WorkflowPlan) {
    const t = window.setTimeout(() => {
      pushAI(
        `That is a complete workflow. I have saved it as a draft so nothing runs until you say so. Run it on a few real ones to see how it does, then publish.`,
        { name: p.name, toolId: p.toolId },
      );
      setPhase("done");
    }, 520);
    track(t);
  }

  function runBuild(text: string) {
    setRevealCount(0);
    setPendingClarify(null);
    setPhase("thinking");
    // Real pi-mono engine when the agent service is reachable; deterministic mock
    // otherwise (frontend-first graceful degrade). The thinking phase covers the
    // round-trip; the staggered reveal below is unchanged.
    void buildPlanSmart(text).then((built) => {
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
            presentFinish(built);
          }
        },
        280 + built.steps.length * 440 + 240,
      );
      track(done);
    }, 720);
    track(start);
    });
  }

  function handleAnswer(text: string, clarify: ClarifyQuestion, p: WorkflowPlan) {
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
      presentFinish(p);
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

  return (
    <div className="opr-builder">
      <div className="opr-builder-chat">
        <header className="opr-builder-head">
          <div>
            <Eyebrow>Build a tool</Eyebrow>
            <div className="opr-builder-title">Describe it, I will build it</div>
          </div>
          <button
            type="button"
            className="opr-btn opr-btn-ghost opr-btn-sm"
            onClick={onClose}
            aria-label="Close builder"
          >
            <X size={13} strokeWidth={1.9} aria-hidden />
            Close
          </button>
        </header>

        <div className="opr-builder-scroll" ref={scrollRef}>
          {messages.map((m) => (
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
                    <span className="opr-finish-glyph" aria-hidden>
                      <Check size={15} strokeWidth={2.2} />
                    </span>
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <div className="opr-finish-name">{m.finish.name}</div>
                      <div className="opr-finish-sub">
                        <ToolStatusBadge status="draft" />
                        <span>{targetSteps.length}-step workflow</span>
                      </div>
                    </div>
                  </div>
                  <div className="opr-finish-actions">
                    <button
                      type="button"
                      className="opr-btn opr-btn-primary opr-btn-sm"
                      onClick={() => onFinish(m.finish!.toolId)}
                    >
                      Open the tool
                      <ArrowRight size={13} strokeWidth={1.9} aria-hidden />
                    </button>
                    <button
                      type="button"
                      className="opr-btn opr-btn-sm"
                      onClick={() => onFinish(m.finish!.toolId)}
                    >
                      Run on test data
                    </button>
                  </div>
                </div>
              ) : null}
            </div>
          ))}
          {phase === "thinking" ? (
            <div className="opr-msg opr-msg-ai opr-thinking" aria-live="polite">
              <span className="opr-think-dot" />
              <span className="opr-think-dot" />
              <span className="opr-think-dot" />
            </div>
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
          <button
            type="button"
            className="opr-btn opr-btn-primary"
            onClick={() => send()}
            disabled={composerLocked}
          >
            <Send size={14} strokeWidth={1.9} aria-hidden />
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
            <div className="opr-canvas-empty-glyph" aria-hidden>
              ◇
            </div>
            <div className="opr-canvas-empty-title">
              Your workflow takes shape here
            </div>
            <p className="opr-canvas-empty-hint">
              As you describe what should happen, each step appears on this side,
              wired up and scripted, so you can see exactly what your AI is
              building.
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
                    aria-hidden
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
                  <div className="opr-step-node opr-step-node-ghost" aria-hidden>
                    <span className="opr-think-dot" />
                    <span className="opr-think-dot" />
                    <span className="opr-think-dot" />
                  </div>
                </div>
                <div className="opr-step-body">
                  <div className="opr-step-detail">working out the next step...</div>
                </div>
              </div>
            ) : null}
          </div>
        )}
      </aside>
    </div>
  );
}
