import type { OfficeMember } from "../../api/client";
import { useChannelMembers, useOfficeMembers } from "../../hooks/useMembers";
import { OFFICE_LOADING_PHRASES } from "../../lib/officeLoadingPhrases";
import { useCurrentRoute } from "../../routes/useCurrentRoute";
import { PixelAvatar } from "../ui/PixelAvatar";
import { ThinkingLoader } from "../ui/ThinkingLoader";

/**
 * TypingIndicator renders as an incoming message bubble pinned to the bottom
 * of the feed — the exact spot the next message will land, à la Claude. When
 * the real message arrives it replaces this placeholder in place, so the
 * conversation reads as one continuous stream instead of a footer caption
 * blinking on and off below the composer.
 *
 * Beyond "who is composing", the deeper requirement (vs a fragile
 * phrase-triggered skeleton) is that the user always knows WHAT is happening
 * across a long (~100s) turn: the broker pushes a per-agent progress detail
 * (`liveActivity`/`task`/`activity`/`detail`, fed by the headless runner's
 * thinking/tool_use/text states), so when exactly one agent is active we
 * surface it ("scoping issue", "drafting figure", "writing article") next to
 * the typing label. Falls back to the classic typing bubble when no detail is
 * available, so the indicator never goes silent while an agent is active.
 */
export function TypingIndicator({ channel }: { channel?: string } = {}) {
  const route = useCurrentRoute();
  // Prefer an explicit channel (the task-detail chat passes it, since
  // useCurrentRoute reports kind "task-detail" there, not "channel"). Fall back
  // to the channel route slug so the channel surface keeps working unchanged.
  const currentChannel =
    channel ?? (route.kind === "channel" ? route.channelSlug : "general");
  const { data: members = [] } = useOfficeMembers();
  const { data: channelMembers = [] } = useChannelMembers(currentChannel);
  const channelMemberSlugs = new Set(channelMembers.map((m) => m.slug));

  const active = members.filter((m) => {
    if (m.status !== "active" || m.slug === "human") return false;
    return channelMemberSlugs.size === 0 || channelMemberSlugs.has(m.slug);
  });

  if (active.length === 0) return null;

  const names = active.map((m) => m.name || m.slug);
  const heading =
    names.length === 1
      ? names[0]
      : names.length <= 3
        ? names.join(", ")
        : `${names.length} agents`;
  const verb = names.length === 1 ? "is typing" : "are typing";
  // buildLabel carries the canonical "X is typing..." accessible label so the
  // loader's screen-reader text and aria-label stay stable across surfaces.
  const label = buildLabel(active);
  // Live per-agent progress detail (single active agent only) surfaced next to
  // the typing verb so the user sees WHAT is happening, not just WHO.
  const detail = resolveProgressDetail(active);

  // Up to three stacked avatars echo who is composing; the bubble itself
  // carries the live loader so the focus stays on the incoming message.
  const avatarSlugs = active.slice(0, 3).map((m) => m.slug);

  return (
    <div className="message typing-message" data-testid="typing-indicator">
      <div className="message-avatar typing-message-avatar">
        {avatarSlugs.map((slug, i) => (
          <span
            key={slug}
            className="typing-avatar-stack-item"
            style={{ zIndex: avatarSlugs.length - i }}
          >
            <PixelAvatar slug={slug} size={24} />
          </span>
        ))}
      </div>
      <div className="message-content">
        <div className="message-header">
          <span className="message-author">{heading}</span>
          <span className="typing-verb">{verb}</span>
          {detail ? (
            <>
              {/* Hairline separator between the typing verb and the live
                  progress detail. Token-driven via CSS so it tracks the
                  theme. */}
              <span className="typing-indicator-sep" aria-hidden="true">
                ·
              </span>
              <span
                className="typing-indicator-detail"
                // Live region so screen readers announce progress updates
                // without a focus change. `aria-live="polite"` matches the
                // unobtrusive intent — it's ambient status, not an alert.
                aria-live="polite"
              >
                {detail}
              </span>
            </>
          ) : null}
        </div>
        <ThinkingLoader label={label} phrases={OFFICE_LOADING_PHRASES} />
      </div>
    </div>
  );
}

/** Names-based "is typing…" label. Used as the always-present base line. */
function buildLabel(active: ReadonlyArray<OfficeMember>): string {
  const names = active.map((m) => m.name || m.slug);
  if (names.length === 1) return `${names[0]} is typing...`;
  if (names.length <= 3) return `${names.join(", ")} are typing...`;
  return `${names.length} agents are typing...`;
}

/**
 * Resolve the human-readable progress detail to show alongside the typing
 * label. Mirrors ChannelParticipants' precedence so the same broker signal
 * reads consistently across surfaces: `liveActivity` (the freshest headless
 * progress string) wins, then the active `task`, then `activity`, then the
 * lower-level `detail`.
 *
 * When exactly one agent is active we show its detail; with several active
 * we suppress it to avoid implying one agent's progress is shared.
 */
function resolveProgressDetail(active: ReadonlyArray<OfficeMember>): string {
  if (active.length !== 1) return "";
  const [member] = active;
  return (
    member.liveActivity?.trim() ||
    member.task?.trim() ||
    member.activity?.trim() ||
    member.detail?.trim() ||
    ""
  );
}
