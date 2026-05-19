import { MailIn, Settings, SidebarCollapse } from "iconoir-react";

import type { Meta, StoryObj } from "@storybook/react-vite";

import { HarnessBadge } from "../ui/HarnessBadge";
import { PixelAvatar } from "../ui/PixelAvatar";

const meta: Meta = {
  title: "Sidebar/Full view",
  parameters: {
    layout: "fullscreen",
    backgrounds: { default: "elevated" },
  },
};

export default meta;

export const Expanded: StoryObj = {
  render: () => (
    <div style={{ display: "flex", minHeight: "100vh" }}>
      <FullSidebar />
      <div
        style={{
          flex: 1,
          padding: 32,
          color: "var(--text)",
          background: "var(--bg)",
        }}
      >
        <h2 style={{ margin: 0, marginBottom: 8 }}>Channel content area</h2>
        <p style={{ color: "var(--text-secondary)", maxWidth: 520 }}>
          The sidebar is the persistent navigation rail; the right side
          renders the active route. Switch themes from the toolbar to see
          the sidebar reskin via tokens.
        </p>
      </div>
    </div>
  ),
};

export const Collapsed: StoryObj = {
  render: () => (
    <div style={{ display: "flex", minHeight: "100vh" }}>
      <CollapsedRail />
      <div
        style={{
          flex: 1,
          padding: 32,
          color: "var(--text)",
          background: "var(--bg)",
        }}
      >
        <h2 style={{ margin: 0, marginBottom: 8 }}>Collapsed rail</h2>
        <p style={{ color: "var(--text-secondary)", maxWidth: 520 }}>
          When collapsed, the sidebar shrinks to a 48px icon strip with
          hover popovers for each group. Inbox + active app stay visible
          for one-click return.
        </p>
      </div>
    </div>
  ),
};

export const Unread: StoryObj = {
  name: "Heavy unread state",
  render: () => (
    <div style={{ display: "flex", minHeight: "100vh" }}>
      <FullSidebar unread />
      <div
        style={{
          flex: 1,
          padding: 32,
          color: "var(--text)",
          background: "var(--bg)",
        }}
      >
        <h2 style={{ margin: 0, marginBottom: 8 }}>Heavy unread</h2>
        <p style={{ color: "var(--text-secondary)", maxWidth: 520 }}>
          Inbox + per-channel badges show how the sidebar feels when the
          user has been away. Badge stays a chip; the Kbd hint hides when
          there's an unread to surface.
        </p>
      </div>
    </div>
  ),
};

function FullSidebar({ unread = false }: { unread?: boolean }) {
  return (
    <aside className="sidebar" style={{ width: 240, flexShrink: 0 }}>
      <div className="sidebar-header">
        <span className="sidebar-logo">WUPHF</span>
        <div className="sidebar-header-actions">
          <button
            type="button"
            className="sidebar-icon-btn"
            aria-label="Collapse sidebar"
          >
            <SidebarCollapse />
          </button>
          <button
            type="button"
            className="sidebar-icon-btn"
            aria-label="Open settings"
          >
            <Settings />
          </button>
        </div>
      </div>

      <div className="sidebar-primary" style={{ padding: 12 }}>
        <a className="sidebar-item" href="#inbox">
          <span className="sidebar-item-icon">
            <MailIn width={16} height={16} />
          </span>
          <span className="sidebar-item-label">
            <span className="sidebar-item-label-inner">Inbox</span>
          </span>
          {unread ? <span className="sidebar-badge">12</span> : null}
        </a>
      </div>

      <div className="sidebar-section is-team">
        <div className="sidebar-section-title">Agents</div>
      </div>
      <div className="sidebar-collapsible is-open">
        <div className="sidebar-agents">
          {AGENTS.map((a) => (
            <a key={a.slug} className="sidebar-agent" href={`#${a.slug}`}>
              <span className="sidebar-agent-wrap">
                <PixelAvatar
                  slug={a.slug}
                  size={20}
                  className="pixel-avatar-sidebar"
                />
                <span className={`status-dot ${a.dot}`} aria-hidden="true" />
                <HarnessBadge kind={a.harness} size={12} />
              </span>
              <span className="sidebar-agent-name">{a.slug}</span>
              <span className="sidebar-agent-task">{a.task}</span>
            </a>
          ))}
          <button type="button" className="sidebar-add-btn">
            <span>+</span> <span>New agent</span>
          </button>
        </div>
      </div>

      <div className="sidebar-section">
        <div className="sidebar-section-title">Channels</div>
      </div>
      <div className="sidebar-collapsible is-open">
        <div className="sidebar-channels">
          {CHANNELS.map((c, i) => (
            <a
              key={c.slug}
              className={`sidebar-item${c.active ? " active" : ""}`}
              href={`#${c.slug}`}
            >
              <span className="sidebar-item-icon">#</span>
              <span className="sidebar-item-label">
                <span className="sidebar-item-label-inner">{c.slug}</span>
              </span>
              {unread && c.unread > 0 ? (
                <span className="sidebar-badge">{c.unread}</span>
              ) : (
                <kbd
                  className="kbd kbd-sm"
                  style={{ marginLeft: "auto", opacity: 0.5 }}
                >
                  ⌘{i + 1}
                </kbd>
              )}
            </a>
          ))}
          <button type="button" className="sidebar-add-btn">
            <span>+</span> <span>New channel</span>
          </button>
        </div>
      </div>

      <div className="sidebar-section">
        <div className="sidebar-section-title">Tools</div>
      </div>
      <div className="sidebar-collapsible is-open">
        <div className="sidebar-apps">
          {APPS.map((app) => (
            <a
              key={app.id}
              className={`sidebar-item${app.active ? " active" : ""}`}
              href={`#${app.id}`}
            >
              <span className="sidebar-item-emoji">{app.icon}</span>
              <span className="sidebar-item-label">
                <span className="sidebar-item-label-inner">{app.name}</span>
              </span>
            </a>
          ))}
        </div>
      </div>

      <div
        className="workspace-summary"
        style={{
          padding: "var(--space-3) var(--space-4)",
          display: "flex",
          flexDirection: "column",
          gap: 4,
          fontSize: "var(--text-xs)",
          color: "var(--nex-sidebar-text, var(--text-secondary))",
          borderTop: "1px solid var(--border)",
          marginTop: "auto",
        }}
      >
        <span>{AGENTS.length} agents · 8 tasks</span>
        <span>14.2k tokens · $0.84</span>
      </div>
    </aside>
  );
}

function CollapsedRail() {
  return (
    <aside className="sidebar sidebar-collapsed" style={{ flexShrink: 0 }}>
      <div className="sidebar-rail-top">
        <button
          type="button"
          className="sidebar-icon-btn"
          aria-label="Expand sidebar"
        >
          <SidebarCollapse />
        </button>
      </div>
      <div className="sidebar-rail-middle">
        <button
          type="button"
          className="sidebar-icon-btn active"
          aria-label="Inbox"
        >
          <MailIn width={18} height={18} />
        </button>
        <button
          type="button"
          className="sidebar-icon-btn"
          aria-label="Agents"
        >
          <span style={{ fontSize: 14 }}>👥</span>
        </button>
        <button
          type="button"
          className="sidebar-icon-btn"
          aria-label="Channels"
        >
          <span style={{ fontSize: 14 }}>#</span>
        </button>
      </div>
      <div className="sidebar-rail-apps">
        {APPS.slice(0, 5).map((app) => (
          <button
            key={app.id}
            type="button"
            className={`sidebar-icon-btn${app.active ? " active" : ""}`}
            aria-label={app.name}
            title={app.name}
          >
            <span style={{ fontSize: 14 }}>{app.icon}</span>
          </button>
        ))}
      </div>
      <div className="sidebar-rail-bottom">
        <button
          type="button"
          className="sidebar-icon-btn"
          aria-label="Settings"
        >
          <Settings width={18} height={18} />
        </button>
      </div>
    </aside>
  );
}

const AGENTS = [
  {
    slug: "atlas",
    task: "writing migration plan",
    harness: "claude-code" as const,
    dot: "shipping",
  },
  {
    slug: "lina",
    task: "wireframing the inbox",
    harness: "codex" as const,
    dot: "plotting",
  },
  {
    slug: "sage",
    task: "drafting the FAQ",
    harness: "opencode" as const,
    dot: "active",
  },
  {
    slug: "ops",
    task: "watching CI",
    harness: "hermes-agent" as const,
    dot: "lurking",
  },
];

const CHANNELS = [
  { slug: "architecture", unread: 0, active: true },
  { slug: "deploys", unread: 2, active: false },
  { slug: "wiki", unread: 0, active: false },
  { slug: "incidents", unread: 12, active: false },
];

const APPS = [
  { id: "overview", icon: "🏠", name: "Overview", active: false },
  { id: "wiki", icon: "📖", name: "Wiki", active: true },
  { id: "console", icon: ">", name: "Console", active: false },
  { id: "calendar", icon: "📅", name: "Calendar", active: false },
  { id: "skills", icon: "⚡", name: "Skills", active: false },
];
