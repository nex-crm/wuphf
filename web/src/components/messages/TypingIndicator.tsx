import type { OfficeMember } from "../../api/client";
import { useChannelMembers, useOfficeMembers } from "../../hooks/useMembers";
import { useCurrentRoute } from "../../routes/useCurrentRoute";

/**
 * Live "agent is working" indicator shown under the message feed during a
 * turn. The deeper requirement (vs a fragile phrase-triggered skeleton) is
 * that the user always knows what's happening across a long (~100s) turn:
 * the broker pushes a per-agent progress detail (`liveActivity`/`activity`/
 * `detail`, fed by the headless runner's thinking/tool_use/text states), so
 * when one is present we surface it ("scoping issue", "drafting figure",
 * "writing article") instead of a bare "is typing…".
 *
 * Falls back to the classic typing label when no detail is available, so the
 * indicator never goes silent while an agent is active.
 */
export function TypingIndicator() {
  const route = useCurrentRoute();
  const currentChannel =
    route.kind === "channel" || route.kind === "dm"
      ? route.channelSlug
      : "general";
  const dm = route.kind === "dm" ? { agentSlug: route.agentSlug } : null;
  const { data: members = [] } = useOfficeMembers();
  const { data: channelMembers = [] } = useChannelMembers(currentChannel);
  const channelMemberSlugs = new Set(channelMembers.map((m) => m.slug));

  const active = members.filter((m) => {
    if (m.status !== "active" || m.slug === "human") return false;
    if (dm) return m.slug === dm.agentSlug;
    return channelMemberSlugs.size === 0 || channelMemberSlugs.has(m.slug);
  });

  if (active.length === 0) return null;

  const label = buildLabel(active);
  const detail = resolveProgressDetail(active);

  return (
    <div className="typing-indicator">
      <div className="typing-dots" aria-hidden="true">
        <span className="typing-dot" />
        <span className="typing-dot" />
        <span className="typing-dot" />
      </div>
      <span className="typing-indicator-label">{label}</span>
      {detail ? (
        <>
          {/* Hairline separator between the typing label and the live
              progress detail. Token-driven via CSS so it tracks the theme. */}
          <span className="typing-indicator-sep" aria-hidden="true">
            ·
          </span>
          <span
            className="typing-indicator-detail"
            // Live region so screen readers announce progress updates without
            // a focus change. `aria-live="polite"` matches the unobtrusive
            // intent — it's ambient status, not an alert.
            aria-live="polite"
          >
            {detail}
          </span>
        </>
      ) : null}
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
