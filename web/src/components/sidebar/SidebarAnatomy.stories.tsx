import type { Meta, StoryObj } from "@storybook/react-vite";

const meta: Meta = {
  title: "Sidebar/Anatomy",
  parameters: { layout: "fullscreen" },
};

export default meta;

const PARTS: Array<{ label: string; desc: string; cls: string }> = [
  {
    label: "Header",
    desc: "WUPHF logo · workspace badge · collapse + settings icon buttons",
    cls: ".sidebar-header",
  },
  {
    label: "Primary action",
    desc: "Inbox button with unread badge — always first, always visible",
    cls: ".sidebar-primary",
  },
  {
    label: "Agents section",
    desc: "Collapsible group · per-row pixel avatar + harness badge + activity pill",
    cls: ".sidebar-section.is-team + .sidebar-collapsible",
  },
  {
    label: "Channels section",
    desc: "Collapsible group · # prefix + name + ⌘N kbd hint (slots 1–9) + unread badge",
    cls: ".sidebar-section + .sidebar-collapsible",
  },
  {
    label: "Issues section",
    desc: "Collapsible group · open issues with state · + New issue affordance",
    cls: ".sidebar-section + IssuesGroup body",
  },
  {
    label: "Apps section",
    desc: "Collapsible group · pinned tools (Wiki, Console, Calendar, Skills, …)",
    cls: ".sidebar-section + .sidebar-collapsible",
  },
  {
    label: "Recent objects",
    desc: "Auto-populated list of last-touched objects",
    cls: ".sidebar-recent",
  },
  {
    label: "Workspace summary",
    desc: "Bottom-anchored: active agents · open tasks · token usage",
    cls: ".workspace-summary",
  },
  {
    label: "Usage panel",
    desc: "Collapsible per-agent token + cost breakdown",
    cls: ".usage-toggle",
  },
  {
    label: "Color picker",
    desc: "Theme tinting for the sidebar background only — affects this surface, not the app",
    cls: ".sidebar-color-picker",
  },
];

export const Anatomy: StoryObj = {
  render: () => (
    <div
      style={{
        display: "grid",
        gridTemplateColumns: "260px 1fr",
        gap: 32,
        padding: 32,
        background: "var(--bg-warm)",
        minHeight: "100vh",
        color: "var(--text)",
      }}
    >
      <MockSidebar />
      <div style={{ maxWidth: 640 }}>
        <h1
          style={{
            margin: 0,
            marginBottom: 8,
            fontSize: 24,
            fontFamily: "Newsreader, serif",
            fontWeight: 600,
          }}
        >
          Sidebar anatomy
        </h1>
        <p
          style={{
            margin: 0,
            marginBottom: 24,
            color: "var(--text-secondary)",
            fontSize: 14,
            lineHeight: 1.6,
          }}
        >
          The sidebar is a stacked rail: a fixed header, then a primary
          action, then up to four collapsible content groups (Agents,
          Channels, Issues, Apps), then a bottom strip of metadata. Width
          is user-resizable between 180–420px via{" "}
          <code>--sidebar-resize-width</code>; the rail collapses to a
          48px icon strip via <code>.sidebar-collapsed</code>.
        </p>
        <ol
          style={{
            listStyle: "none",
            padding: 0,
            margin: 0,
            display: "flex",
            flexDirection: "column",
            gap: 12,
          }}
        >
          {PARTS.map((part, idx) => (
            <li
              key={part.label}
              style={{
                display: "grid",
                gridTemplateColumns: "32px 1fr",
                gap: 12,
                padding: 12,
                background: "var(--bg-card)",
                border: "1px solid var(--border)",
                borderRadius: "var(--radius-sm)",
              }}
            >
              <span
                style={{
                  width: 24,
                  height: 24,
                  borderRadius: "var(--radius-full)",
                  background: "var(--accent-bg)",
                  color: "var(--accent-warm)",
                  display: "inline-flex",
                  alignItems: "center",
                  justifyContent: "center",
                  fontSize: 12,
                  fontWeight: 600,
                }}
              >
                {idx + 1}
              </span>
              <div>
                <div style={{ fontWeight: 600, marginBottom: 2 }}>
                  {part.label}
                </div>
                <div
                  style={{
                    color: "var(--text-secondary)",
                    fontSize: 13,
                    marginBottom: 4,
                  }}
                >
                  {part.desc}
                </div>
                <code
                  style={{
                    fontFamily: "var(--font-mono)",
                    fontSize: 11,
                    color: "var(--text-tertiary)",
                  }}
                >
                  {part.cls}
                </code>
              </div>
            </li>
          ))}
        </ol>
      </div>
    </div>
  ),
};

function MockSidebar() {
  return (
    <aside
      className="sidebar"
      style={{
        position: "sticky",
        top: 24,
        height: "calc(100vh - 64px)",
        borderRadius: "var(--radius-md)",
        overflow: "hidden",
      }}
    >
      <NumberOverlay i={1} top={0} />
      <div className="sidebar-header">
        <span className="sidebar-logo">WUPHF</span>
      </div>
      <NumberOverlay i={2} top={56} />
      <div className="sidebar-primary" style={{ padding: "0 12px" }}>
        <div className="sidebar-item">
          <span className="sidebar-item-icon">📥</span>
          <span className="sidebar-item-label">
            <span className="sidebar-item-label-inner">Inbox</span>
          </span>
          <span className="sidebar-badge">3</span>
        </div>
      </div>
      <NumberOverlay i={3} top={104} />
      <div className="sidebar-section is-team">
        <div className="sidebar-section-title">Agents</div>
      </div>
      <NumberOverlay i={4} top={156} />
      <div className="sidebar-section">
        <div className="sidebar-section-title">Channels</div>
      </div>
      <NumberOverlay i={5} top={208} />
      <div className="sidebar-section">
        <div className="sidebar-section-title">Issues</div>
      </div>
      <NumberOverlay i={6} top={260} />
      <div className="sidebar-section">
        <div className="sidebar-section-title">Tools</div>
      </div>
    </aside>
  );
}

function NumberOverlay({ i, top }: { i: number; top: number }) {
  return (
    <div
      style={{
        position: "absolute",
        right: 8,
        top,
        width: 20,
        height: 20,
        borderRadius: "var(--radius-full)",
        background: "var(--accent)",
        color: "#fff",
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        fontSize: 11,
        fontWeight: 600,
        pointerEvents: "none",
      }}
    >
      {i}
    </div>
  );
}
