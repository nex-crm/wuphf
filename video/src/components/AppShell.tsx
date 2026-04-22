import React from "react";
import { colors, cyan, fonts, olive, starterAgents, tertiary } from "../theme";
import { PixelAvatar } from "./PixelAvatar";

interface AppShellProps {
  /** Which sidebar item is active — used to highlight the right row. */
  activeView:
    | "channel:general"
    | "channel:product"
    | "channel:gtm"
    | "app:wiki"
    | "app:tasks"
    | "app:reviews"
    | "dm:ceo"
    | "dm:gtm"
    | "dm:eng";
  /** When provided, the corresponding team member gets a pulsing active dot. */
  liveAgents?: string[];
  /** Main area (the actual scene content). */
  children: React.ReactNode;
  /** Optional top channel header text above the children. */
  headerTitle?: string;
  headerSub?: string;
  /** Optional extra edit-log footer overlay (rendered in the same layer). */
  footerSlot?: React.ReactNode;
  /** Bottom status bar labels on the right (e.g. "5 agents · claude-code · connected"). */
  statusRight?: React.ReactNode;
  /** Show the "all quiet" subheader strip above the main area. */
  allQuiet?: boolean;
}

const SIDEBAR_WIDTH = 260;
const APP_BAR_HEIGHT = 48;
const STATUS_BAR_HEIGHT = 28;

export const AppShell: React.FC<AppShellProps> = ({
  activeView,
  liveAgents = [],
  children,
  headerTitle,
  headerSub,
  footerSlot,
  statusRight,
  allQuiet = true,
}) => {
  const channels: Array<{ id: string; label: string }> = [
    { id: "channel:general", label: "general" },
    { id: "channel:product", label: "product" },
    { id: "channel:gtm", label: "gtm" },
  ];

  const apps: Array<{ id: string; label: string; badge?: string }> = [
    { id: "app:wiki", label: "Wiki", badge: "3" },
    { id: "app:tasks", label: "Tasks" },
    { id: "app:requests", label: "Requests" },
    { id: "app:policies", label: "Policies" },
    { id: "app:calendar", label: "Calendar" },
    { id: "app:skills", label: "Skills" },
    { id: "app:activity", label: "Activity" },
    { id: "app:receipts", label: "Receipts" },
  ];

  return (
    <div
      style={{
        position: "absolute",
        inset: 0,
        display: "flex",
        flexDirection: "column",
        backgroundColor: colors.bg,
        fontFamily: fonts.sans,
      }}
    >
      <div style={{ flex: 1, display: "flex", minHeight: 0 }}>
        {/* ─────────── Sidebar (purple chrome) ─────────── */}
        <div
          style={{
            width: SIDEBAR_WIDTH,
            background: `linear-gradient(180deg, ${colors.sidebar} 0%, ${colors.sidebarDeep} 100%)`,
            color: colors.sidebarText,
            display: "flex",
            flexDirection: "column",
            flexShrink: 0,
            fontSize: 14,
          }}
        >
          {/* Brand row */}
          <div
            style={{
              height: APP_BAR_HEIGHT,
              display: "flex",
              alignItems: "center",
              padding: "0 16px",
              gap: 12,
              borderBottom: `1px solid ${colors.sidebarBorder}`,
            }}
          >
            <div
              style={{
                color: colors.textBright,
                fontWeight: 800,
                letterSpacing: -0.3,
                fontSize: 18,
              }}
            >
              WUPHF
            </div>
            <div style={{ marginLeft: "auto", display: "flex", gap: 10, opacity: 0.7 }}>
              <SidebarIcon />
              <SidebarIcon kind="settings" />
            </div>
          </div>

          {/* TEAM */}
          <SidebarGroup label="Team">
            {starterAgents.map((a) => {
              const live = liveAgents.includes(a.slug);
              return (
                <SidebarRow key={a.slug} active={activeView === (`dm:${a.slug}` as typeof activeView)}>
                  <div
                    style={{
                      width: 22,
                      height: 22,
                      background: "rgba(255,255,255,0.05)",
                      borderRadius: 5,
                      display: "flex",
                      alignItems: "center",
                      justifyContent: "center",
                      overflow: "hidden",
                    }}
                  >
                    <PixelAvatar slug={a.slug} color={a.color} size={20} />
                  </div>
                  <span>{a.name}</span>
                  <span
                    style={{
                      marginLeft: "auto",
                      width: 7,
                      height: 7,
                      borderRadius: "50%",
                      background: live ? cyan[400] : "rgba(255,255,255,0.25)",
                      boxShadow: live ? `0 0 0 2px ${cyan[400]}40` : "none",
                    }}
                  />
                </SidebarRow>
              );
            })}
            <SidebarRow dim>
              <span style={{ opacity: 0.6 }}>+ New Agent</span>
            </SidebarRow>
          </SidebarGroup>

          {/* CHANNELS */}
          <SidebarGroup label="Channels">
            {channels.map((c) => (
              <SidebarRow
                key={c.id}
                active={activeView === (c.id as typeof activeView)}
                channel
              >
                <span style={{ opacity: 0.6, marginRight: 4 }}>#</span>
                <span>{c.label}</span>
              </SidebarRow>
            ))}
            <SidebarRow dim>
              <span style={{ opacity: 0.6 }}>+ New Channel</span>
            </SidebarRow>
          </SidebarGroup>

          {/* APPS */}
          <SidebarGroup label="Apps" flex>
            {apps.map((a) => {
              const isActive = activeView === (a.id as typeof activeView);
              return (
                <div
                  key={a.id}
                  style={{
                    display: "flex",
                    alignItems: "center",
                    gap: 10,
                    padding: "7px 14px",
                    margin: "1px 10px",
                    borderRadius: 6,
                    color: isActive ? colors.sidebarAppActiveFg : colors.sidebarText,
                    background: isActive ? colors.sidebarAppActive : "transparent",
                    fontWeight: isActive ? 600 : 400,
                  }}
                >
                  <AppIcon kind={a.id.replace("app:", "")} active={isActive} />
                  <span>{a.label}</span>
                  {a.badge && (
                    <span
                      style={{
                        marginLeft: "auto",
                        background: isActive ? "rgba(0,0,0,0.15)" : tertiary[400],
                        color: isActive ? colors.sidebarAppActiveFg : colors.textBright,
                        borderRadius: 999,
                        fontSize: 11,
                        fontWeight: 600,
                        padding: "1px 8px",
                      }}
                    >
                      {a.badge}
                    </span>
                  )}
                </div>
              );
            })}
          </SidebarGroup>

          {/* Bottom usage bar */}
          <div
            style={{
              borderTop: `1px solid ${colors.sidebarBorder}`,
              padding: "10px 16px 12px",
              fontSize: 12,
              color: colors.sidebarTextMuted,
            }}
          >
            <div>{liveAgents.length} agents active, 2 tasks open,</div>
            <div style={{ marginTop: 2 }}>2.4M tokens</div>
            <div style={{ marginTop: 6, fontSize: 11, opacity: 0.7 }}>
              Type <span style={{ fontFamily: fonts.mono }}>/</span> for commands
            </div>
          </div>
          <div
            style={{
              height: 28,
              borderTop: `1px solid ${colors.sidebarBorder}`,
              padding: "0 14px",
              display: "flex",
              alignItems: "center",
              fontSize: 11,
              color: colors.sidebarTextMuted,
            }}
          >
            <span>Usage</span>
            <span style={{ marginLeft: "auto", color: colors.sidebarText, fontFamily: fonts.mono }}>
              $2.0871
            </span>
          </div>
        </div>

        {/* ─────────── Main area ─────────── */}
        <div style={{ flex: 1, display: "flex", flexDirection: "column", minWidth: 0 }}>
          {/* Channel header */}
          <div
            style={{
              height: APP_BAR_HEIGHT,
              borderBottom: `1px solid ${colors.border}`,
              padding: "0 24px",
              display: "flex",
              alignItems: "center",
              gap: 14,
              flexShrink: 0,
              background: colors.bg,
            }}
          >
            {headerTitle ? (
              <>
                <div style={{ fontWeight: 700, color: colors.text, fontSize: 16 }}>
                  {headerTitle}
                </div>
                {headerSub && (
                  <div style={{ color: colors.textTertiary, fontSize: 13 }}>{headerSub}</div>
                )}
              </>
            ) : (
              <div style={{ color: colors.textTertiary, fontSize: 13 }}>&nbsp;</div>
            )}
          </div>

          {allQuiet && (
            <div
              style={{
                height: 26,
                padding: "0 24px",
                display: "flex",
                alignItems: "center",
                color: colors.textTertiary,
                fontSize: 12,
                borderBottom: `1px solid ${colors.borderLight}`,
                background: colors.bg,
                flexShrink: 0,
              }}
            >
              all quiet
            </div>
          )}

          {/* Content slot */}
          <div style={{ flex: 1, position: "relative", minHeight: 0, background: colors.bg }}>
            {children}
          </div>

          {/* Footer bar */}
          <div
            style={{
              height: STATUS_BAR_HEIGHT,
              borderTop: `1px solid ${colors.border}`,
              padding: "0 20px",
              display: "flex",
              alignItems: "center",
              fontFamily: fonts.mono,
              fontSize: 11,
              color: colors.textTertiary,
              flexShrink: 0,
              background: colors.bgWarm,
              gap: 18,
            }}
          >
            {footerSlot || <span>wiki &nbsp;·&nbsp; office</span>}
            <span style={{ marginLeft: "auto", display: "flex", gap: 14, alignItems: "center" }}>
              {statusRight || (
                <>
                  <span>5 agents</span>
                  <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
                    <span
                      style={{
                        width: 6,
                        height: 6,
                        borderRadius: "50%",
                        background: colors.green,
                      }}
                    />
                    claude-code
                  </span>
                  <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
                    <span
                      style={{
                        width: 6,
                        height: 6,
                        borderRadius: "50%",
                        background: colors.green,
                      }}
                    />
                    connected
                  </span>
                </>
              )}
            </span>
          </div>
        </div>
      </div>
    </div>
  );
};

// ─────────── Sidebar helpers ───────────

const SidebarGroup: React.FC<{ label: string; children: React.ReactNode; flex?: boolean }> = ({
  label,
  children,
  flex,
}) => (
  <div style={{ padding: "10px 0 6px", flex: flex ? 1 : undefined, overflow: "hidden" }}>
    <div
      style={{
        padding: "0 16px 6px",
        fontSize: 10,
        fontWeight: 700,
        letterSpacing: "0.08em",
        textTransform: "uppercase",
        color: olive[200],
        opacity: 0.7,
      }}
    >
      {label}
    </div>
    {children}
  </div>
);

const SidebarRow: React.FC<{
  children: React.ReactNode;
  active?: boolean;
  channel?: boolean;
  dim?: boolean;
}> = ({ children, active, channel, dim }) => (
  <div
    style={{
      display: "flex",
      alignItems: "center",
      gap: 10,
      padding: channel ? "5px 14px" : "6px 14px",
      margin: "1px 10px",
      borderRadius: 5,
      fontSize: 13,
      color: dim ? colors.sidebarTextMuted : colors.sidebarText,
      background: active
        ? channel
          ? colors.channelActiveBg
          : "rgba(255,255,255,0.08)"
        : "transparent",
      fontWeight: active ? 600 : 400,
    }}
  >
    {children}
  </div>
);

const SidebarIcon: React.FC<{ kind?: "settings" }> = ({ kind }) => (
  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
    {kind === "settings" ? (
      <>
        <circle cx="12" cy="12" r="3" />
        <path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3h.1a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5h.1a1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8v.1a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z" />
      </>
    ) : (
      <>
        <rect x="3" y="4" width="18" height="16" rx="2" />
        <path d="M9 4v16" />
      </>
    )}
  </svg>
);

const AppIcon: React.FC<{ kind: string; active: boolean }> = ({ kind, active }) => {
  const color = active ? colors.sidebarAppActiveFg : colors.sidebarText;
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2">
      {kind === "wiki" && (
        <>
          <rect x="4" y="3" width="16" height="18" rx="2" />
          <path d="M8 7h8M8 11h8M8 15h5" />
        </>
      )}
      {kind === "tasks" && (
        <>
          <circle cx="12" cy="12" r="9" />
          <path d="m9 12 2 2 4-4" />
        </>
      )}
      {kind === "requests" && (
        <>
          <rect x="4" y="4" width="16" height="16" rx="2" />
          <path d="M9 9h6M9 13h6M9 17h4" />
        </>
      )}
      {kind === "policies" && (
        <path d="M12 2 4 5v7c0 5 3.5 8.5 8 10 4.5-1.5 8-5 8-10V5l-8-3z" />
      )}
      {kind === "calendar" && (
        <>
          <rect x="3" y="5" width="18" height="16" rx="2" />
          <path d="M3 9h18M8 3v4M16 3v4" />
        </>
      )}
      {kind === "skills" && <path d="M13 2 3 14h8l-1 8 10-12h-8l1-8z" />}
      {kind === "activity" && <path d="M22 12h-4l-3 9L9 3l-3 9H2" />}
      {kind === "receipts" && (
        <>
          <path d="M5 3v18l3-2 3 2 3-2 3 2 3-2V3H5z" />
          <path d="M9 8h6M9 12h6M9 16h4" />
        </>
      )}
    </svg>
  );
};
