import { MailIn, Settings, SidebarCollapse } from "iconoir-react";

import type { Meta, StoryObj } from "@storybook/react-vite";

import { HarnessBadge } from "../ui/HarnessBadge";
import { PixelAvatar } from "../ui/PixelAvatar";

const meta: Meta = {
  title: "Sidebar/Modules",
  parameters: {
    layout: "padded",
    backgrounds: { default: "elevated" },
  },
  decorators: [
    (Story) => (
      <aside
        className="sidebar"
        style={{
          width: 240,
          height: "auto",
          minHeight: 280,
          borderRadius: "var(--radius-md)",
          display: "flex",
          flexDirection: "column",
        }}
      >
        <Story />
      </aside>
    ),
  ],
};

export default meta;

export const WorkspaceHeader: StoryObj = {
  name: "Workspace header",
  render: () => (
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
  ),
};

export const InboxButton: StoryObj = {
  name: "Inbox button",
  render: () => (
    <div className="sidebar-primary" style={{ padding: 12 }}>
      <a className="sidebar-item active" href="#inbox">
        <span className="sidebar-item-icon">
          <MailIn width={16} height={16} />
        </span>
        <span className="sidebar-item-label">
          <span className="sidebar-item-label-inner">Inbox</span>
        </span>
        <span className="sidebar-badge" aria-label="3 unread">
          3
        </span>
      </a>
    </div>
  ),
};

export const Agents: StoryObj = {
  render: () => (
    <>
      <div className="sidebar-section is-team">
        <div className="sidebar-section-title">Agents</div>
      </div>
      <div className="sidebar-collapsible is-open">
        <div className="sidebar-agents">
          {[
            {
              slug: "atlas",
              role: "engineer",
              task: "writing migration plan",
              harness: "claude-code" as const,
              dot: "shipping",
            },
            {
              slug: "lina",
              role: "designer",
              task: "wireframing the inbox",
              harness: "codex" as const,
              dot: "plotting",
            },
            {
              slug: "sage",
              role: "writer",
              task: "drafting the FAQ",
              harness: "opencode" as const,
              dot: "active",
            },
            {
              slug: "ops",
              role: "ops",
              task: "watching CI",
              harness: "hermes-agent" as const,
              dot: "lurking",
            },
          ].map((a) => (
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
    </>
  ),
};

export const Channels: StoryObj = {
  render: () => (
    <>
      <div className="sidebar-section">
        <div className="sidebar-section-title">Channels</div>
      </div>
      <div className="sidebar-collapsible is-open">
        <div className="sidebar-channels">
          {[
            { slug: "architecture", unread: 0, active: true, n: 1 },
            { slug: "deploys", unread: 2, active: false, n: 2 },
            { slug: "wiki", unread: 0, active: false, n: 3 },
            { slug: "incidents", unread: 12, active: false, n: 4 },
          ].map((c) => (
            <a
              key={c.slug}
              className={`sidebar-item${c.active ? " active" : ""}`}
              href={`#${c.slug}`}
            >
              <span className="sidebar-item-icon">#</span>
              <span className="sidebar-item-label">
                <span className="sidebar-item-label-inner">{c.slug}</span>
              </span>
              {c.unread > 0 ? (
                <span className="sidebar-badge">{c.unread}</span>
              ) : (
                <kbd
                  className="kbd kbd-sm"
                  style={{ marginLeft: "auto", opacity: 0.5 }}
                >
                  ⌘{c.n}
                </kbd>
              )}
            </a>
          ))}
          <button type="button" className="sidebar-add-btn">
            <span>+</span> <span>New channel</span>
          </button>
        </div>
      </div>
    </>
  ),
};

export const Issues: StoryObj = {
  render: () => (
    <>
      <div className="sidebar-section">
        <div className="sidebar-section-title">Issues</div>
      </div>
      <div className="sidebar-collapsible is-open">
        <div className="sidebar-channels">
          <a className="sidebar-item" href="#issue-1">
            <span className="sidebar-item-icon">○</span>
            <span className="sidebar-item-label">
              <span className="sidebar-item-label-inner">
                Auth token rotation
              </span>
            </span>
            <span className="badge badge-yellow">blocked</span>
          </a>
          <a className="sidebar-item" href="#issue-2">
            <span className="sidebar-item-icon">○</span>
            <span className="sidebar-item-label">
              <span className="sidebar-item-label-inner">
                Calendar sync drift
              </span>
            </span>
            <span className="badge badge-orange">stuck</span>
          </a>
          <a className="sidebar-item" href="#issue-3">
            <span className="sidebar-item-icon">●</span>
            <span className="sidebar-item-label">
              <span className="sidebar-item-label-inner">
                Wiki ranking heuristic
              </span>
            </span>
          </a>
          <button type="button" className="sidebar-add-btn">
            <span>+</span> <span>New issue</span>
          </button>
        </div>
      </div>
    </>
  ),
};

export const Apps: StoryObj = {
  render: () => (
    <>
      <div className="sidebar-section">
        <div className="sidebar-section-title">Tools</div>
      </div>
      <div className="sidebar-collapsible is-open">
        <div className="sidebar-apps">
          {[
            { id: "overview", icon: "🏠", name: "Overview", active: false },
            { id: "wiki", icon: "📖", name: "Wiki", active: true },
            { id: "console", icon: ">", name: "Console", active: false },
            { id: "calendar", icon: "📅", name: "Calendar", active: false },
            { id: "skills", icon: "⚡", name: "Skills", active: false },
            { id: "settings", icon: "⚙", name: "Settings", active: false },
          ].map((app) => (
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
    </>
  ),
};

export const WorkspaceSummary: StoryObj = {
  name: "Workspace summary",
  render: () => (
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
      }}
    >
      <span>4 agents · 8 tasks</span>
      <span>14.2k tokens · $0.84</span>
    </div>
  ),
};

export const UsagePanel: StoryObj = {
  name: "Usage panel",
  render: () => (
    <div style={{ borderTop: "1px solid var(--border)" }}>
      <button type="button" className="usage-toggle open">
        <span>Usage · $0.84</span>
        <span style={{ opacity: 0.6 }}>▾</span>
      </button>
      <div style={{ padding: "var(--space-2) var(--space-4)" }}>
        <table
          className="usage-table"
          style={{
            width: "100%",
            fontSize: "var(--text-xs)",
            color: "var(--text-secondary)",
            borderCollapse: "collapse",
          }}
        >
          <thead>
            <tr>
              <th style={{ textAlign: "left", padding: "4px 0" }}>Agent</th>
              <th style={{ textAlign: "right", padding: "4px 0" }}>Tokens</th>
              <th style={{ textAlign: "right", padding: "4px 0" }}>Cost</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td>atlas</td>
              <td style={{ textAlign: "right" }}>6.2k</td>
              <td style={{ textAlign: "right" }}>$0.31</td>
            </tr>
            <tr>
              <td>lina</td>
              <td style={{ textAlign: "right" }}>4.8k</td>
              <td style={{ textAlign: "right" }}>$0.28</td>
            </tr>
            <tr>
              <td>sage</td>
              <td style={{ textAlign: "right" }}>3.2k</td>
              <td style={{ textAlign: "right" }}>$0.25</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  ),
};

export const ColorPicker: StoryObj = {
  name: "Color picker",
  render: () => (
    <div
      className="sidebar-color-picker"
      style={{ padding: "var(--space-3) var(--space-4)" }}
    >
      <div
        className="sidebar-color-picker-label"
        style={{
          fontSize: "var(--text-xs)",
          color: "var(--text-tertiary)",
          marginBottom: 6,
          textTransform: "uppercase",
          letterSpacing: "0.06em",
          fontWeight: 600,
        }}
      >
        Sidebar color
      </div>
      <div
        className="sidebar-color-picker-row"
        style={{ display: "flex", gap: 6 }}
      >
        {[
          { label: "Default", color: null },
          { label: "Noir", color: "#0d0d10" },
          { label: "Slate", color: "#1f2933" },
          { label: "Forest", color: "#16321f" },
          { label: "Burgundy", color: "#3a1620" },
          { label: "Indigo", color: "#1c1f3d" },
        ].map((p) => (
          <button
            key={p.label}
            type="button"
            title={p.label}
            aria-label={p.label}
            style={{
              width: 22,
              height: 22,
              borderRadius: "var(--radius-full)",
              background:
                p.color ?? "linear-gradient(135deg, #b6b6b6 50%, #fff 50%)",
              border: "1px solid var(--border)",
              cursor: "pointer",
              padding: 0,
            }}
          />
        ))}
      </div>
    </div>
  ),
};
