import { useChannels } from "../../hooks/useChannels";
import { useAppStore } from "../../stores/app";

export function ChannelHeader() {
  const currentChannel = useAppStore((s) => s.currentChannel);
  const currentApp = useAppStore((s) => s.currentApp);
  const setSearchOpen = useAppStore((s) => s.setSearchOpen);
  const { data: channels = [] } = useChannels();

  const channel = channels.find((c) => c.slug === currentChannel);
  const title = currentApp
    ? currentApp.charAt(0).toUpperCase() + currentApp.slice(1)
    : `# ${currentChannel}`;
  const desc = currentApp ? "" : channel?.description || "";

  return (
    <div className="channel-header">
      <div style={{ display: "flex", alignItems: "center" }}>
        <span className="channel-title">{title}</span>
        {desc && <span className="channel-desc">{desc}</span>}
      </div>
      <div className="channel-actions">
        <button
          className="sidebar-btn"
          title="Search"
          aria-label="Search"
          onClick={() => setSearchOpen(true)}
        >
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <circle cx="11" cy="11" r="8" />
            <path d="m21 21-4.3-4.3" />
          </svg>
        </button>
      </div>
    </div>
  );
}
