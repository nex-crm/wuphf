import { useEffect, useMemo, useState } from "react";

import type { Message, OfficeMember } from "../../api/client";
import { extractRichArtifactIds } from "../../lib/richArtifactReferences";

/**
 * Phrases an agent uses to promise a follow-up artifact ("article", "chart", etc).
 * Lowercase, no anchors: matched against the trailing window of the message body.
 */
const ARTIFACT_PROMISE_PHRASES: readonly string[] = [
  "visual artifact below",
  "visual artifact coming",
  "chart below",
  "chart coming",
  "article coming",
  "full breakdown",
  "full article",
  "in the article",
  "below",
];

/** Window after the message for which the skeleton is shown (ms). */
export const ARTIFACT_SKELETON_RECENCY_WINDOW_MS = 60_000;

/**
 * The skeleton card itself. Pure presentational: render-time fixed, no
 * intrinsic timers. Triggering / unmount is the caller's job.
 *
 * Visual size mirrors a typical artifact card frame (~600px × ~120px).
 * Shimmer is compositor-only via `background-position` so it can't trigger
 * layout. `prefers-reduced-motion` is honoured by the CSS (animation: none).
 */
export function ArtifactSkeleton({
  label = "drafting visual…",
}: {
  label?: string;
} = {}) {
  return (
    <div
      className="artifact-skeleton"
      role="status"
      aria-live="polite"
      aria-label={label}
      data-testid="artifact-skeleton"
    >
      <div className="artifact-skeleton-head">
        <span className="artifact-skeleton-title artifact-skeleton-shimmer" />
        <span className="artifact-skeleton-label">{label}</span>
      </div>
      <div className="artifact-skeleton-body">
        <span className="artifact-skeleton-line artifact-skeleton-shimmer" />
        <span className="artifact-skeleton-line artifact-skeleton-line-short artifact-skeleton-shimmer" />
        <span className="artifact-skeleton-figure artifact-skeleton-shimmer" />
      </div>
    </div>
  );
}

/**
 * Inputs to the trigger heuristic. Kept as a plain object so the test suite
 * can drive it without mounting the whole component tree.
 */
export interface ShouldShowArtifactSkeletonInput {
  /** The candidate message (typically the last assistant message in the channel). */
  message: Pick<Message, "id" | "from" | "content" | "timestamp">;
  /**
   * Every message in the channel *after* `message.timestamp`. Used to check
   * "has a `visual-artifact:` marker landed yet". Order is irrelevant.
   */
  newerMessages: ReadonlyArray<Pick<Message, "from" | "content">>;
  /** Office members. Used to confirm the same agent is still active/typing. */
  members: ReadonlyArray<Pick<OfficeMember, "slug" | "status">>;
  /**
   * Current wall-clock time in ms. Injected so tests are deterministic and
   * the trigger can age out cleanly without a setInterval.
   */
  nowMs: number;
}

/**
 * Decide whether to render the skeleton under `message`.
 *
 * Trigger requires ALL of:
 *  1. message is from an agent currently in `status: "active"` (server's
 *     "agent X is typing" signal — same source as <TypingIndicator>).
 *  2. message body ends with one of {@link ARTIFACT_PROMISE_PHRASES}.
 *  3. message is less than {@link ARTIFACT_SKELETON_RECENCY_WINDOW_MS} old.
 *  4. message itself does not already carry a `visual-artifact:` marker
 *     (no skeleton when the artifact reference is already inline).
 *  5. no newer message from the same agent carries a `visual-artifact:`
 *     marker yet (skeleton unmounts cleanly when the artifact card lands).
 */
export function shouldShowArtifactSkeleton(
  input: ShouldShowArtifactSkeletonInput,
): boolean {
  const { message, newerMessages, members, nowMs } = input;
  const content = message.content ?? "";

  if (!isAgentAuthor(message.from)) return false;
  if (!isMemberActive(members, message.from)) return false;
  if (!endsWithArtifactPromise(content)) return false;
  if (!isWithinSkeletonWindow(message.timestamp, nowMs)) return false;
  // Message already has the artifact inline — no skeleton needed.
  if (extractRichArtifactIds(content).length > 0) return false;
  // The artifact card has already landed — unmount.
  if (hasArtifactArrivedFromSameAgent(newerMessages, message.from)) {
    return false;
  }
  return true;
}

function isAgentAuthor(from: string): boolean {
  if (!from) return false;
  if (from === "human" || from === "you") return false;
  if (from.startsWith("human:")) return false;
  return true;
}

function isMemberActive(
  members: ReadonlyArray<Pick<OfficeMember, "slug" | "status">>,
  slug: string,
): boolean {
  const member = members.find((m) => m.slug === slug);
  return member?.status === "active";
}

function isWithinSkeletonWindow(
  timestamp: string | undefined,
  nowMs: number,
): boolean {
  if (!timestamp) return false;
  const messageMs = Date.parse(timestamp);
  if (!Number.isFinite(messageMs)) return false;
  const delta = nowMs - messageMs;
  if (delta < 0) return false;
  return delta < ARTIFACT_SKELETON_RECENCY_WINDOW_MS;
}

function hasArtifactArrivedFromSameAgent(
  newerMessages: ReadonlyArray<Pick<Message, "from" | "content">>,
  from: string,
): boolean {
  for (const newer of newerMessages) {
    if (newer.from !== from) continue;
    if (extractRichArtifactIds(newer.content ?? "").length > 0) return true;
  }
  return false;
}

/**
 * Decide whether to render the artifact skeleton under a given message.
 * Subscribes to a coarse 5s ticker only while the skeleton might still be
 * live so the 60s recency window expires cleanly without depending on an
 * upstream refetch or re-render. When `enabled` is false the ticker is
 * skipped entirely, so non-candidate bubbles stay zero-overhead.
 */
export function useArtifactSkeletonTrigger({
  enabled,
  message,
  channelMessages,
  members,
}: {
  enabled: boolean;
  message: Message;
  channelMessages: ReadonlyArray<Message>;
  members: ReadonlyArray<Pick<OfficeMember, "slug" | "status">>;
}): boolean {
  const [nowMs, setNowMs] = useState<number>(() => Date.now());

  const newerMessages = useMemo(() => {
    if (!(enabled && message.timestamp)) return [];
    const baseMs = Date.parse(message.timestamp);
    if (!Number.isFinite(baseMs)) return [];
    return channelMessages.filter((m) => {
      if (m.id === message.id) return false;
      if (!m.timestamp) return false;
      const ms = Date.parse(m.timestamp);
      if (!Number.isFinite(ms)) return false;
      return ms > baseMs;
    });
  }, [enabled, message.id, message.timestamp, channelMessages]);

  const baseMs = message.timestamp ? Date.parse(message.timestamp) : NaN;
  const withinWindow =
    Number.isFinite(baseMs) &&
    nowMs - baseMs < ARTIFACT_SKELETON_RECENCY_WINDOW_MS;

  useEffect(() => {
    if (!(enabled && withinWindow)) return;
    const id = setInterval(() => setNowMs(Date.now()), 5_000);
    return () => clearInterval(id);
  }, [enabled, withinWindow]);

  if (!enabled) return false;
  return shouldShowArtifactSkeleton({
    message,
    newerMessages,
    members,
    nowMs,
  });
}

function endsWithArtifactPromise(content: string): boolean {
  const trimmed = content.trim();
  if (!trimmed) return false;
  // Strip trailing markdown punctuation so phrases at the very end ("...below.")
  // still match. Keep alphanumerics, spaces, hyphens — strip everything else.
  const tail = trimmed
    .slice(-160)
    .toLowerCase()
    .replace(/[^a-z0-9 -]/g, " ")
    .replace(/\s+/g, " ")
    .trim();
  if (!tail) return false;
  for (const phrase of ARTIFACT_PROMISE_PHRASES) {
    if (tail.endsWith(phrase)) return true;
  }
  return false;
}
