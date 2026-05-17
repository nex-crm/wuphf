/**
 * API client for the Decision Inbox + Decision Packet endpoints.
 *
 * Lanes A/C ship the real `/api/tasks/inbox` and `/api/tasks/:id`
 * endpoints. Until those merge, these helpers fall back to the local
 * mock fixtures so Lane G can render every state and ship screenshots
 * without a live broker.
 *
 * TODO(post-lane-a): swap `USE_MOCKS` to `false` once the broker
 * exposes the real endpoints. The shape on the wire is identical.
 */

import {
  getDecisionPacketMock,
  getInboxPayloadMock,
} from "../lib/mocks/decisionPackets";
import type { InboxItem, InboxItemKind } from "../lib/types/inbox";
import type {
  InboxThreadDetail,
  InboxThreadsResponse,
} from "../lib/types/inboxThread";
import type {
  DecisionPacket,
  InboxCounts,
  InboxFilter,
  InboxPayload,
} from "../lib/types/lifecycle";
import { get, post } from "./client";

export type DecisionAction = "approve" | "request_changes" | "defer";

const USE_MOCKS = false;

/**
 * Fetch the indexed-lookup payload for the Decision Inbox. Returns
 * `InboxPayload` with all rows + per-bucket counts the sidebar needs.
 *
 * Auth: 401 propagates as a thrown Error; the route surfaces this as
 * the error-state banner. Tasks the human session has no read access
 * to are filtered out broker-side (see auth matrix in design doc).
 */
export async function getInboxPayload(): Promise<InboxPayload> {
  if (USE_MOCKS) {
    return getInboxPayloadMock();
  }
  return get<InboxPayload>("/tasks/inbox");
}

/**
 * Phase 2: fan-out inbox merging tasks + requests + reviews. The
 * `filter` narrows the task half by lifecycle bucket (same set as
 * /tasks/inbox); the optional `kind` trims the result to one
 * artifact kind for per-kind frontend tabs.
 *
 * Wire shape:
 *   { items: InboxItem[]; counts: InboxCounts; refreshedAt: string }
 *
 * Items are sorted most-recent-activity first. Counts remain
 * broker-wide (auth filter applies to items only).
 */
export interface UnifiedInboxResponse {
  items: InboxItem[];
  counts: InboxCounts;
  refreshedAt: string;
}

export async function getInboxItems(
  filter: InboxFilter | "all" = "all",
  kind?: InboxItemKind,
): Promise<UnifiedInboxResponse> {
  const params = new URLSearchParams({ filter });
  if (kind) {
    params.set("kind", kind);
  }
  const raw = await get<UnifiedInboxResponse>(
    `/inbox/items?${params.toString()}`,
  );
  return {
    items: (raw.items ?? []).map(normalizeInboxItem),
    counts: raw.counts,
    refreshedAt: raw.refreshedAt,
  };
}

/**
 * The Go broker serializes the task row with `lifecycleState` +
 * `severitySummary` (the InboxRow struct's JSON tags). The TS
 * InboxRow type uses `state` + `severityCounts`. This is a
 * pre-existing wire/TS skew that predates Phase 2. We normalize
 * at the API boundary so every component below this sees the
 * shape its types promise.
 */
function normalizeInboxItem(item: InboxItem): InboxItem {
  if (item.kind !== "task") return item;
  const row = item.task as unknown as Record<string, unknown>;
  if (!row) return item;
  const lifecycleState = row.lifecycleState ?? row.state;
  const severitySummary = row.severitySummary as
    | Record<string, number>
    | undefined;
  const severityCounts =
    row.severityCounts ??
    (severitySummary
      ? {
          critical: severitySummary.critical ?? 0,
          major: severitySummary.major ?? 0,
          minor: severitySummary.minor ?? 0,
          nitpick: severitySummary.nitpick ?? 0,
          skipped: severitySummary.skipped ?? 0,
        }
      : undefined);
  const normalizedRow = {
    ...item.task,
    state: lifecycleState ?? item.task.state,
    severityCounts: severityCounts ?? item.task.severityCounts,
  } as InboxItem extends { kind: "task"; task: infer T } ? T : never;
  return { ...item, task: normalizedRow };
}

/**
 * Phase 3: per-agent thread inbox. Groups InboxItems by the agent
 * who owns/sent/submitted them and enriches each thread with recent
 * message activity from the agent's DM channel.
 *
 * Composes on top of /inbox/items — items are the same shape, just
 * grouped + decorated with the agent + preview line + DM channel.
 */
export async function getInboxThreads(): Promise<InboxThreadsResponse> {
  const raw = await get<InboxThreadsResponse>("/inbox/threads");
  return {
    threads: raw.threads ?? [],
    counts: raw.counts,
    refreshedAt: raw.refreshedAt,
  };
}

/**
 * Fetch one thread's interleaved event stream (messages + action
 * cards in chronological order). Frontend opens this when the user
 * clicks a thread row.
 */
export async function getInboxThreadDetail(
  agentSlug: string,
): Promise<InboxThreadDetail> {
  const raw = await get<InboxThreadDetail>(
    `/inbox/threads/${encodeURIComponent(agentSlug)}`,
  );
  return {
    thread: raw.thread,
    events: raw.events ?? [],
  };
}

/**
 * Fetch the full Decision Packet for one task ID. Returns `403` when
 * the human session is not in the task's reviewer list and not the
 * broker/owner token.
 */
export async function getDecisionPacket(
  taskId: string,
): Promise<DecisionPacket> {
  if (USE_MOCKS) {
    return getDecisionPacketMock(taskId);
  }
  const raw = await get<DecisionPacket>(`/tasks/${encodeURIComponent(taskId)}`);
  return normalizeDecisionPacket(raw);
}

/**
 * Defensive normalization: the Go broker serializes empty Go slices as
 * `null`, not `[]`. TS components iterate these as if they were always
 * arrays. Normalize at the API boundary so every component below this
 * sees the contract their types promise.
 */
function normalizeDecisionPacket(p: DecisionPacket): DecisionPacket {
  return {
    ...p,
    banners: p.banners ?? [],
    changedFiles: p.changedFiles ?? [],
    reviewerGrades: p.reviewerGrades ?? [],
    reviewers: p.reviewers ?? [],
    subIssues: p.subIssues ?? [],
    spec: {
      ...p.spec,
      acceptanceCriteria: p.spec?.acceptanceCriteria ?? [],
      constraints: p.spec?.constraints ?? [],
      feedback: p.spec?.feedback ?? [],
    },
    sessionReport: {
      ...p.sessionReport,
      topWins: p.sessionReport?.topWins ?? [],
      deadEnds: p.sessionReport?.deadEnds ?? [],
    },
    dependencies: {
      ...p.dependencies,
      blockedOn: p.dependencies?.blockedOn ?? [],
    },
  };
}

/**
 * Mark the inbox as "seen up to now" for the current human session.
 * Posts the wall-clock time so the broker stores it on
 * Broker.userInboxCursor[slug]. The frontend calls this after each
 * decision action so the next inbox refresh can recompute the badge
 * count against the cursor.
 *
 * Fire-and-forget — the call returns void, errors are swallowed so a
 * cursor-write failure can't block the user's decision flow.
 */
export async function postInboxCursor(): Promise<void> {
  try {
    await post("/inbox/cursor", {
      lastSeenAt: new Date().toISOString(),
    });
  } catch {
    // intentional: cursor writes are best-effort.
  }
}

/**
 * POST a human decision (merge / request_changes / defer) for one task.
 * The broker transitions the task lifecycle and persists the decision
 * onto the Decision Packet. Returns the broker's confirmation envelope.
 *
 * In mock mode this is a no-op resolved promise so the buttons remain
 * clickable in the screenshot harness without spinning up a broker.
 */
export async function postDecision(
  taskId: string,
  action: DecisionAction,
  comment?: string,
): Promise<{ taskId: string; action: string; status: string }> {
  if (USE_MOCKS) {
    return Promise.resolve({ taskId, action, status: "recorded-mock" });
  }
  const body: { action: DecisionAction; comment?: string } = { action };
  const trimmed = (comment ?? "").trim();
  if (trimmed) body.comment = trimmed;
  return post(`/tasks/${encodeURIComponent(taskId)}/decision`, body);
}

/**
 * POST a PR-style comment on a task without making a decision. The
 * broker appends the body as a FeedbackItem on the task's Decision
 * Packet so it shows up in the unified Inbox discussion thread for
 * other humans and agents to react to. State does not change.
 *
 * In mock mode this is a no-op resolved promise so the button remains
 * clickable in the screenshot harness without spinning up a broker.
 */
export async function postTaskComment(
  taskId: string,
  _channel: string,
  body: string,
): Promise<{ taskId: string; status: string; author: string }> {
  const trimmed = body.trim();
  if (!trimmed) {
    throw new Error("comment body required");
  }
  if (USE_MOCKS) {
    return Promise.resolve({
      taskId,
      status: "comment-mock",
      author: "human",
    });
  }
  return post(`/tasks/${encodeURIComponent(taskId)}/comment`, {
    body: trimmed,
  });
}

/**
 * POST a terminal Reject on a task. Distinct from `block` (recoverable
 * waiting on upstream) and from `request_changes` (revise + resubmit).
 * After reject, downstream tasks that depend on this task STAY blocked
 * permanently because the work did not land.
 *
 * Reject runs through the task mutation endpoint (POST /tasks with
 * action=reject) rather than /tasks/{id}/decision so the lifecycle
 * transition uses the same code path as the agent-side reject path.
 */
export async function postTaskReject(
  taskId: string,
  comment: string,
): Promise<{ taskId: string; action: string; status: string }> {
  const trimmed = comment.trim();
  if (USE_MOCKS) {
    return Promise.resolve({ taskId, action: "reject", status: "recorded-mock" });
  }
  return post(`/tasks`, {
    action: "reject",
    id: taskId,
    channel: "general",
    details: trimmed,
    created_by: "human",
  });
}
