/**
 * Phase 3 unified-inbox thread types. Mirrors the Go shapes in
 * internal/team/broker_inbox_threads.go.
 *
 * A thread groups every attention item from one AI agent into a
 * single conversation surface. The detail view interleaves agent
 * messages with inline action cards (one card per pending item) in
 * chronological order — the same pattern as a Slack DM or Gmail
 * conversation that has approval requests embedded.
 */

import type { InboxItem } from "./inbox";
import type { InboxCounts } from "./lifecycle";

export interface InboxThread {
  key: string; // "agent:<slug>"
  agentSlug: string;
  agentName: string;
  agentRole?: string;
  dmChannel?: string;
  lastActivityAt: string; // RFC3339
  preview: string;
  pendingCount: number;
  items: InboxItem[];
}

export type InboxThreadEventKind = "message" | "item";

/**
 * One event in the chat-style detail stream. Discriminated on `kind`:
 * `message` renders as a message bubble, `item` renders as an inline
 * approval card. Adding a third kind without extending the renderer
 * switch fails compile via never-default.
 */
export type InboxThreadEvent =
  | {
      kind: "message";
      timestamp: string;
      message: {
        id: string;
        from: string;
        channel?: string;
        content: string;
        timestamp: string;
        kind?: string;
        tagged?: string[];
        replyTo?: string;
      };
    }
  | {
      kind: "item";
      timestamp: string;
      item: InboxItem;
    };

export interface InboxThreadDetail {
  thread: InboxThread;
  events: InboxThreadEvent[];
}

export interface InboxThreadsResponse {
  threads: InboxThread[];
  counts: InboxCounts;
  refreshedAt: string;
}

/**
 * Stable per-event key for React lists. Switches on kind; the
 * default branch's `_exhaustive: never` assignment fails compile
 * when a new kind is added without a render path.
 */
export function inboxThreadEventKey(event: InboxThreadEvent): string {
  if (event.kind === "message") return `message:${event.message.id}`;
  const it = event.item;
  switch (it.kind) {
    case "task":
      return `item:task:${it.taskId}`;
    case "request":
      return `item:request:${it.requestId}`;
    case "review":
      return `item:review:${it.reviewId}`;
    default: {
      const _exhaustive: never = it;
      return _exhaustive;
    }
  }
}
