import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import { NexApiClient } from "../client.js";

// ─── Local Broker Client ───
// Talks to a shared localhost broker so all agent MCP instances share one channel.
// Falls back to in-memory if broker is unavailable.

const BROKER_PORT = 7890;
const BROKER_URL = `http://127.0.0.1:${BROKER_PORT}`;
const BROKER_TOKEN = process.env.NEX_BROKER_TOKEN ?? "";

interface ChannelMessage {
  id: string;
  from: string;
  content: string;
  tagged: string[];
  timestamp: string;
}

// Fallback in-memory store (used when broker is down)
const localMessages: ChannelMessage[] = [];
let localCounter = 0;
let brokerAvailable: boolean | null = null;

async function checkBroker(): Promise<boolean> {
  if (brokerAvailable !== null) return brokerAvailable;
  try {
    const resp = await fetch(`${BROKER_URL}/health`, {
      signal: AbortSignal.timeout(500),
    });
    brokerAvailable = resp.ok;
  } catch {
    brokerAvailable = false;
  }
  return brokerAvailable;
}

function authHeaders(): Record<string, string> {
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (BROKER_TOKEN) {
    headers["Authorization"] = `Bearer ${BROKER_TOKEN}`;
  }
  return headers;
}

async function brokerPost(
  path: string,
  body: unknown,
): Promise<unknown | null> {
  try {
    const resp = await fetch(`${BROKER_URL}${path}`, {
      method: "POST",
      headers: authHeaders(),
      body: JSON.stringify(body),
      signal: AbortSignal.timeout(2000),
    });
    if (!resp.ok) return null;
    return await resp.json();
  } catch {
    return null;
  }
}

async function brokerGet(path: string): Promise<unknown | null> {
  try {
    const headers: Record<string, string> = {};
    if (BROKER_TOKEN) {
      headers["Authorization"] = `Bearer ${BROKER_TOKEN}`;
    }
    const resp = await fetch(`${BROKER_URL}${path}`, {
      headers,
      signal: AbortSignal.timeout(2000),
    });
    if (!resp.ok) return null;
    return await resp.json();
  } catch {
    return null;
  }
}

function extractTags(content: string): string[] {
  const matches = content.match(/@([a-z][a-z0-9-]*)/g);
  if (!matches) return [];
  return [...new Set(matches.map((m) => m.slice(1)))];
}

export function registerTeamTools(server: McpServer, client: NexApiClient) {
  // ─── Broadcast: Post a message to the team channel ───
  server.tool(
    "team_broadcast",
    "Post a message to the team channel. All teammates will see this message. " +
      "Use @slug to tag specific teammates (e.g., '@fe can you handle this?'). " +
      "Tagged agents are expected to respond. Use this for announcements, " +
      "delegation, questions, progress updates, and team discussion. " +
      "NOTE: This is for conversation only. To record a DECISION or important " +
      "fact permanently, use add_context instead.",
    {
      message: z.string().describe("The message to post to the team channel"),
      from: z
        .string()
        .describe("Your agent slug (e.g., 'ceo', 'fe', 'pm')"),
    },
    { readOnlyHint: false },
    async ({ message, from }) => {
      const tagged = extractTags(message);

      if (await checkBroker()) {
        // Use shared broker
        const result = (await brokerPost("/messages", {
          from,
          content: message,
          tagged,
        })) as { id?: string; total?: number } | null;
        if (result) {
          const taggedStr =
            tagged.length > 0
              ? ` (tagged: ${tagged.map((t) => `@${t}`).join(", ")})`
              : "";
          return {
            content: [
              {
                type: "text" as const,
                text: `Posted to channel${taggedStr}. ${result.total ?? "?"} total messages.`,
              },
            ],
          };
        }
      }

      // Fallback: in-memory
      localCounter++;
      const msg: ChannelMessage = {
        id: `msg-${localCounter}`,
        from,
        content: message,
        tagged,
        timestamp: new Date().toISOString(),
      };
      localMessages.push(msg);

      const taggedStr =
        tagged.length > 0
          ? ` (tagged: ${tagged.map((t) => `@${t}`).join(", ")})`
          : "";
      return {
        content: [
          {
            type: "text" as const,
            text: `Posted to channel (local)${taggedStr}. ${localMessages.length} messages. Note: broker not running — messages only visible to you. Start broker with: wuphf team-broker`,
          },
        ],
      };
    },
  );

  // ─── Poll: Read recent messages from the team channel ───
  server.tool(
    "team_poll",
    "Read recent messages from the team channel. Call this regularly to see " +
      "what teammates are saying. Returns messages in chronological order. " +
      "You should poll when: (1) you just joined, (2) periodically during work, " +
      "(3) before making decisions, (4) when you think others might have responded.",
    {
      limit: z
        .number()
        .optional()
        .describe("Max messages to return (default: 20, max: 100)"),
      since_id: z
        .string()
        .optional()
        .describe("Only return messages after this ID (for incremental polling)"),
      my_slug: z
        .string()
        .optional()
        .describe("Your agent slug — highlights messages that @tag you"),
    },
    { readOnlyHint: true },
    async ({ limit, since_id, my_slug }) => {
      const max = Math.min(limit ?? 20, 100);

      if (await checkBroker()) {
        const params = new URLSearchParams();
        params.set("limit", String(max));
        if (since_id) params.set("since_id", since_id);
        if (my_slug) params.set("my_slug", my_slug);
        const result = (await brokerGet(
          `/messages?${params}`,
        )) as { messages?: ChannelMessage[]; tagged_count?: number } | null;
        if (result?.messages) {
          return { content: [{ type: "text" as const, text: formatMessages(result.messages, my_slug, result.tagged_count) }] };
        }
      }

      // Fallback: in-memory
      let messages = localMessages;
      if (since_id) {
        const idx = messages.findIndex((m) => m.id === since_id);
        if (idx >= 0) messages = messages.slice(idx + 1);
      }
      const recent = messages.slice(-max);
      const taggedCount = my_slug
        ? recent.filter((m) => m.tagged.includes(my_slug)).length
        : 0;
      return { content: [{ type: "text" as const, text: formatMessages(recent, my_slug, taggedCount) }] };
    },
  );

  // ─── Status: Set your current activity ───
  server.tool(
    "team_status",
    "Set your current status so teammates know what you're working on.",
    {
      from: z.string().describe("Your agent slug"),
      status: z.string().describe("What you're currently doing"),
    },
    { readOnlyHint: false },
    async ({ from, status }) => {
      const message = `[STATUS] ${status}`;
      if (await checkBroker()) {
        await brokerPost("/messages", { from, content: message, tagged: [] });
      } else {
        localCounter++;
        localMessages.push({
          id: `msg-${localCounter}`,
          from,
          content: message,
          tagged: [],
          timestamp: new Date().toISOString(),
        });
      }
      return { content: [{ type: "text" as const, text: `Status updated: ${status}` }] };
    },
  );

  // ─── Members: List active team members ───
  server.tool(
    "team_members",
    "List all team members and their most recent channel activity.",
    {},
    { readOnlyHint: true },
    async () => {
      if (await checkBroker()) {
        const result = (await brokerGet("/members")) as {
          members?: Array<{
            slug: string;
            lastMessage: string;
            lastTime: string;
          }>;
        } | null;
        if (result?.members) {
          if (result.members.length === 0) {
            return {
              content: [{ type: "text" as const, text: "No team activity yet." }],
            };
          }
          const lines = result.members.map(
            (m) => `@${m.slug} — last active ${m.lastTime}: "${m.lastMessage}"`,
          );
          return {
            content: [
              {
                type: "text" as const,
                text: `Team members (${result.members.length} active):\n${lines.join("\n")}`,
              },
            ],
          };
        }
      }

      // Fallback: in-memory
      const members = new Map<string, { lastMessage: string; lastTime: string }>();
      for (const msg of localMessages) {
        members.set(msg.from, {
          lastMessage: msg.content.slice(0, 80),
          lastTime: msg.timestamp.slice(11, 19),
        });
      }
      if (members.size === 0) {
        return { content: [{ type: "text" as const, text: "No team activity yet." }] };
      }
      const lines: string[] = [];
      for (const [slug, activity] of members) {
        lines.push(`@${slug} — last active ${activity.lastTime}: "${activity.lastMessage}"`);
      }
      return {
        content: [
          { type: "text" as const, text: `Team members (${members.size} active):\n${lines.join("\n")}` },
        ],
      };
    },
  );
}

/**
 * startChannelPush polls the broker for new messages and pushes them
 * to the Claude session via notifications/claude/channel.
 * This makes messages appear in the conversation without the user typing.
 */
export function startChannelPush(server: McpServer, agentSlug?: string) {
  let lastSeenId = "";
  const POLL_INTERVAL = 1500; // 1.5 seconds

  const poll = async () => {
    if (!(await checkBroker())) return;

    const params = new URLSearchParams();
    params.set("limit", "10");
    if (lastSeenId) params.set("since_id", lastSeenId);
    if (agentSlug) params.set("my_slug", agentSlug);

    const result = (await brokerGet(`/messages?${params}`)) as {
      messages?: ChannelMessage[];
    } | null;

    if (!result?.messages?.length) return;

    // Update cursor
    lastSeenId = result.messages[result.messages.length - 1].id;

    // Push each new message as a channel notification
    for (const msg of result.messages) {
      // Don't push your own messages back to yourself
      if (agentSlug && msg.from === agentSlug) continue;

      const taggedStr =
        agentSlug && msg.tagged.includes(agentSlug)
          ? " (YOU ARE TAGGED — please respond)"
          : "";

      try {
        await server.server.notification({
          method: "notifications/claude/channel",
          params: {
            content: `@${msg.from}: ${msg.content}${taggedStr}`,
            meta: {
              from_id: msg.from,
              sent_at: msg.timestamp,
              channel: "wuphf-team",
            },
          },
        });
      } catch {
        // notification method may not be supported — fall through silently
      }
    }
  };

  // Start polling loop
  setInterval(poll, POLL_INTERVAL);
  // Initial poll after short delay
  setTimeout(poll, 500);
}

function formatMessages(
  messages: ChannelMessage[],
  mySlug?: string | null,
  taggedCount?: number | null,
): string {
  if (messages.length === 0) {
    return "No new messages in the team channel.";
  }

  const lines = messages.map((m) => {
    const tagged =
      mySlug && m.tagged.includes(mySlug) ? " ← YOU ARE TAGGED" : "";
    return `[${m.timestamp.slice(11, 19)}] @${m.from}: ${m.content}${tagged}`;
  });

  const header =
    taggedCount && taggedCount > 0
      ? `${messages.length} messages (${taggedCount} tagging you):`
      : `${messages.length} messages:`;

  return `${header}\n\n${lines.join("\n")}`;
}
