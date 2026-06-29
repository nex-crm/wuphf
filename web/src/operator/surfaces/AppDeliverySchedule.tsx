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
  runScheduledJob,
  scheduleRoutine,
} from "../apps/scheduleClient";

// The agent that runs the routine and posts to Slack. Executor is the office's
// doer; it shares the workspace's Gmail + Slack connections.
const DELIVERY_AGENT = "executor";

interface AppDeliveryScheduleProps {
  appName: string;
}

type Phase = "idle" | "scheduling" | "scheduled" | "running" | "ran";

export function AppDeliverySchedule({ appName }: AppDeliveryScheduleProps) {
  const [channel, setChannel] = useState("#general");
  const [phase, setPhase] = useState<Phase>("idle");
  const [slug, setSlug] = useState<string | null>(null);

  async function schedule(): Promise<void> {
    const target = channel.trim() || "#general";
    setPhase("scheduling");
    try {
      await grantSlackSend(DELIVERY_AGENT);
      const jobSlug = await scheduleRoutine({
        appName,
        agentSlug: DELIVERY_AGENT,
        slackChannel: target,
        cron: DAILY_9AM_CRON,
        prompt: composeDeliveryPrompt(appName, target),
      });
      setSlug(jobSlug);
      setPhase("scheduled");
      showNotice(`Scheduled daily delivery to ${target} at 9:00am`, "success");
    } catch {
      setPhase("idle");
      showNotice("Could not schedule the delivery.", "error");
    }
  }

  async function runNow(): Promise<void> {
    if (!slug) return;
    setPhase("running");
    try {
      await runScheduledJob(slug);
      setPhase("ran");
      showNotice(
        `Running now — ${DELIVERY_AGENT} will post the digest to ${channel.trim() || "#general"} shortly.`,
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
          Test run kicked off. {DELIVERY_AGENT} is composing the digest and
          posting it to {channel.trim() || "#general"} — check Slack in a
          moment.
        </p>
      ) : null}
    </div>
  );
}
