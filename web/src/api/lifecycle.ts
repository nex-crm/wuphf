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

export type DecisionAction =
  | "merge"
  | "request_changes"
  | "defer";

const USE_MOCKS = true;

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
  return get<DecisionPacket>(`/tasks/${encodeURIComponent(taskId)}`);
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
