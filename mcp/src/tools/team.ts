import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { readFileSync } from "node:fs";
import { z } from "zod";

const brokerBaseUrl = (process.env.WUPHF_TEAM_BROKER_URL ?? process.env.NEX_TEAM_BROKER_URL ?? "http://127.0.0.1:7890").replace(/\/+$/, "");
const brokerTokenPath = process.env.WUPHF_BROKER_TOKEN_FILE ?? process.env.NEX_BROKER_TOKEN_FILE ?? "/tmp/wuphf-broker-token";

type BrokerMessage = {
  id: string;
  from: string;
  kind?: string;
  source?: string;
  source_label?: string;
  event_id?: string;
  title?: string;
  content: string;
  tagged?: string[];
  reply_to?: string;
  timestamp: string;
};

type InterviewOption = {
  id: string;
  label: string;
  description?: string;
};

type InterviewAnswer = {
  choice_id?: string;
  choice_text?: string;
  custom_text?: string;
  answered_at?: string;
};

function resolveSlug(input?: string): string {
  const slug = input ?? process.env.WUPHF_AGENT_SLUG ?? process.env.NEX_AGENT_SLUG ?? "";
  if (!slug) {
    throw new Error("Missing agent slug. Pass my_slug explicitly or set WUPHF_AGENT_SLUG.");
  }
  return slug;
}

function authHeaders(): Record<string, string> {
  const token = (process.env.WUPHF_BROKER_TOKEN ?? process.env.NEX_BROKER_TOKEN ?? "").trim() || readBrokerTokenFile();
  if (!token) return {};
  return { Authorization: `Bearer ${token}` };
}

function readBrokerTokenFile(): string {
  try {
    return readFileSync(brokerTokenPath, "utf8").trim();
  } catch {
    return "";
  }
}

async function brokerGet(path: string): Promise<unknown> {
  const res = await fetch(`${brokerBaseUrl}${path}`, {
    headers: { ...authHeaders() },
    signal: AbortSignal.timeout(10_000),
  });
  if (!res.ok) {
    throw new Error(`Broker GET ${path} failed: ${res.status} ${res.statusText}`);
  }
  return res.json();
}

async function brokerPost(path: string, body: unknown): Promise<unknown> {
  const res = await fetch(`${brokerBaseUrl}${path}`, {
    method: "POST",
    headers: { "Content-Type": "application/json", ...authHeaders() },
    body: JSON.stringify(body),
    signal: AbortSignal.timeout(10_000),
  });
  if (!res.ok) {
    throw new Error(`Broker POST ${path} failed: ${res.status} ${res.statusText}`);
  }
  return res.json();
}

async function sleep(ms: number): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, ms));
}

function formatMessages(messages: BrokerMessage[], mySlug?: string): string {
  if (messages.length === 0) {
    return "No recent team messages.";
  }

  const lines = messages.map((message) => {
    const ts = message.timestamp.length > 19 ? message.timestamp.slice(11, 19) : message.timestamp;
    const mentionsMe = !!mySlug && (message.tagged ?? []).includes(mySlug);
    const tagNote = mentionsMe ? " [tagged you]" : "";
    const threadNote = message.reply_to ? ` ↳ ${message.reply_to}` : "";
    if (message.kind === "automation" || message.from === "wuphf") {
      const source = message.source ?? "context_graph";
      const label = message.source_label ?? "WUPHF";
      const title = message.title ? `${message.title}: ` : "";
      return `${ts} ${message.id}${threadNote} [${label}/${source}]: ${title}${message.content}${tagNote}`;
    }
    return `${ts} ${message.id}${threadNote} @${message.from}: ${message.content}${tagNote}`;
  });
  return lines.join("\n");
}

function isRecentEnough(timestamp: string, maxAgeMs: number): boolean {
  const parsed = Date.parse(timestamp);
  if (Number.isNaN(parsed)) {
    return false;
  }
  return Date.now() - parsed <= maxAgeMs;
}

async function inferReplyTarget(slug: string): Promise<string | undefined> {
  const params = new URLSearchParams();
  params.set("my_slug", slug);
  params.set("limit", "25");
  const result = await brokerGet(`/messages?${params.toString()}`) as {
    messages?: BrokerMessage[];
  };
  const messages = result.messages ?? [];
  for (let i = messages.length - 1; i >= 0; i--) {
    const message = messages[i];
    if (message.from === slug) {
      continue;
    }
    if (!(message.tagged ?? []).includes(slug)) {
      continue;
    }
    if (!isRecentEnough(message.timestamp, 15 * 60 * 1000)) {
      continue;
    }
    return message.id;
  }
  return undefined;
}

async function inferDefaultThreadTarget(slug: string): Promise<string | undefined> {
  const params = new URLSearchParams();
  params.set("my_slug", slug);
  params.set("limit", "40");
  const result = await brokerGet(`/messages?${params.toString()}`) as {
    messages?: BrokerMessage[];
  };
  const messages = result.messages ?? [];
  for (let i = messages.length - 1; i >= 0; i--) {
    const message = messages[i];
    if (message.from === slug) {
      continue;
    }
    if (message.content.startsWith("[STATUS]")) {
      continue;
    }
    if (!isRecentEnough(message.timestamp, 20 * 60 * 1000)) {
      continue;
    }
    return message.id;
  }
  return undefined;
}

export function registerTeamTools(server: McpServer) {
  server.tool(
    "team_broadcast",
    "Post a message into the shared team channel so the human and every agent can see it.",
    {
      content: z.string().describe("Message to post to the shared team channel"),
      my_slug: z.string().optional().describe("Agent slug sending the message. Defaults to WUPHF_AGENT_SLUG."),
      tagged: z.array(z.string()).optional().describe("Optional list of tagged agent slugs who should respond"),
      reply_to_id: z.string().optional().describe("Reply in-thread to a specific message ID when continuing a narrow discussion"),
      new_topic: z.boolean().optional().describe("Set true only when this genuinely needs to start a new top-level thread"),
    },
    { readOnlyHint: false, openWorldHint: true },
    async ({ content, my_slug, tagged, reply_to_id, new_topic }) => {
      const slug = resolveSlug(my_slug);
      let replyTo = reply_to_id;
      if (!replyTo && !new_topic) {
        replyTo = await inferReplyTarget(slug);
      }
      if (!replyTo && !new_topic) {
        replyTo = await inferDefaultThreadTarget(slug);
      }
      const result = await brokerPost("/messages", {
        from: slug,
        content,
        tagged: tagged ?? [],
        reply_to: replyTo,
      }) as { id?: string; total?: number };
      return {
        content: [{
          type: "text",
          text: `Posted to team channel as @${slug}${result.id ? ` (${result.id})` : ""}${replyTo ? ` in reply to ${replyTo}` : ""}.`,
        }],
      };
    },
  );

  server.tool(
    "team_poll",
    "Read recent messages from the shared team channel so you can stay in sync with teammates.",
    {
      my_slug: z.string().optional().describe("Your agent slug so tagged_count can be computed. Defaults to WUPHF_AGENT_SLUG."),
      since_id: z.string().optional().describe("Only return messages after this message ID"),
      limit: z.number().optional().describe("Maximum messages to return (default 20, max 100)"),
    },
    { readOnlyHint: true, openWorldHint: true },
    async ({ my_slug, since_id, limit }) => {
      const slug = my_slug ?? process.env.WUPHF_AGENT_SLUG ?? process.env.NEX_AGENT_SLUG;
      const params = new URLSearchParams();
      if (slug) params.set("my_slug", slug);
      if (since_id) params.set("since_id", since_id);
      if (limit !== undefined) params.set("limit", String(limit));
      const qs = params.toString();
      const result = await brokerGet(`/messages${qs ? `?${qs}` : ""}`) as {
        messages?: BrokerMessage[];
        tagged_count?: number;
      };
      const messages = result.messages ?? [];
      const taggedCount = result.tagged_count ?? 0;
      const summary = formatMessages(messages, slug);
      return {
        content: [{
          type: "text",
          text: `${summary}\n\nTagged messages for you: ${taggedCount}`,
        }],
      };
    },
  );

  server.tool(
    "team_status",
    "Share a short status update in the team channel. This is rendered as lightweight activity in the channel UI.",
    {
      status: z.string().describe("Short status like 'reviewing onboarding flow' or 'implementing search index'"),
      my_slug: z.string().optional().describe("Agent slug sending the status. Defaults to WUPHF_AGENT_SLUG."),
    },
    { readOnlyHint: false, openWorldHint: true },
    async ({ status, my_slug }) => {
      const slug = resolveSlug(my_slug);
      await brokerPost("/messages", {
        from: slug,
        content: `[STATUS] ${status}`,
        tagged: [],
      });
      return {
        content: [{
          type: "text",
          text: `Updated team status for @${slug}: ${status}`,
        }],
      };
    },
  );

  server.tool(
    "team_members",
    "List active participants in the shared team channel with their latest visible activity.",
    {},
    { readOnlyHint: true, openWorldHint: true },
    async () => {
      const result = await brokerGet("/members") as {
        members?: Array<{ slug: string; lastMessage?: string; lastTime?: string }>;
      };
      const members = result.members ?? [];
      if (members.length === 0) {
        return { content: [{ type: "text", text: "No active team members yet." }] };
      }
      const lines = members.map((member) => {
        const time = member.lastTime ? ` at ${member.lastTime}` : "";
        const detail = member.lastMessage ? ` — ${member.lastMessage}` : "";
        return `- @${member.slug}${time}${detail}`;
      });
      return {
        content: [{
          type: "text",
          text: `Active team members:\n${lines.join("\n")}`,
        }],
      };
    },
  );

  server.tool(
    "human_interview",
    "Ask the human a blocking interview question when the team cannot proceed responsibly without a decision. Presents options and optionally marks one as recommended. Pauses the team until answered.",
    {
      question: z.string().describe("The specific decision or clarification needed from the human"),
      context: z.string().optional().describe("Short context explaining why the team is asking now"),
      my_slug: z.string().optional().describe("Agent slug asking the question. Defaults to WUPHF_AGENT_SLUG."),
      options: z.array(z.object({
        id: z.string().describe("Stable short ID like 'sales' or 'smbs'"),
        label: z.string().describe("User-facing option label"),
        description: z.string().optional().describe("One-sentence explanation of tradeoff/impact"),
      })).optional().describe("Suggested answer options to show the human"),
      recommended_option_id: z.string().optional().describe("Which option you recommend, if any"),
    },
    { readOnlyHint: false, openWorldHint: true },
    async ({ question, context, my_slug, options, recommended_option_id }) => {
      const slug = resolveSlug(my_slug);
      const result = await brokerPost("/interview", {
        from: slug,
        question,
        context,
        options: options ?? [],
        recommended_id: recommended_option_id,
      }) as { id?: string };
      const interviewId = result.id;
      if (!interviewId) {
        throw new Error("Interview request did not return an ID.");
      }

      const timeoutAt = Date.now() + 30 * 60 * 1000;
      while (Date.now() < timeoutAt) {
        const answerResult = await brokerGet(`/interview/answer?id=${encodeURIComponent(interviewId)}`) as {
          answered?: InterviewAnswer | null;
        };
        if (answerResult.answered) {
          const answer = answerResult.answered;
          const finalText = answer.custom_text || answer.choice_text || "";
          return {
            content: [{
              type: "text",
              text: JSON.stringify({
                interview_id: interviewId,
                answered: true,
                choice_id: answer.choice_id ?? "",
                answer: finalText,
                answered_at: answer.answered_at ?? "",
              }, null, 2),
            }],
          };
        }
        await sleep(1500);
      }

      throw new Error("Timed out waiting for human interview answer.");
    },
  );
}
