import { useChannels } from "../../hooks/useChannels";
import { appTitle } from "../../lib/constants";
import { useCurrentRoute } from "../../routes/useCurrentRoute";
import { useAppStore } from "../../stores/app";
import { ThemeSwitcher } from "./ThemeSwitcher";

function headerTitleAndDesc(
  route: ReturnType<typeof useCurrentRoute>,
  channels: { slug: string; description?: string }[],
): { title: string; desc: string } {
  switch (route.kind) {
    case "channel": {
      const ch = channels.find((c) => c.slug === route.channelSlug);
      return {
        title: `# ${route.channelSlug}`,
        desc: ch?.description || "",
      };
    }
    case "dm":
      return { title: `@${route.agentSlug}`, desc: "" };
    case "app":
      return { title: appTitle(route.appId), desc: "" };
    case "task-board":
    case "task-detail":
      return { title: appTitle("tasks"), desc: "" };
    case "wiki":
    case "wiki-article":
    case "wiki-lookup":
      return { title: appTitle("wiki"), desc: "" };
    case "notebook-catalog":
    case "notebook-agent":
    case "notebook-entry":
      return { title: "Notebooks", desc: "" };
    case "reviews":
      return { title: "Reviews", desc: "" };
    case "unknown":
      return { title: "", desc: "" };
    default: {
      const _exhaustive: never = route;
      void _exhaustive;
      return { title: "", desc: "" };
    }
  }
}

export function ChannelHeader() {
  const route = useCurrentRoute();
  const setSearchOpen = useAppStore((s) => s.setSearchOpen);
  const { data: channels = [] } = useChannels();

  const { title, desc } = headerTitleAndDesc(route, channels);

  return (
    <div className="channel-header">
      <div style={{ display: "flex", alignItems: "center" }}>
        <span className="channel-title">{title}</span>
        {desc ? <span className="channel-desc">{desc}</span> : null}
      </div>
      <div className="channel-actions">
        <ThemeSwitcher />
        <button
          type="button"
          className="sidebar-btn"
          title="Search"
          aria-label="Search"
          onClick={() => setSearchOpen(true)}
        >
          <svg
            aria-hidden="true"
            focusable="false"
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
