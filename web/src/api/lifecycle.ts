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
import type { DecisionPacket, InboxPayload } from "../lib/types/lifecycle";
import { get, post } from "./client";

export type DecisionAction = "merge" | "request_changes" | "defer";

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
  // The Go broker can serialize an empty slice as `null`, which would
  // make DecisionInbox.filterRows throw before it reaches the
  // empty-state branch. Coerce to [] here so the rest of the UI can
  // treat `rows` as always-array, mirroring normalizeDecisionPacket.
  const raw = await get<InboxPayload>("/tasks/inbox");
  return {
    ...raw,
    rows: raw.rows ?? [],
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
): Promise<{ taskId: string; action: string; status: string }> {
  if (USE_MOCKS) {
    return Promise.resolve({ taskId, action, status: "recorded-mock" });
  }
  return post(`/tasks/${encodeURIComponent(taskId)}/decision`, { action });
}
