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
import { get } from "./client";

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
