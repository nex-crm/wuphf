import { useChannels } from "../../hooks/useChannels";
import { useAppStore } from "../../stores/app";

export function ChannelHeader() {
  const currentChannel = useAppStore((s) => s.currentChannel);
  const currentApp = useAppStore((s) => s.currentApp);
  const setSearchOpen = useAppStore((s) => s.setSearchOpen);
  const theme = useAppStore((s) => s.theme);
  const setTheme = useAppStore((s) => s.setTheme);
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
          title={theme === "nex-dark" ? "Light mode" : "Dark mode"}
          aria-label={theme === "nex-dark" ? "Switch to light mode" : "Switch to dark mode"}
          onClick={() => setTheme(theme === "nex-dark" ? "nex" : "nex-dark")}
        >
          {theme === "nex-dark" ? (
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <circle cx="12" cy="12" r="4"/>
              <path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41"/>
            </svg>
          ) : (
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/>
            </svg>
          )}
        </button>
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
