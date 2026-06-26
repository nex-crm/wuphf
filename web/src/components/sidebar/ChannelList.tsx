import { type Channel, useChannels } from "../../hooks/useChannels";
import { useOverflow } from "../../hooks/useOverflow";
import { router } from "../../lib/router";
import { useCurrentRoute } from "../../routes/useCurrentRoute";
import { useAppStore } from "../../stores/app";
import { ChannelWizard, useChannelWizard } from "../channels/ChannelWizard";
import { Kbd, MOD_KEY } from "../ui/Kbd";
import { SidebarItem } from "./SidebarItem";

function navigateToChannel(channelSlug: string): void {
  void router.navigate({
    to: "/channels/$channelSlug",
    params: { channelSlug },
  });
}

function ChannelRow({
  channel,
  index,
  active,
  unreadCount,
  onSelect,
}: {
  channel: Channel;
  index: number;
  active: boolean;
  unreadCount: number;
  onSelect: (slug: string) => void;
}) {
  // Only the first 9 get a Cmd+N shortcut — the global handler caps there,
  // so advertising #10+ would be a lie.
  const shortcutIdx = index < 9 ? index + 1 : null;
  const name = channel.name || channel.slug;
  const title =
    shortcutIdx !== null ? `${name} — ${MOD_KEY}${shortcutIdx}` : name;
  const unreadLabel = unreadCount > 99 ? "99+" : String(unreadCount);
  const buttonLabel = unreadCount > 0 ? `${name}, ${unreadCount} unread` : name;

  return (
    <SidebarItem
      icon="#"
      label={name}
      active={active}
      onClick={() => onSelect(channel.slug)}
      aria-label={buttonLabel}
      title={title}
      badge={
        unreadCount > 0 ? (
          <span className="sidebar-badge" title={`${unreadCount} unread`}>
            {unreadLabel}
          </span>
        ) : undefined
      }
      shortcut={
        shortcutIdx !== null ? (
          <span className="sidebar-shortcut" aria-hidden="true">
            <Kbd size="sm">{`${MOD_KEY}${shortcutIdx}`}</Kbd>
          </span>
        ) : undefined
      }
    />
  );
}

export function ChannelList() {
  const { data: channels = [] } = useChannels();
  const route = useCurrentRoute();
  const unreadByChannel = useAppStore((s) => s.unreadByChannel);
  const wizard = useChannelWizard();
  const overflowRef = useOverflow<HTMLDivElement>();
  const activeChannelSlug = route.kind === "channel" ? route.channelSlug : null;

  return (
    <>
      <div className="sidebar-scroll-wrap is-channels">
        <div className="sidebar-channels" ref={overflowRef}>
          {channels.map((ch, idx) => {
            const isActive = activeChannelSlug === ch.slug;
            const unreadCount = unreadByChannel[ch.slug] ?? 0;
            return (
              <ChannelRow
                key={ch.slug}
                channel={ch}
                index={idx}
                active={isActive}
                unreadCount={unreadCount}
                onSelect={navigateToChannel}
              />
            );
          })}
          <SidebarItem
            variant="add"
            icon="+"
            label="New Channel"
            onClick={wizard.show}
            title="Create a new channel"
          />
        </div>
      </div>
      <ChannelWizard open={wizard.open} onClose={wizard.hide} />
    </>
  );
}
