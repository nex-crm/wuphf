import { useChannelMembers, useOfficeMembers } from "../../hooks/useMembers";
import { useCurrentRoute } from "../../routes/useCurrentRoute";
import { PixelAvatar } from "../ui/PixelAvatar";
import { ThinkingLoader } from "../ui/ThinkingLoader";

/**
 * TypingIndicator renders as an incoming message bubble pinned to the bottom
 * of the feed — the exact spot the next message will land, à la Claude. When
 * the real message arrives it replaces this placeholder in place, so the
 * conversation reads as one continuous stream instead of a footer caption
 * blinking on and off below the composer.
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

  const names = active.map((m) => m.name || m.slug);
  const heading =
    names.length === 1
      ? names[0]
      : names.length <= 3
        ? names.join(", ")
        : `${names.length} agents`;
  const verb = names.length === 1 ? "is typing" : "are typing";
  const label = `${heading} ${verb}…`;

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
        </div>
        <ThinkingLoader label={label} />
      </div>
    </div>
  );
}
