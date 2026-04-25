import { useChannels } from "../../hooks/useChannels";
import { useOverflow } from "../../hooks/useOverflow";
import { useAppStore } from "../../stores/app";
import { ChannelWizard, useChannelWizard } from "../channels/ChannelWizard";
import { Kbd, MOD_KEY } from "../ui/Kbd";
import { SidebarItemLabel } from "./SidebarItemLabel";

export function ChannelList() {
  const { data: channels = [] } = useChannels();
  const currentChannel = useAppStore((s) => s.currentChannel);
  const setCurrentChannel = useAppStore((s) => s.setCurrentChannel);
  const currentApp = useAppStore((s) => s.currentApp);
  const wizard = useChannelWizard();
  const overflowRef = useOverflow<HTMLDivElement>();

  return (
    <>
      <div className="sidebar-scroll-wrap is-channels">
        <div className="sidebar-channels" ref={overflowRef}>
          {channels.map((ch, idx) => {
            const isActive = currentChannel === ch.slug && !currentApp;
            // Only the first 9 get a Cmd+N shortcut — the global handler
            // caps there, so advertising #10+ would be a lie.
            const shortcutIdx = idx < 9 ? idx + 1 : null;
            const title =
              shortcutIdx !== null
                ? `${ch.name || ch.slug} — ${MOD_KEY}${shortcutIdx}`
                : ch.name || ch.slug;
            return (
              <button
                key={ch.slug}
                className={`sidebar-item${isActive ? " active" : ""}`}
                onClick={() => setCurrentChannel(ch.slug)}
                title={title}
              >
                <span
                  style={{
                    color: "currentColor",
                    width: 18,
                    textAlign: "center",
                    flexShrink: 0,
                  }}
                >
                  #
                </span>
                <SidebarItemLabel>{ch.name || ch.slug}</SidebarItemLabel>
                {shortcutIdx !== null && (
                  <span className="sidebar-shortcut" aria-hidden="true">
                    <Kbd size="sm">{`${MOD_KEY}${shortcutIdx}`}</Kbd>
                  </span>
                )}
              </button>
            );
          })}
          <button
            className="sidebar-item sidebar-add-btn"
            onClick={wizard.show}
            title="Create a new channel"
          >
            <span
              style={{
                width: 18,
                textAlign: "center",
                flexShrink: 0,
                display: "inline-block",
              }}
            >
              +
            </span>
            <span>New Channel</span>
          </button>
        </div>
      </div>
      <ChannelWizard open={wizard.open} onClose={wizard.hide} />
    </>
  );
}
