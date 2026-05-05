import { useChannels } from "../../hooks/useChannels";
import { appTitle } from "../../lib/constants";
import { useCurrentRoute } from "../../routes/useCurrentRoute";
import type { Theme } from "../../stores/app";
import { useAppStore } from "../../stores/app";

function nextTheme(t: Theme): Theme {
  if (t === "noir-gold") return "nex";
  if (t === "nex") return "nex-dark";
  return "noir-gold";
}

function themeLabel(t: Theme): string {
  if (t === "noir-gold") return "Noir Gold";
  if (t === "nex-dark") return "Nex Dark";
  return "Nex Light";
}

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
  const theme = useAppStore((s) => s.theme);
  const setTheme = useAppStore((s) => s.setTheme);
  const { data: channels = [] } = useChannels();

  const { title, desc } = headerTitleAndDesc(route, channels);
  const targetTheme = nextTheme(theme);

  return (
    <div className="channel-header">
      <div style={{ display: "flex", alignItems: "center" }}>
        <span className="channel-title">{title}</span>
        {desc ? <span className="channel-desc">{desc}</span> : null}
      </div>
      <div className="channel-actions">
        <button
          type="button"
          className="sidebar-btn"
          title={`Switch theme to ${themeLabel(targetTheme)}`}
          aria-label={`Switch theme to ${themeLabel(targetTheme)}`}
          onClick={() => setTheme(targetTheme)}
        >
          {targetTheme === "noir-gold" ? (
            // Sparkle / star — switch to the gold theme
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
              <path d="M12 3l1.8 5.5H19l-4.6 3.4 1.8 5.6L12 14l-4.2 3.5 1.8-5.6L5 8.5h5.2L12 3z" />
            </svg>
          ) : targetTheme === "nex" ? (
            // Sun — switch to the light theme
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
              <circle cx="12" cy="12" r="4" />
              <path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41" />
            </svg>
          ) : (
            // Moon — switch to the dark theme
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
              <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z" />
            </svg>
          )}
        </button>
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
