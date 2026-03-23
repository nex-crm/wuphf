import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import { NexApiClient } from "../client.js";

// In-memory channel store for the current session.
// Messages also get persisted to Nex knowledge graph via add_context.
interface ChannelMessage {
  id: string;
  from: string;
  content: string;
  tagged: string[];
  timestamp: string;
}

const channelMessages: ChannelMessage[] = [];
let messageCounter = 0;

function extractTags(content: string): string[] {
  const matches = content.match(/@([a-z][a-z0-9-]*)/g);
  if (!matches) return [];
  return matches.map((m) => m.slice(1));
}

export function registerTeamTools(server: McpServer, client: NexApiClient) {
  // ─── Broadcast: Post a message to the team channel ───
  server.tool(
    "team_broadcast",
    "Post a message to the team channel. All teammates will see this message. " +
      "Use @slug to tag specific teammates (e.g., '@fe can you handle this?'). " +
      "Tagged agents are expected to respond. Use this for announcements, " +
      "delegation, questions, progress updates, and team discussion.",
    {
      message: z.string().describe("The message to post to the team channel"),
      from: z
        .string()
        .describe(
          "Your agent slug (e.g., 'ceo', 'fe', 'pm'). This identifies who is speaking.",
        ),
    },
    { readOnlyHint: false },
    async ({ message, from }) => {
      messageCounter++;
      const msg: ChannelMessage = {
        id: `msg-${messageCounter}`,
        from,
        content: message,
        tagged: extractTags(message),
        timestamp: new Date().toISOString(),
      };
      channelMessages.push(msg);

      // Persist to Nex knowledge graph for cross-session recall
      try {
        await client.post("/v1/context/text", {
          content: `[Team Channel] @${from}: ${message}`,
          context: "Team channel message from multi-agent collaboration session",
        });
      } catch {
        // Non-fatal: channel still works even if persistence fails
      }

      const taggedStr =
        msg.tagged.length > 0
          ? ` (tagged: ${msg.tagged.map((t) => `@${t}`).join(", ")})`
          : "";

      return {
        content: [
          {
            type: "text",
            text: `Message posted to team channel${taggedStr}. ${channelMessages.length} total messages in channel.`,
          },
        ],
      };
    },
  );

  // ─── Poll: Read recent messages from the team channel ───
  server.tool(
    "team_poll",
    "Read recent messages from the team channel. Call this to see what your " +
      "teammates have been saying. Returns messages in chronological order. " +
      "You should poll regularly to stay aware of team activity and respond " +
      "when relevant or when you are @tagged.",
    {
      limit: z
        .number()
        .optional()
        .describe("Max messages to return (default: 20, max: 100)"),
      since_id: z
        .string()
        .optional()
        .describe(
          "Only return messages after this ID (for incremental polling)",
        ),
      my_slug: z
        .string()
        .optional()
        .describe("Your agent slug — highlights messages that @tag you"),
    },
    { readOnlyHint: true },
    async ({ limit, since_id, my_slug }) => {
      const max = Math.min(limit ?? 20, 100);

      let messages = channelMessages;
      if (since_id) {
        const idx = messages.findIndex((m) => m.id === since_id);
        if (idx >= 0) {
          messages = messages.slice(idx + 1);
        }
      }

      const recent = messages.slice(-max);

      if (recent.length === 0) {
        return {
          content: [
            {
              type: "text",
              text: "No new messages in the team channel.",
            },
          ],
        };
      }

      const lines = recent.map((m) => {
        const tagged =
          my_slug && m.tagged.includes(my_slug) ? " ← YOU ARE TAGGED" : "";
        return `[${m.timestamp.slice(11, 19)}] @${m.from}: ${m.content}${tagged}`;
      });

      const taggedCount = my_slug
        ? recent.filter((m) => m.tagged.includes(my_slug)).length
        : 0;
      const header =
        taggedCount > 0
          ? `${recent.length} messages (${taggedCount} tagging you):`
          : `${recent.length} messages:`;

      return {
        content: [
          {
            type: "text",
            text: `${header}\n\n${lines.join("\n")}`,
          },
        ],
      };
    },
  );

  // ─── Status: Set your current status/activity ───
  server.tool(
    "team_status",
    "Set your current status so teammates know what you're working on. " +
      "This is visible when others poll the channel.",
    {
      from: z.string().describe("Your agent slug"),
      status: z
        .string()
        .describe(
          "What you're currently doing (e.g., 'building hero component', 'reviewing PR #42')",
        ),
    },
    { readOnlyHint: false },
    async ({ from, status }) => {
      // Post as a status update message
      messageCounter++;
      const msg: ChannelMessage = {
        id: `msg-${messageCounter}`,
        from,
        content: `[STATUS] ${status}`,
        tagged: [],
        timestamp: new Date().toISOString(),
      };
      channelMessages.push(msg);

      return {
        content: [
          {
            type: "text",
            text: `Status updated: ${status}`,
          },
        ],
      };
    },
  );

  // ─── Members: List team members and their recent activity ───
  server.tool(
    "team_members",
    "List all team members and their most recent channel activity.",
    {},
    { readOnlyHint: true },
    async () => {
      // Build member activity from channel messages
      const members = new Map<
        string,
        { lastMessage: string; lastTime: string }
      >();

      for (const msg of channelMessages) {
        members.set(msg.from, {
          lastMessage: msg.content.slice(0, 80),
          lastTime: msg.timestamp.slice(11, 19),
        });
      }

      if (members.size === 0) {
        return {
          content: [
            {
              type: "text",
              text: "No team activity yet. Use team_broadcast to post the first message.",
            },
          ],
        };
      }

      const lines: string[] = [];
      for (const [slug, activity] of members) {
        lines.push(
          `@${slug} — last active ${activity.lastTime}: "${activity.lastMessage}"`,
        );
      }

      return {
        content: [
          {
            type: "text",
            text: `Team members (${members.size} active):\n${lines.join("\n")}`,
          },
        ],
      };
    },
  );
}
