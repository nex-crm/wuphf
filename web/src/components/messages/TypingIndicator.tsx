import { useChannelMembers, useOfficeMembers } from "../../hooks/useMembers";
import { useCurrentRoute } from "../../routes/useCurrentRoute";

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
  const label =
    names.length === 1
      ? `${names[0]} is typing...`
      : names.length <= 3
        ? `${names.join(", ")} are typing...`
        : `${names.length} agents are typing...`;

  return (
    <div className="typing-indicator" style={{ padding: "0 20px 8px" }}>
      <div className="typing-dots">
        <span className="typing-dot" />
        <span className="typing-dot" />
        <span className="typing-dot" />
      </div>
      <span>{label}</span>
    </div>
  );
}
