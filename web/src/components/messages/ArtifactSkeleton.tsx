import { useEffect, useMemo, useState } from "react";

import type { Message, OfficeMember } from "../../api/client";
import { extractRichArtifactIds } from "../../lib/richArtifactReferences";

/**
 * Trailing promise nouns an agent emits right before it posts a follow-up
 * visual artifact ("...full breakdown below", "...see the diagram", etc).
 *
 * The heuristic matches when the message ends with a SHORT clause built from
 * these nouns plus a small set of promise verbs/prepositions. We match the
 * tail only — never mid-body — so normal sentences that merely mention a
 * "chart" earlier on don't false-positive. On the codex path the agent may
 * not emit one of a fixed set of phrases, so we match the shape of a promise
 * (a deictic like "below"/"see the" or an artifact noun at the very end)
 * rather than an exact string.
 */
const ARTIFACT_PROMISE_NOUNS: readonly string[] = [
  "visual artifact",
  "visual",
  "artifact",
  "chart",
  "diagram",
  "figure",
  "graphic",
  "schematic",
  "breakdown",
  "full breakdown",
  "full article",
  "article",
  "writeup",
  "write up",
  "rendering",
];

/**
 * Trailing deictic clauses that promise something is being rendered just
 * below the gist. Strong enough to fire on their own ("...rendered below")
 * because they explicitly point at a follow-up render.
 */
const ARTIFACT_PROMISE_DEICTICS: readonly string[] = [
  "below",
  "rendered below",
  "rendered",
  "see the",
  "see below",
  "attached below",
];

/**
 * Trailing "still being produced" verbs. Too weak to fire alone ("the
 * weekend is coming"), so they only count when an artifact noun appears
 * earlier in the same clause ("the chart is coming", "diagram incoming").
 */
const ARTIFACT_PROMISE_PENDING_VERBS: readonly string[] = [
  "coming",
  "incoming",
  "on its way",
  "one moment",
  "loading",
];

/**
 * Lead-in clauses that, immediately before a trailing artifact noun, read as
 * a fresh promise of a follow-up artifact ("here is the chart", "drafting
 * the figure", "more in the article").
 *
 * Deliberately excludes bare articles ("the", "a", "this") on their own:
 * "I really like this article" ends with "this article" but is not a
 * promise. Each lead-in pairs a presentation/production/reference verb with
 * the article so only deliberate hand-offs match.
 */
const ARTIFACT_PROMISE_LEADINS: readonly string[] = [
  "here is the",
  "here is a",
  "here's the",
  "here's a",
  "here is",
  "here's",
  "drafting the",
  "drafting a",
  "building the",
  "building a",
  "rendering the",
  "rendering a",
  "preparing the",
  "preparing a",
  "put together the",
  "put together a",
  "in the",
  "in a",
  "see the",
  "see a",
  "attached the",
  "attached a",
];

/**
 * Window after the message for which the skeleton is shown (ms).
 *
 * Sized to comfortably cover a long codex turn (~100s) plus broker/SSE lag,
 * so the skeleton does not age out mid-build. It unmounts the instant the
 * artifact marker lands (see {@link hasArtifactArrivedFromSameAgent}) and on
 * the agent leaving "active", so the wide window is an upper bound, not the
 * common case.
 */
export const ARTIFACT_SKELETON_RECENCY_WINDOW_MS = 180_000;

interface ArtifactSkeletonProps {
  /** Caption next to the FIG label. Defaults to "drafting figure". */
  label?: string;
  /**
   * Optional figure number override. Defaults to a stable per-mount pick so
   * two skeletons rendered in the same channel won't collide on "FIG 001".
   */
  figureNumber?: number;
}

/**
 * Technical-manual draft preview shown while an agent is building an HTML
 * artifact. Visual language matches the artifact aesthetic (paper card,
 * accent hairlines, monospace captions, schematic figure being plotted
 * live) so the loader previews the form factor instead of looking like a
 * generic shimmer block.
 *
 * Motion: SVG strokes self-draw via stroke-dashoffset, a pulsing accent
 * dot signals "live", an ellipsis ticks under the caption, and a thin
 * accent progress sliver grows over ~25s and loops. All compositor-only.
 * `prefers-reduced-motion` collapses everything to a static state with
 * the figure pre-drawn.
 *
 * Pure presentational. Triggering and unmount are the caller's job.
 */
export function ArtifactSkeleton({
  label = "drafting figure",
  figureNumber,
}: ArtifactSkeletonProps = {}) {
  const fig = useMemo(
    () => figureNumber ?? pickStableFigureNumber(),
    [figureNumber],
  );
  const figLabel = `FIG_${fig.toString().padStart(3, "0")}`;
  const ariaLabel = `${figLabel} — ${label}`;

  return (
    <div
      className="artifact-skeleton"
      role="status"
      aria-live="polite"
      aria-label={ariaLabel}
      data-testid="artifact-skeleton"
    >
      <div className="artifact-skeleton-head">
        <span className="artifact-skeleton-figlabel">
          <span className="artifact-skeleton-dot" aria-hidden="true" />
          {figLabel}
        </span>
        <span className="artifact-skeleton-meta" aria-hidden="true">
          NOTEBOOK · DRAFT
        </span>
      </div>
      <div className="artifact-skeleton-plot" aria-hidden="true">
        <svg
          viewBox="0 0 520 96"
          preserveAspectRatio="none"
          className="artifact-skeleton-svg"
          role="presentation"
          focusable="false"
        >
          {/* Hairline grid — paper-light, static */}
          <g className="artifact-skeleton-grid">
            <line x1="0" y1="0" x2="520" y2="0" />
            <line x1="0" y1="32" x2="520" y2="32" />
            <line x1="0" y1="64" x2="520" y2="64" />
            <line x1="0" y1="96" x2="520" y2="96" />
            <line x1="0" y1="0" x2="0" y2="96" />
            <line x1="130" y1="0" x2="130" y2="96" />
            <line x1="260" y1="0" x2="260" y2="96" />
            <line x1="390" y1="0" x2="390" y2="96" />
            <line x1="520" y1="0" x2="520" y2="96" />
          </g>
          {/* Two sketched paths that draw themselves in. Curve A is a
              rising trend; curve B is a step series. Together they read as
              "a chart is being plotted" without committing to a specific
              figure type. */}
          <path
            className="artifact-skeleton-stroke artifact-skeleton-stroke-a"
            d="M8 80 C 80 70, 150 40, 220 36 S 360 22, 510 14"
            fill="none"
          />
          <path
            className="artifact-skeleton-stroke artifact-skeleton-stroke-b"
            d="M8 86 L 100 86 L 100 64 L 200 64 L 200 48 L 320 48 L 320 28 L 510 28"
            fill="none"
          />
        </svg>
      </div>
      <div className="artifact-skeleton-caption">
        <span className="artifact-skeleton-caption-text">{label}</span>
        <span className="artifact-skeleton-ellipsis" aria-hidden="true">
          <span />
          <span />
          <span />
        </span>
      </div>
      <span className="artifact-skeleton-progress" aria-hidden="true">
        <span className="artifact-skeleton-progress-bar" />
      </span>
    </div>
  );
}

/**
 * Pick a per-mount figure number in [1, 99]. Deterministic per render but
 * varies between mounts so two parallel skeletons don't both read "FIG_001".
 * Uses `Math.random` because there's no semantic meaning — purely visual.
 */
function pickStableFigureNumber(): number {
  return Math.floor(Math.random() * 99) + 1;
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
 *  2. message body ends with an artifact promise (see
 *     {@link endsWithArtifactPromise}).
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

/**
 * Decide whether `content` ends with a short clause promising a follow-up
 * artifact. The match is precise by construction: it only fires on the
 * trailing clause (last sentence/line), so a "chart" mentioned earlier in a
 * long message never triggers it.
 *
 * Matches when the trailing clause:
 *  - ends with a deictic promise ("...below", "...rendered", "see the …"), OR
 *  - ends with a lead-in + artifact noun ("...the chart", "here is the
 *    figure", "...drafting the diagram"), OR
 *  - ends with a bare artifact noun preceded by a lead-in word anywhere in
 *    the clause ("...the full breakdown").
 */
function endsWithArtifactPromise(content: string): boolean {
  const tail = trailingClause(content);
  if (!tail) return false;
  return (
    endsWithDeicticPromise(tail) ||
    endsWithPendingArtifact(tail) ||
    endsWithLeadInArtifactNoun(tail)
  );
}

/** Strong deictic promise at the very end ("...rendered below", "see the …"). */
function endsWithDeicticPromise(tail: string): boolean {
  return ARTIFACT_PROMISE_DEICTICS.some((deictic) => tail.endsWith(deictic));
}

/**
 * "Still producing" verb at the end, but only when an artifact noun is
 * present earlier in the clause ("the chart is coming", "diagram incoming").
 * Keeps everyday "...the weekend is coming" from matching.
 */
function endsWithPendingArtifact(tail: string): boolean {
  if (!ARTIFACT_PROMISE_PENDING_VERBS.some((verb) => tail.endsWith(verb))) {
    return false;
  }
  return clauseMentionsArtifactNoun(tail);
}

/**
 * Lead-in + artifact noun at the very end ("...here is the chart",
 * "...drafting the figure"). Requiring the lead-in keeps a sentence that
 * happens to end on a noun ("I like this article") from matching unless it
 * reads as a fresh promise.
 */
function endsWithLeadInArtifactNoun(tail: string): boolean {
  for (const noun of ARTIFACT_PROMISE_NOUNS) {
    if (!tail.endsWith(noun)) continue;
    const head = tail.slice(0, tail.length - noun.length).trimEnd();
    if (ARTIFACT_PROMISE_LEADINS.some((leadin) => head.endsWith(leadin))) {
      return true;
    }
  }
  return false;
}

/** True when the clause contains an artifact noun as a whole word. */
function clauseMentionsArtifactNoun(clause: string): boolean {
  for (const noun of ARTIFACT_PROMISE_NOUNS) {
    if (clause === noun) return true;
    if (
      clause.startsWith(`${noun} `) ||
      clause.endsWith(` ${noun}`) ||
      clause.includes(` ${noun} `)
    ) {
      return true;
    }
  }
  return false;
}

/**
 * Lowercase, punctuation-stripped trailing clause of a message — the last
 * sentence or line. Matching against the clause (not the whole tail) is what
 * keeps the heuristic from firing on artifact nouns buried mid-message.
 */
function trailingClause(content: string): string {
  const trimmed = content.trim();
  if (!trimmed) return "";
  // Split on sentence terminators and line breaks; take the final non-empty
  // segment so "Some context here. Full breakdown below." matches on the
  // promise clause, not the whole body.
  const segments = trimmed
    .slice(-240)
    .split(/[.!?\n]+/)
    .map((s) => s.trim())
    .filter(Boolean);
  const clause = segments.length ? segments[segments.length - 1] : trimmed;
  return clause
    .toLowerCase()
    .replace(/[^a-z0-9 '-]/g, " ")
    .replace(/\s+/g, " ")
    .trim();
}
