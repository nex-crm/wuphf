// AppDeliverySchedule — the "deliver this app to Slack on a schedule" control
// that lives in an app's Workflow tab. It composes the shipped scheduler +
// action-grant stack: a standing Slack grant + a daily agent routine, plus a
// "Run now" button that force-fires the routine once for a live test. No new
// backend — this is the automation behind the screen.

import { useState } from "react";
import { Clock, Play, Send } from "lucide-react";

import { showNotice } from "../../components/ui/Toast";
import {
  composeDeliveryPrompt,
  DAILY_9AM_CRON,
  grantSlackSend,
  runDeliveryOnce,
  scheduleRoutine,
} from "../apps/scheduleClient";
import { Eyebrow } from "../components/primitives";

// The agent that runs the routine and posts to Slack. Executor is the office's
// doer; it shares the workspace's Gmail + Slack connections.
const DELIVERY_AGENT = "executor";

interface AppDeliveryScheduleProps {
  appName: string;
  appId: string;
}

type Phase = "idle" | "scheduling" | "scheduled" | "running" | "ran";

export function AppDeliverySchedule({
  appName,
  appId,
}: AppDeliveryScheduleProps) {
  const [channel, setChannel] = useState("#general");
  const [phase, setPhase] = useState<Phase>("idle");

  async function schedule(): Promise<void> {
    const target = channel.trim() || "#general";
    setPhase("scheduling");
    try {
      await grantSlackSend(DELIVERY_AGENT);
      await scheduleRoutine({
        appName,
        agentSlug: DELIVERY_AGENT,
        slackChannel: target,
        cron: DAILY_9AM_CRON,
        prompt: composeDeliveryPrompt(appName, target),
      });
      setPhase("scheduled");
      showNotice(`Scheduled daily delivery to ${target} at 9:00am`, "success");
    } catch {
      setPhase("idle");
      showNotice("Could not schedule the delivery.", "error");
    }
  }

  // Test it now: build the digest and ATTEMPT the Slack post. The post raises a
  // human approval (the operator surfaces it) — we never auto-send.
  async function runNow(): Promise<void> {
    const target = channel.trim() || "#general";
    setPhase("running");
    try {
      const res = await runDeliveryOnce(appId, target);
      if (res.error) {
        setPhase("scheduled");
        showNotice(`Could not run the digest: ${res.error}`, "error");
        return;
      }
      setPhase("ran");
      showNotice(
        res.requestId
          ? `Digest ready — approve the Slack post to ${target} to deliver it.`
          : `Digest delivered to ${target}.`,
        "success",
      );
    } catch {
      setPhase("scheduled");
      showNotice("Could not run the delivery.", "error");
    }
  }

  const scheduled =
    phase === "scheduled" || phase === "running" || phase === "ran";

  return (
    <div className="opr-tool-scoped opr-delivery">
      <DigestWorkflowFlow
        channel={channel.trim() || "#general"}
        scheduled={scheduled}
      />

      <div className="opr-delivery-head">
        <span className="opr-delivery-glyph" aria-hidden={true}>
          <Send size={16} strokeWidth={1.8} />
        </span>
        <div>
          <div className="opr-delivery-title">Deliver to Slack</div>
          <p className="opr-scoped-note">
            Post this digest to Slack every day at 9:00am. {DELIVERY_AGENT} runs
            it on the schedule and is pre-authorized to send, so it never waits
            on a click.
          </p>
        </div>
      </div>

      <label className="opr-delivery-field">
        <span className="opr-delivery-label">Slack channel</span>
        <input
          className="opr-conn-input"
          type="text"
          value={channel}
          onChange={(e) => setChannel(e.target.value)}
          placeholder="#general"
          disabled={scheduled}
          aria-label="Slack channel"
        />
      </label>

      <div className="opr-delivery-cadence">
        <Clock size={13} strokeWidth={1.9} aria-hidden={true} />
        Every day at 9:00am
      </div>

      <div className="opr-delivery-actions">
        {scheduled ? (
          <>
            <span className="opr-pill opr-pill-good">
              <span className="opr-led opr-led-live" />
              Scheduled
            </span>
            <button
              type="button"
              className="opr-btn opr-btn-sm"
              onClick={() => void runNow()}
              disabled={phase === "running"}
            >
              <Play size={13} strokeWidth={1.9} aria-hidden={true} />
              {phase === "running" ? "Running…" : "Run once now"}
            </button>
          </>
        ) : (
          <button
            type="button"
            className="opr-btn opr-btn-primary opr-btn-sm"
            onClick={() => void schedule()}
            disabled={phase === "scheduling"}
          >
            <Send size={13} strokeWidth={1.9} aria-hidden={true} />
            {phase === "scheduling" ? "Scheduling…" : "Schedule daily delivery"}
          </button>
        )}
      </div>

      {phase === "ran" ? (
        <p className="opr-scoped-note">
          Digest built. An approval to post it to {channel.trim() || "#general"}{" "}
          is waiting — approve it and it goes to Slack.
        </p>
      ) : null}
    </div>
  );
}

// ── Workflow flow diagram (trigger → read → analyze → decide → branch → send) ──

type FlowKind = "trigger" | "enrich" | "ai" | "decision" | "action" | "branch";

const FLOW_GLYPH: Record<FlowKind, string> = {
  trigger: "TR",
  enrich: "EN",
  ai: "AI",
  decision: "IF",
  action: "DO",
  branch: "EL",
};

interface FlowStep {
  id: string;
  kind: FlowKind;
  title: string;
  detail: string;
  integration?: string;
  gated?: boolean;
}

function digestSteps(channel: string): FlowStep[] {
  return [
    {
      id: "trigger",
      kind: "trigger",
      title: "Every day at 9:00 AM",
      detail:
        'Runs on a daily schedule. "Run once now" fires it immediately to test.',
    },
    {
      id: "read",
      kind: "enrich",
      title: "Read the last 24 hours of email",
      integration: "Gmail",
      detail: "Fetch messages newer than 1 day, read-only, through the bridge.",
    },
    {
      id: "analyze",
      kind: "ai",
      title: "Score and group by entity",
      detail:
        "AI ranks the action items per entity by urgency (0–100) from the email body, through the Nex.ai relevance lens.",
    },
    {
      id: "decision",
      kind: "decision",
      title: "Anything urgent to send?",
      detail: "Branch on whether the digest has action items worth delivering.",
    },
    {
      id: "branch",
      kind: "branch",
      title: "If nothing urgent — skip",
      detail: "No empty digests: on a quiet day, nothing is posted.",
    },
    {
      id: "send",
      kind: "action",
      title: "Post the digest to Slack",
      integration: "Slack",
      detail: `Send the formatted per-entity digest to ${channel}.`,
      gated: true,
    },
  ];
}

function DigestWorkflowFlow({
  channel,
  scheduled,
}: {
  channel: string;
  scheduled: boolean;
}) {
  const steps = digestSteps(channel);
  return (
    <div className="opr-delivery-flow">
      <Eyebrow>How it runs · trigger to delivery</Eyebrow>
      <div className="opr-flow" style={{ marginTop: "var(--space-3)" }}>
        {steps.map((step, i) => (
          <div className="opr-step" key={step.id}>
            <div className="opr-step-rail">
              <div
                className={`opr-step-node opr-step-node-${step.kind}`}
                aria-hidden={true}
              >
                {FLOW_GLYPH[step.kind]}
              </div>
              {i < steps.length - 1 ? <div className="opr-step-line" /> : null}
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
                  {scheduled
                    ? "Scheduled — pre-authorized to send"
                    : "Approval required before it sends"}
                </div>
              ) : null}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
