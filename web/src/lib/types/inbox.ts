/**
 * Phase 2 unified-inbox TS types.
 *
 * Mirrors the Go shapes in internal/team/broker_inbox_phase2.go:
 * InboxItemKind is a closed string set; InboxItem is a discriminated
 * union with the `kind` tag carrying the discriminator. The
 * never-default branch in renderInboxItemKey enforces exhaustiveness
 * at compile time — adding a new kind without updating the switch
 * fails type-checking.
 *
 * Phase 2 ships task / request / review. Adding a fourth kind in a
 * future phase requires touching this file, the renderer switch in
 * the inbox list component, and the per-kind auth helper on the Go
 * side. Compile-time exhaustiveness keeps any one of those three
 * from drifting.
 */

import type { InboxRow } from "./lifecycle";

export type InboxItemKind = "task" | "request" | "review";

export interface InboxItemTask {
  kind: "task";
  taskId: string;
  title: string;
  channel?: string;
  createdAt?: string;
  elapsedMs?: number;
  /** Agent who owns / sent / submitted this item. Phase 3+. */
  agentSlug?: string;
  task: InboxRow;
}

export interface InboxItemRequest {
  kind: "request";
  requestId: string;
  title: string;
  channel?: string;
  createdAt?: string;
  elapsedMs?: number;
  agentSlug?: string;
  request: {
    kind: string;
    question: string;
    from: string;
    blocking?: boolean;
  };
}

export interface InboxItemReview {
  kind: "review";
  reviewId: string;
  title: string;
  channel?: string;
  createdAt?: string;
  elapsedMs?: number;
  agentSlug?: string;
  review: {
    state: string;
    reviewerSlug: string;
    sourceSlug: string;
    targetPath: string;
  };
}

export type InboxItem = InboxItemTask | InboxItemRequest | InboxItemReview;

/**
 * Stable React key + cache key for a row. Switches on `kind` and
 * derives the per-kind ID. Adding a new InboxItemKind without
 * extending this switch fails compilation — the default-branch
 * `_exhaustive: never` assignment catches the missing case.
 */
export function renderInboxItemKey(item: InboxItem): string {
  switch (item.kind) {
    case "task":
      return `task:${item.taskId}`;
    case "request":
      return `request:${item.requestId}`;
    case "review":
      return `review:${item.reviewId}`;
    default: {
      const _exhaustive: never = item;
      return _exhaustive;
    }
  }
}
