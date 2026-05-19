import {
  BookStack,
  Calendar,
  CheckCircle,
  Flash,
  MailIn,
  Page,
  Search,
  Settings,
  ShareAndroid,
  Shield,
  SidebarCollapse,
  Terminal,
} from "iconoir-react";

import type { Meta, StoryObj } from "@storybook/react-vite";

import { HarnessBadge } from "../ui/HarnessBadge";
import { Kbd, MOD_KEY } from "../ui/Kbd";
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
          title="Collapse sidebar"
        >
          <SidebarCollapse />
        </button>
        <button
          type="button"
          className="sidebar-icon-btn"
          aria-label="Open settings"
          title="Settings"
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
    <div className="sidebar-primary">
      <button type="button" className="sidebar-item active">
        <MailIn className="sidebar-item-icon" />
        <span className="sidebar-item-label">
          <span className="sidebar-item-label-inner">Inbox</span>
        </span>
        <span className="sidebar-badge" aria-label="3 unread">
          3
        </span>
      </button>
    </div>
  ),
};

export const Agents: StoryObj = {
  render: () => (
    <div className="sidebar-section is-team">
      <div className="sidebar-section-title">Agents</div>
      <div className="sidebar-collapsible is-open">
        <div className="sidebar-scroll-wrap is-agents">
          <div className="sidebar-agents">
            {AGENTS.map((a, i) => (
              <div key={a.slug} className="sidebar-agent-row">
                <button
                  type="button"
                  className={`sidebar-agent${i === 0 ? " active" : ""}`}
                  title={`${a.slug} — ${a.activityLabel}`}
                >
                  <span className="sidebar-agent-avatar avatar-with-harness">
                    <PixelAvatar
                      slug={a.slug}
                      size={24}
                      className="pixel-avatar-sidebar"
                    />
                    <HarnessBadge
                      kind={a.harness}
                      size={10}
                      className="harness-badge-on-avatar"
                    />
                    {a.online ? (
                      <span className="online-badge" aria-hidden="true" />
                    ) : null}
                  </span>
                  <div className="sidebar-agent-wrap">
                    <span className="sidebar-agent-name">{a.slug}</span>
                    <span
                      className="sidebar-agent-pill"
                      data-state={a.pillState}
                      title={a.activity}
                    >
                      {a.activity}
                    </span>
                  </div>
                  <span className={`status-dot ${a.dotClass}`} />
                </button>
                <button
                  type="button"
                  className="sidebar-agent-peek-trigger"
                  aria-label={`Recent activity for ${a.slug}`}
                >
                  <svg
                    width="8"
                    height="8"
                    viewBox="0 0 8 8"
                    aria-hidden="true"
                  >
                    <path
                      d="M2 1 L6 4 L2 7"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="1.5"
                      strokeLinecap="round"
                      strokeLinejoin="round"
                    />
                  </svg>
                </button>
              </div>
            ))}
            <button type="button" className="sidebar-item sidebar-add-btn">
              <span
                style={{ width: 18, textAlign: "center", flexShrink: 0 }}
              >
                +
              </span>
              <span>New agent</span>
            </button>
          </div>
        </div>
      </div>
    </div>
  ),
};

export const Channels: StoryObj = {
  render: () => (
    <div className="sidebar-section">
      <div className="sidebar-section-title">Channels</div>
      <div className="sidebar-collapsible is-open">
        <div className="sidebar-scroll-wrap is-channels">
          <div className="sidebar-channels">
            {CHANNELS.map((c, idx) => (
              <button
                key={c.slug}
                type="button"
                className={`sidebar-item${c.active ? " active" : ""}`}
                title={`${c.slug} — ${MOD_KEY}${idx + 1}`}
              >
                <span
                  style={{ width: 18, textAlign: "center", flexShrink: 0 }}
                >
                  #
                </span>
                <span className="sidebar-item-label">
                  <span className="sidebar-item-label-inner">{c.slug}</span>
                </span>
                {c.unread > 0 ? (
                  <span className="sidebar-badge">{c.unread}</span>
                ) : null}
                <span className="sidebar-shortcut" aria-hidden="true">
                  <Kbd size="sm">{`${MOD_KEY}${idx + 1}`}</Kbd>
                </span>
              </button>
            ))}
            <button type="button" className="sidebar-item sidebar-add-btn">
              <span
                style={{ width: 18, textAlign: "center", flexShrink: 0 }}
              >
                +
              </span>
              <span>New channel</span>
            </button>
          </div>
        </div>
      </div>
    </div>
  ),
};

export const Issues: StoryObj = {
  render: () => (
    <>
      <div className="sidebar-section-title-row issues-group-header">
        <button
          type="button"
          className="sidebar-section-title sidebar-section-toggle"
          aria-expanded
        >
          <span>Issues</span>
          <svg
            aria-hidden="true"
            style={{
              width: 10,
              height: 10,
              transform: "rotate(90deg)",
            }}
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <path d="m9 18 6-6-6-6" />
          </svg>
        </button>
        <button
          type="button"
          className="sidebar-icon-btn issues-new-icon-btn"
          title="New issue"
          aria-label="New issue"
        >
          +
        </button>
      </div>
      <div className="sidebar-collapsible is-open is-issues">
        {ISSUES.map((issue) => (
          <button
            key={issue.id}
            type="button"
            className={`sidebar-item${issue.active ? " active" : ""}`}
            title={issue.title}
          >
            <span
              style={{
                width: 18,
                textAlign: "center",
                flexShrink: 0,
                fontSize: 11,
              }}
            >
              #
            </span>
            <span className="sidebar-item-label">
              <span className="sidebar-item-label-inner">{issue.title}</span>
            </span>
          </button>
        ))}
        <button type="button" className="sidebar-item sidebar-add-btn">
          <span
            style={{
              width: 18,
              textAlign: "center",
              flexShrink: 0,
              display: "inline-block",
            }}
          />
          <span style={{ color: "var(--text-tertiary)" }}>View all</span>
        </button>
      </div>
    </>
  ),
};

export const Apps: StoryObj = {
  render: () => (
    <div className="sidebar-section">
      <div className="sidebar-section-title">Tools</div>
      <div className="sidebar-collapsible is-open">
        <div className="sidebar-scroll-wrap is-apps">
          <div className="sidebar-apps">
            {APPS.map((app) => (
              <button
                key={app.id}
                type="button"
                className={`sidebar-item${app.active ? " active" : ""}`}
              >
                <app.Icon className="sidebar-item-icon" />
                <span style={{ flex: 1 }}>{app.name}</span>
                {app.badge ? (
                  <span
                    className="sidebar-badge"
                    aria-label={`${app.badge} pending`}
                  >
                    {app.badge}
                  </span>
                ) : null}
              </button>
            ))}
          </div>
        </div>
      </div>
    </div>
  ),
};

export const WorkspaceSummary: StoryObj = {
  name: "Workspace summary",
  render: () => (
    <div style={{ marginTop: "auto" }}>
      <div className="sidebar-summary">
        4 agents active, 8 tasks open, 14.2k tokens
      </div>
      <div className="sidebar-hint">8 tasks in progress</div>
    </div>
  ),
};

export const UsagePanel: StoryObj = {
  name: "Usage panel",
  render: () => (
    <div style={{ marginTop: "auto" }}>
      <button type="button" className="usage-toggle open">
        <svg
          aria-hidden="true"
          width="10"
          height="10"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          style={{ transform: "rotate(90deg)" }}
        >
          <path d="m9 18 6-6-6-6" />
        </svg>
        <span>Usage</span>
        <span style={{ marginLeft: "auto" }}>$0.84</span>
      </button>
      <div className="usage-table-wrap" style={{ padding: "0 12px 12px" }}>
        <table className="usage-table">
          <thead>
            <tr>
              <th>Agent</th>
              <th>Tokens</th>
              <th>Cost</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td>atlas</td>
              <td>6.2k</td>
              <td>$0.31</td>
            </tr>
            <tr>
              <td>lina</td>
              <td>4.8k</td>
              <td>$0.28</td>
            </tr>
            <tr>
              <td>sage</td>
              <td>3.2k</td>
              <td>$0.25</td>
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
    <div className="sidebar-color-picker" style={{ marginTop: "auto" }}>
      <div className="sidebar-color-picker-label">Sidebar color</div>
      <div className="sidebar-color-picker-row">
        {COLOR_PRESETS.map((p) => (
          <button
            key={p.label}
            type="button"
            title={p.label}
            aria-label={p.label}
            className={`sidebar-color-swatch${p.label === "Default" ? " is-active" : ""}`}
            style={{
              background:
                p.value ?? "linear-gradient(135deg, #b6b6b6 50%, #fff 50%)",
            }}
          />
        ))}
      </div>
    </div>
  ),
};

const AGENTS: Array<{
  slug: string;
  activity: string;
  activityLabel: string;
  harness:
    | "claude-code"
    | "codex"
    | "opencode"
    | "openclaw"
    | "hermes-agent";
  online: boolean;
  pillState: "halo" | "holding" | "dim" | "idle" | "stuck";
  dotClass: string;
}> = [
  {
    slug: "atlas",
    activity: "writing migration plan",
    activityLabel: "shipping",
    harness: "claude-code",
    online: true,
    pillState: "halo",
    dotClass: "shipping",
  },
  {
    slug: "lina",
    activity: "wireframing the inbox",
    activityLabel: "plotting",
    harness: "codex",
    online: true,
    pillState: "holding",
    dotClass: "plotting",
  },
  {
    slug: "sage",
    activity: "drafting the FAQ",
    activityLabel: "talking",
    harness: "opencode",
    online: true,
    pillState: "halo",
    dotClass: "active pulse",
  },
  {
    slug: "ops",
    activity: "watching CI",
    activityLabel: "lurking",
    harness: "hermes-agent",
    online: false,
    pillState: "idle",
    dotClass: "lurking",
  },
];

const CHANNELS = [
  { slug: "architecture", unread: 0, active: true },
  { slug: "deploys", unread: 2, active: false },
  { slug: "wiki", unread: 0, active: false },
  { slug: "incidents", unread: 12, active: false },
];

const ISSUES = [
  { id: "1", title: "Auth token rotation", active: false },
  { id: "2", title: "Calendar sync drift", active: true },
  { id: "3", title: "Wiki ranking heuristic", active: false },
];

const APPS = [
  { id: "wiki", name: "Wiki", Icon: BookStack, badge: 2, active: true },
  { id: "console", name: "Console", Icon: Terminal, active: false },
  { id: "tasks", name: "Tasks", Icon: CheckCircle, active: false },
  { id: "calendar", name: "Calendar", Icon: Calendar, active: false },
  { id: "skills", name: "Skills", Icon: Flash, active: false },
  { id: "graph", name: "Graph", Icon: ShareAndroid, active: false },
  { id: "policies", name: "Policies", Icon: Shield, active: false },
  { id: "receipts", name: "Receipts", Icon: Page, active: false },
  { id: "health-check", name: "Access & Health", Icon: Search, active: false },
];

const COLOR_PRESETS = [
  { label: "Default", value: null },
  { label: "Noir", value: "#0d0d10" },
  { label: "Slate", value: "#1f2933" },
  { label: "Forest", value: "#16321f" },
  { label: "Burgundy", value: "#3a1620" },
  { label: "Indigo", value: "#1c1f3d" },
];
