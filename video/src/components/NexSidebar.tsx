import React from "react";
import { useCurrentFrame } from "remotion";
import { colors, cyan, fonts, starterAgents } from "../theme";
import { PixelAvatar } from "./PixelAvatar";

// The WUPHF app's purple nex-themed sidebar. Mirrors the real
// web/src/components/layout/Sidebar.tsx + nex.css.

const NEX = {
  sidebarBg: "#612a92",
  sidebarText: "rgba(255,255,255,0.72)",
  sidebarTextMuted: "rgba(255,255,255,0.55)",
  sidebarTitle: "rgba(255,255,255,0.55)",
  sidebarBorder: "rgba(255,255,255,0.08)",
  activeBg: cyan[400],
  activeFg: "#0b3a44",
  presence: "#35da79",
};

type Active =
  | { kind: "channel"; slug: string }
  | { kind: "dm"; slug: string }
  | { kind: "app"; slug: string };

interface NexSidebarProps {
  active: Active;
  /** Optional per-agent activity labels (shown under the agent name). */
  agentTasks?: Record<string, string>;
  /** Bottom summary line (defaults to `5 agents active · 2 tasks open`). */
  summary?: string;
  /** Right-aligned usage total (defaults to `$2.0871`). */
  usage?: string;
  /** Render the narrow collapsed rail (icons only). */
  collapsed?: boolean;
}

export const NexSidebar: React.FC<NexSidebarProps> = ({
  active,
  agentTasks,
  summary = "5 agents active · 2 tasks open",
  usage = "$2.0871",
  collapsed = false,
}) => {
  const frame = useCurrentFrame();

  if (collapsed) {
    return <CollapsedRail active={active} />;
  }

  return (
    <aside
      style={{
        width: 260,
        flexShrink: 0,
        background: NEX.sidebarBg,
        borderRight: "1px solid rgba(0,0,0,0.12)",
        display: "flex",
        flexDirection: "column",
        fontFamily: fonts.sans,
        overflow: "hidden",
      }}
    >
      {/* Header */}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          height: 56,
          padding: "0 14px",
          borderBottom: `1px solid ${NEX.sidebarBorder}`,
          flexShrink: 0,
        }}
      >
        <span style={{ fontSize: 17, fontWeight: 800, color: "#FFFFFF", letterSpacing: "-0.02em" }}>
          WUPHF
        </span>
        <div style={{ display: "flex", gap: 6 }}>
          <div style={iconBtnStyle}>
            <IconSidebarCollapse />
          </div>
          <div style={iconBtnStyle}>
            <IconSettings />
          </div>
        </div>
      </div>

      {/* Team */}
      <SectionTitle label="Team" withChevron expanded />
      <div style={{ padding: "4px 12px 16px", borderBottom: `1px solid ${NEX.sidebarBorder}` }}>
        {starterAgents.map((agent, i) => {
          const dotPulse = Math.sin(frame * 0.12 + i * 2) * 0.2 + 0.8;
          const task = agentTasks?.[agent.slug];
          const isActive = active.kind === "dm" && active.slug === agent.slug;
          return (
            <div
              key={agent.slug}
              style={{
                display: "flex",
                alignItems: "center",
                gap: 10,
                padding: "6px 10px",
                marginBottom: 1,
                borderRadius: 6,
                color: isActive ? NEX.activeFg : NEX.sidebarText,
                background: isActive ? NEX.activeBg : "transparent",
                fontWeight: isActive ? 600 : 400,
              }}
            >
              <span style={{ width: 24, height: 24, display: "inline-flex", flexShrink: 0 }}>
                <PixelAvatar slug={agent.slug} color={agent.color} size={24} />
              </span>
              <div style={{ display: "flex", flexDirection: "column", flex: 1, minWidth: 0 }}>
                <span
                  style={{
                    fontSize: 13,
                    color: "inherit",
                    overflow: "hidden",
                    textOverflow: "ellipsis",
                    whiteSpace: "nowrap",
                  }}
                >
                  {agent.name}
                </span>
                {task && (
                  <span
                    style={{
                      fontSize: 11,
                      color: isActive ? "rgba(11,58,68,0.6)" : "rgba(255,255,255,0.45)",
                      overflow: "hidden",
                      textOverflow: "ellipsis",
                      whiteSpace: "nowrap",
                    }}
                  >
                    {task}
                  </span>
                )}
              </div>
              <div
                style={{
                  width: 6,
                  height: 6,
                  borderRadius: "50%",
                  backgroundColor: NEX.presence,
                  opacity: dotPulse,
                  flexShrink: 0,
                }}
              />
            </div>
          );
        })}
        <AddRow label="New Agent" />
      </div>

      {/* Channels */}
      <SectionTitle label="Channels" />
      <div style={{ padding: "4px 12px 16px" }}>
        {[
          { label: "general" },
          { label: "product" },
          { label: "gtm" },
        ].map((c) => {
          const isActive = active.kind === "channel" && active.slug === c.label;
          return (
            <div
              key={c.label}
              style={{
                display: "flex",
                alignItems: "center",
                gap: 10,
                padding: "6px 10px",
                marginBottom: 1,
                borderRadius: 6,
                fontSize: 13,
                color: isActive ? NEX.activeFg : NEX.sidebarText,
                background: isActive ? NEX.activeBg : "transparent",
                fontWeight: isActive ? 600 : 400,
              }}
            >
              <span style={{ width: 18, textAlign: "center", flexShrink: 0, color: "currentColor" }}>#</span>
              <span style={{ flex: 1, minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                {c.label}
              </span>
            </div>
          );
        })}
        <AddRow label="New Channel" />
      </div>

      {/* Apps */}
      <SectionTitle label="Apps" borderTop />
      <div style={{ padding: "4px 12px 16px", flex: 1, overflow: "hidden" }}>
        {[
          { slug: "wiki", label: "Wiki", icon: <IconBookStack />, badge: "3" },
          { slug: "tasks", label: "Tasks", icon: <IconCheckCircle /> },
          { slug: "requests", label: "Requests", icon: <IconClipboardCheck /> },
          { slug: "policies", label: "Policies", icon: <IconShield /> },
          { slug: "calendar", label: "Calendar", icon: <IconCalendar /> },
          { slug: "skills", label: "Skills", icon: <IconFlash /> },
          { slug: "activity", label: "Activity", icon: <IconPackage /> },
          { slug: "receipts", label: "Receipts", icon: <IconPage /> },
        ].map((a) => {
          const isActive = active.kind === "app" && active.slug === a.slug;
          return (
            <div
              key={a.slug}
              style={{
                display: "flex",
                alignItems: "center",
                gap: 10,
                padding: "6px 10px",
                marginBottom: 1,
                borderRadius: 6,
                fontSize: 13,
                color: isActive ? NEX.activeFg : NEX.sidebarText,
                background: isActive ? NEX.activeBg : "transparent",
                fontWeight: isActive ? 600 : 400,
              }}
            >
              <span
                style={{
                  width: 16,
                  height: 16,
                  flexShrink: 0,
                  display: "inline-flex",
                  alignItems: "center",
                  justifyContent: "center",
                  color: "currentColor",
                }}
              >
                {a.icon}
              </span>
              <span style={{ flex: 1, minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                {a.label}
              </span>
              {a.badge && (
                <span
                  style={{
                    minWidth: 18,
                    height: 18,
                    padding: "0 6px",
                    display: "inline-flex",
                    alignItems: "center",
                    justifyContent: "center",
                    borderRadius: 9,
                    background: colors.accent,
                    color: "#FFFFFF",
                    fontSize: 10,
                    fontWeight: 600,
                  }}
                >
                  {a.badge}
                </span>
              )}
            </div>
          );
        })}
      </div>

      {/* Workspace summary + usage */}
      <div
        style={{
          padding: "6px 20px 2px",
          borderTop: `1px solid ${NEX.sidebarBorder}`,
          fontSize: 11,
          color: NEX.sidebarTextMuted,
          flexShrink: 0,
        }}
      >
        {summary}
      </div>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 6,
          height: 36,
          padding: "0 16px",
          fontSize: 11,
          color: "rgba(255,255,255,0.78)",
          flexShrink: 0,
        }}
      >
        <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <path d="m9 18 6-6-6-6" />
        </svg>
        <span>Usage</span>
        <span style={{ marginLeft: "auto", fontFamily: fonts.mono, color: "#FFFFFF" }}>{usage}</span>
      </div>
    </aside>
  );
};

// ── Helpers ──

const iconBtnStyle: React.CSSProperties = {
  width: 30,
  height: 30,
  borderRadius: 6,
  color: "rgba(255,255,255,0.78)",
  display: "inline-flex",
  alignItems: "center",
  justifyContent: "center",
};

const SectionTitle: React.FC<{ label: string; withChevron?: boolean; expanded?: boolean; borderTop?: boolean }> = ({
  label,
  withChevron,
  expanded,
  borderTop,
}) => (
  <div style={{ padding: "14px 12px 2px", borderTop: borderTop ? `1px solid ${NEX.sidebarBorder}` : undefined }}>
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: 6,
        padding: "4px 10px",
        fontSize: 11,
        fontWeight: 600,
        textTransform: "uppercase",
        letterSpacing: "0.06em",
        color: NEX.sidebarTitle,
        fontFamily: fonts.sans,
      }}
    >
      <span>{label}</span>
      {withChevron && (
        <svg
          width="10"
          height="10"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          style={{ transform: expanded ? "rotate(90deg)" : "rotate(0deg)" }}
        >
          <path d="m9 18 6-6-6-6" />
        </svg>
      )}
    </div>
  </div>
);

const AddRow: React.FC<{ label: string }> = ({ label }) => (
  <div
    style={{
      display: "flex",
      alignItems: "center",
      gap: 10,
      padding: "6px 10px",
      fontSize: 12,
      color: NEX.sidebarTextMuted,
    }}
  >
    <span style={{ width: 18, textAlign: "center", flexShrink: 0 }}>+</span>
    <span>{label}</span>
  </div>
);

// Inline iconoir-matched SVGs — stroke 1.8, 24 viewBox, currentColor
const iconProps = {
  width: 16,
  height: 16,
  viewBox: "0 0 24 24",
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 1.8,
  strokeLinecap: "round" as const,
  strokeLinejoin: "round" as const,
};

const IconSidebarCollapse = () => (
  <svg {...iconProps}>
    <rect x="3" y="4" width="18" height="16" rx="2" />
    <path d="M9 4v16" />
    <path d="m15 9-2 3 2 3" />
  </svg>
);
const IconSettings = () => (
  <svg {...iconProps}>
    <circle cx="12" cy="12" r="3" />
    <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.6 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.6 1.65 1.65 0 0 0 10 3.09V3a2 2 0 1 1 4 0v.09A1.65 1.65 0 0 0 15 4.6a1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9c.14.35.25.7.25 1.09V10a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
  </svg>
);
const IconBookStack = () => (
  <svg {...iconProps}>
    <path d="M4 19V6a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v13" />
    <path d="M4 19a2 2 0 0 0 2 2h14" />
    <path d="M4 19a2 2 0 0 1 2-2h14" />
    <path d="M8 8h8M8 12h5" />
  </svg>
);
const IconCheckCircle = () => (
  <svg {...iconProps}>
    <circle cx="12" cy="12" r="9" />
    <path d="m8.5 12 2.5 2.5 4.5-5" />
  </svg>
);
const IconClipboardCheck = () => (
  <svg {...iconProps}>
    <rect x="6" y="4" width="12" height="17" rx="2" />
    <path d="M9 4V3a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v1" />
    <path d="m9 13 2 2 4-4" />
  </svg>
);
const IconShield = () => (
  <svg {...iconProps}>
    <path d="M12 3 4 6v6c0 4.5 3.2 8.5 8 9.5 4.8-1 8-5 8-9.5V6l-8-3Z" />
  </svg>
);
const IconCalendar = () => (
  <svg {...iconProps}>
    <rect x="3" y="5" width="18" height="16" rx="2" />
    <path d="M3 9h18" />
    <path d="M8 3v4M16 3v4" />
  </svg>
);
const IconFlash = () => (
  <svg {...iconProps}>
    <path d="M13 3 4 14h7l-1 7 9-11h-7l1-7Z" />
  </svg>
);
const IconPackage = () => (
  <svg {...iconProps}>
    <path d="M3 7.5 12 3l9 4.5v9L12 21l-9-4.5v-9Z" />
    <path d="m3 7.5 9 4.5 9-4.5" />
    <path d="M12 12v9" />
  </svg>
);
const IconPage = () => (
  <svg {...iconProps}>
    <path d="M6 3h8l5 5v11a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2Z" />
    <path d="M14 3v5h5" />
    <path d="M8 13h8M8 17h5" />
  </svg>
);

// ── Collapsed rail (matches .sidebar-collapsed in layout.css) ──

const CollapsedRail: React.FC<{ active: Active }> = ({ active }) => {
  const appSlug = active.kind === "app" ? active.slug : null;
  const railBtn = (child: React.ReactNode, activeIcon = false): React.CSSProperties => ({
    width: 36,
    height: 36,
    borderRadius: 6,
    display: "inline-flex",
    alignItems: "center",
    justifyContent: "center",
    color: activeIcon ? "#0b3a44" : "rgba(255,255,255,0.78)",
    background: activeIcon ? NEX.activeBg : "transparent",
  }) as React.CSSProperties & { children?: React.ReactNode };
  void railBtn; // keep the helper inline below

  const apps: Array<{ slug: string; icon: React.ReactNode }> = [
    { slug: "wiki", icon: <IconBookStack /> },
    { slug: "tasks", icon: <IconCheckCircle /> },
    { slug: "requests", icon: <IconClipboardCheck /> },
    { slug: "policies", icon: <IconShield /> },
    { slug: "calendar", icon: <IconCalendar /> },
    { slug: "skills", icon: <IconFlash /> },
    { slug: "activity", icon: <IconPackage /> },
    { slug: "receipts", icon: <IconPage /> },
  ];

  return (
    <aside
      style={{
        width: 56,
        flexShrink: 0,
        background: NEX.sidebarBg,
        borderRight: "1px solid rgba(0,0,0,0.12)",
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        padding: "10px 0",
        gap: 4,
        overflow: "hidden",
      }}
    >
      <div style={{
        display: "flex", flexDirection: "column", gap: 2, alignItems: "center",
        paddingBottom: 8, marginBottom: 8,
        borderBottom: "1px solid rgba(255,255,255,0.05)",
        width: "calc(100% - 20px)",
      }}>
        <div style={{
          width: 36, height: 36, borderRadius: 6,
          display: "flex", alignItems: "center", justifyContent: "center",
          color: "rgba(255,255,255,0.78)",
        }}><IconSidebarCollapse /></div>
        <div style={{
          width: 36, height: 36, borderRadius: 6,
          display: "flex", alignItems: "center", justifyContent: "center",
          color: "rgba(255,255,255,0.78)",
        }}><IconSettings /></div>
      </div>

      <div style={{ flex: 1, display: "flex", flexDirection: "column", gap: 2, alignItems: "center" }}>
        {apps.map((a) => {
          const isActive = appSlug === a.slug;
          return (
            <div
              key={a.slug}
              style={{
                width: 36, height: 36, borderRadius: 6,
                display: "inline-flex", alignItems: "center", justifyContent: "center",
                color: isActive ? "#0b3a44" : "rgba(255,255,255,0.78)",
                background: isActive ? NEX.activeBg : "transparent",
              }}
            >
              {a.icon}
            </div>
          );
        })}
      </div>

      <div style={{
        fontFamily: fonts.mono,
        fontSize: 10,
        color: "#FFFFFF",
        padding: "6px 0",
      }}>
        $2.09
      </div>
    </aside>
  );
};
