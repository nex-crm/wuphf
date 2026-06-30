// Scheduled delivery for an app: reuse the shipped scheduler + action-grant
// stack (no new backend) to run an app's job on a cadence and post the result
// to Slack without a per-send approval. An agent routine fires on the cron and
// posts the prompt to the owner agent; a standing Slack grant lets that agent
// send without a human click; run-once force-fires the job for a live test.

import { post } from "../../api/client";

/** The Composio Slack "post a message" action the grant authorizes. */
export const SLACK_SEND_ACTION = "SLACK_SENDS_A_MESSAGE_TO_A_SLACK_CHANNEL";

/** Daily at 09:00. */
export const DAILY_9AM_CRON = "0 9 * * *";

export interface ScheduleDeliveryInput {
  /** App being delivered (for the routine label). */
  appName: string;
  /** Agent that runs the routine and posts to Slack. */
  agentSlug: string;
  /** Slack channel to post into, e.g. "#general". */
  slackChannel: string;
  /** Cron expression for the cadence. */
  cron: string;
  /** Full instruction posted to the agent on each fire. */
  prompt: string;
}

/**
 * Grant the agent standing permission to post to Slack so a scheduled send
 * never raises a human approval card. Exact (agent, platform, action) match —
 * no wildcards. Idempotent server-side (re-granting is a no-op refresh).
 */
export async function grantSlackSend(agentSlug: string): Promise<void> {
  await post("/integrations/grants", {
    action: "grant",
    agent_slug: agentSlug,
    platform: "slack",
    action_scope: SLACK_SEND_ACTION,
  });
}

/** Register (or update) the daily agent routine. Returns its job slug. */
export async function scheduleRoutine(
  input: ScheduleDeliveryInput,
): Promise<string> {
  const res = await post<{ job?: { slug?: string } }>("/scheduler/routines", {
    purpose: `Daily Slack delivery: ${input.appName}`,
    schedule: input.cron,
    owner: input.agentSlug,
    channel: "general",
    prompt: input.prompt,
    created_by: "human",
  });
  const slug = res.job?.slug;
  if (!slug) {
    throw new Error("Scheduler did not return a job slug");
  }
  return slug;
}

/** Force-fire the routine once now, for a live test. */
export async function runScheduledJob(slug: string): Promise<void> {
  await post(`/scheduler/${encodeURIComponent(slug)}/run`, {});
}

interface BridgeResult {
  connected?: boolean;
  status?: string;
  result?: unknown;
  error?: string;
  request_id?: string;
}

/**
 * runDeliveryOnce performs the digest delivery directly, here and now, so a
 * human can test it: read the last 24h of email, compose the digest with ai(),
 * then ATTEMPT the Slack post. The post is a mutation, so the broker returns
 * needs_approval + a request id instead of sending — the operator surfaces that
 * approval and the human clicks Approve. Returns the request id to approve, or
 * an error string. We never auto-send; the human-in-the-loop is the point.
 */
export async function runDeliveryOnce(
  appId: string,
  channel: string,
): Promise<{ requestId?: string; error?: string }> {
  const gmail = await post<BridgeResult>("/apps/integrations/call", {
    platform: "gmail",
    action: "GMAIL_FETCH_EMAILS",
    params: { query: "newer_than:1d", max_results: 25 },
    app_id: appId,
  });
  if (gmail.error) return { error: gmail.error };

  const aiRes = await post<{ result?: unknown }>("/apps/ai", {
    prompt:
      "Write a Slack message (mrkdwn: *bold*, bullets): a daily digest of the most important action items from these emails, grouped per entity (company/person), each item with a one-line why-it-matters-for-Nex.ai and an urgency score 0-100 from the email body, ordered most urgent first. Keep it tight.",
    input: gmail.result ?? [],
    json: false,
    app_id: appId,
  });
  const text =
    typeof aiRes.result === "string"
      ? aiRes.result
      : JSON.stringify(aiRes.result ?? "");

  const send = await post<BridgeResult>("/apps/integrations/call", {
    platform: "slack",
    action: SLACK_SEND_ACTION,
    params: {
      channel,
      text:
        text.trim() || "Nex daily digest — no notable items in the last 24h.",
    },
    app_id: appId,
  });
  if (send.status === "needs_approval") return { requestId: send.request_id };
  if (send.error) return { error: send.error };
  return {};
}

/**
 * Compose the agent instruction. It restates the digest spec so the routine is
 * self-contained (the agent, not the HTML app, runs it on the schedule) and
 * ends by posting to the chosen Slack channel.
 */
export function composeDeliveryPrompt(
  appName: string,
  slackChannel: string,
): string {
  return [
    `Run the "${appName}" digest now and deliver it to Slack.`,
    "",
    "Produce a daily digest of the most important action items from the last 24 hours of email, viewed entirely through the lens of relevance to Nex.ai as a company. Group the work by entity (the company or person it concerns). For each entity give its action items — each with a one-line why-it-matters-for-Nex.ai — and an urgency score 0-100 derived from the email body. Order entities by urgency, most urgent first.",
    "",
    `Then post the digest as a clear, well-formatted message to the Slack channel ${slackChannel}.`,
  ].join("\n");
}
