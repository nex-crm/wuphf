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
      <MockContent />
    </div>
  ),
};

export const Collapsed: StoryObj = {
  render: () => (
    <div style={{ display: "flex", minHeight: "100vh" }}>
      <CollapsedRail />
      <MockContent />
    </div>
  ),
};

export const Unread: StoryObj = {
  name: "Heavy unread state",
  render: () => (
    <div style={{ display: "flex", minHeight: "100vh" }}>
      <FullSidebar unread />
      <MockContent />
    </div>
  ),
};

function MockContent() {
  return (
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
        Switch themes from the toolbar to see the sidebar reskin via tokens.
      </p>
    </div>
  );
}

function FullSidebar({ unread = false }: { unread?: boolean }) {
  return (
    <aside
      className="sidebar"
      style={{
        width: 240,
        flexShrink: 0,
        display: "flex",
        flexDirection: "column",
      }}
    >
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

      <div className="sidebar-primary">
        <button type="button" className="sidebar-item">
          <MailIn className="sidebar-item-icon" />
          <span className="sidebar-item-label">
            <span className="sidebar-item-label-inner">Inbox</span>
          </span>
          {unread ? <span className="sidebar-badge">12</span> : null}
        </button>
      </div>

      <div className="sidebar-section is-team">
        <div className="sidebar-section-title">Agents</div>
      </div>
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
              <span style={{ width: 18, textAlign: "center", flexShrink: 0 }}>
                +
              </span>
              <span>New agent</span>
            </button>
          </div>
        </div>
      </div>

      <div className="sidebar-section">
        <div className="sidebar-section-title">Channels</div>
      </div>
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
                <span style={{ width: 18, textAlign: "center", flexShrink: 0 }}>
                  #
                </span>
                <span className="sidebar-item-label">
                  <span className="sidebar-item-label-inner">{c.slug}</span>
                </span>
                {unread && c.unread > 0 ? (
                  <span className="sidebar-badge">{c.unread}</span>
                ) : null}
                <span className="sidebar-shortcut" aria-hidden="true">
                  <Kbd size="sm">{`${MOD_KEY}${idx + 1}`}</Kbd>
                </span>
              </button>
            ))}
            <button type="button" className="sidebar-item sidebar-add-btn">
              <span style={{ width: 18, textAlign: "center", flexShrink: 0 }}>
                +
              </span>
              <span>New channel</span>
            </button>
          </div>
        </div>
      </div>

      <div className="sidebar-section-title-row issues-group-header">
        <button
          type="button"
          className="sidebar-section-title sidebar-section-toggle"
          aria-expanded
        >
          <span>Issues</span>
          <svg
            aria-hidden="true"
            style={{ width: 10, height: 10, transform: "rotate(90deg)" }}
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
            className="sidebar-item"
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

      <div className="sidebar-section">
        <div className="sidebar-section-title">Tools</div>
      </div>
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
                  <span className="sidebar-badge">{app.badge}</span>
                ) : null}
              </button>
            ))}
          </div>
        </div>
      </div>

      <div style={{ marginTop: "auto" }}>
        <div className="sidebar-summary">
          {AGENTS.length} agents active, 8 tasks open, 14.2k tokens
        </div>
        <div className="sidebar-hint">8 tasks in progress</div>
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
      </div>
      <div className="sidebar-rail-apps">
        {APPS.slice(0, 6).map((app) => (
          <button
            key={app.id}
            type="button"
            className={`sidebar-icon-btn${app.active ? " active" : ""}`}
            aria-label={app.name}
            title={app.name}
          >
            <app.Icon width={18} height={18} />
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
  { id: "1", title: "Auth token rotation" },
  { id: "2", title: "Calendar sync drift" },
];

const APPS = [
  { id: "wiki", name: "Wiki", Icon: BookStack, badge: 2, active: true },
  { id: "console", name: "Console", Icon: Terminal, active: false, badge: 0 },
  { id: "tasks", name: "Tasks", Icon: CheckCircle, active: false, badge: 0 },
  { id: "calendar", name: "Calendar", Icon: Calendar, active: false, badge: 0 },
  { id: "skills", name: "Skills", Icon: Flash, active: false, badge: 0 },
  { id: "graph", name: "Graph", Icon: ShareAndroid, active: false, badge: 0 },
  { id: "policies", name: "Policies", Icon: Shield, active: false, badge: 0 },
  { id: "receipts", name: "Receipts", Icon: Page, active: false, badge: 0 },
  { id: "health-check", name: "Access & Health", Icon: Search, active: false, badge: 0 },
];
