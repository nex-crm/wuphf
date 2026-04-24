import { AbsoluteFill, useCurrentFrame, interpolate, Easing } from "remotion";
import { colors, cyan, fonts, sec, starterAgents } from "../theme";
import { ChatMessage } from "../components/ChatMessage";
import { PixelAvatar } from "../components/PixelAvatar";

// Sized for 1920×1080 but faithful to web/src/components/layout/Sidebar.tsx
// + ChannelHeader.tsx + Composer.tsx + StatusBar.tsx under the nex theme
// (web/public/themes/nex.css). Purple sidebar, cyan active-channel pill,
// blurred channel header, rounded composer with cyan send, mono status bar.

const NEX = {
  // Chrome
  sidebarBg: "#612a92",           // --nex-sidebar = tertiary-500
  sidebarText: "rgba(255,255,255,0.72)",       // --nex-sidebar-text
  sidebarTextMuted: "rgba(255,255,255,0.55)",
  sidebarTitle: "rgba(255,255,255,0.55)",      // section titles in purple sidebar
  sidebarHover: "rgba(255,255,255,0.16)",
  sidebarBorder: "rgba(255,255,255,0.08)",
  activeBg: cyan[400],                          // --cyan-400 = #00ccff
  activeFg: "#0b3a44",                          // dark text on cyan pill
  presence: "#35da79",                          // --success-300
  // Main
  bg: "#FFFFFF",
  bgCard: "#FFFFFF",
  border: "#e9eaeb",                            // --neutral-100
  borderLight: "#f2f2f3",                       // --neutral-50
  text: "#28292a",
  textSecondary: "#686c6e",
  textTertiary: "#85898b",
};

export const Scene4TheyWork: React.FC = () => {
  const frame = useCurrentFrame();

  const uiOpacity = interpolate(frame, [0, 12], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  // Window slides up from +80px to its target position
  const slideY = interpolate(frame, [0, 18], [80, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.out(Easing.cubic),
  });

  // Fall-out at the end — accelerates with gravity, fades out near the end
  const SCENE_DURATION = sec(11);
  const EXIT_START = SCENE_DURATION - 28;
  const fallY = interpolate(frame, [EXIT_START, SCENE_DURATION], [0, 420], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.in(Easing.cubic),
  });
  const fallRot = interpolate(frame, [EXIT_START, SCENE_DURATION], [0, -5], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.in(Easing.cubic),
  });
  const exitFade = interpolate(
    frame,
    [EXIT_START + 10, SCENE_DURATION],
    [1, 0],
    { extrapolateLeft: "clamp", extrapolateRight: "clamp", easing: Easing.in(Easing.cubic) },
  );

  // Slow Ken-Burns zoom across the full scene length (1.4 → 1.5)
  const slowZoom = interpolate(frame, [0, SCENE_DURATION], [1.4, 1.6], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  const tasks = [
    "delegating to team",
    "writing hero copy",
    "scaffolding page",
    "reviewing brief",
    "drafting visuals",
  ];

  return (
    <AbsoluteFill style={{
      background: "radial-gradient(ellipse at top, #f3e8ff 0%, #ede2f7 40%, #d9c6ea 100%)",
      display: "flex",
      alignItems: "center",
      justifyContent: "center",
    }}>
      {/* Window shell — modern mockup look: rounded corners, drop shadow, traffic-light bar */}
      <div style={{
        width: 1440,
        height: 880,
        opacity: uiOpacity * exitFade,
        transform: `scale(${slowZoom}) translateY(${slideY + fallY}px) rotateZ(${fallRot}deg)`,
        transformOrigin: "top left",
        willChange: "transform",
        backfaceVisibility: "hidden",
        background: "#ebe5f0",
        borderRadius: 20,
        padding: 4,
        overflow: "hidden",
        boxShadow: "0 0 0 1px rgba(0,0,0,0.05), 0 40px 100px rgba(66, 26, 104, 0.35), 0 12px 32px rgba(0,0,0,0.12)",
        display: "flex",
        flexDirection: "column",
      }}>
        {/* Inner UI */}
        <div style={{
          flex: 1,
          display: "flex",
          flexDirection: "column",
          minHeight: 0,
        }}>
        {/* Titlebar */}
        <div style={{
          display: "flex",
          alignItems: "center",
          gap: 10,
          height: 40,
          padding: "0 14px",
          background: "#ebe5f0",
          borderBottom: "1px solid rgba(0,0,0,0.06)",
          flexShrink: 0,
        }}>
          <div style={{ display: "flex", gap: 8 }}>
            <span style={trafficDot("#ff5f57")} />
            <span style={trafficDot("#febc2e")} />
            <span style={trafficDot("#28c840")} />
          </div>
          <span style={{
            flex: 1,
            textAlign: "center",
            fontFamily: fonts.sans,
            fontSize: 12,
            color: "#686c6e",
            letterSpacing: "0.01em",
          }}>
            wuphf.app — # general
          </span>
          <span style={{ width: 54 }} />
        </div>

        <div style={{
          display: "flex",
          flex: 1,
          minHeight: 0,
          borderRadius: 16,
          overflow: "hidden",
        }}>
        {/* ═════ SIDEBAR ═════ */}
        <aside style={{
          width: 260,
          flexShrink: 0,
          background: NEX.sidebarBg,
          borderRight: "1px solid rgba(0,0,0,0.12)",
          display: "flex",
          flexDirection: "column",
          fontFamily: fonts.sans,
          overflow: "hidden",
        }}>
          {/* Sticky header: logo + settings */}
          <div style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            height: 56,
            padding: "0 14px",
            borderBottom: `1px solid ${NEX.sidebarBorder}`,
            flexShrink: 0,
          }}>
            <span style={{
              fontSize: 17,
              fontWeight: 800,
              color: "#FFFFFF",
              letterSpacing: "-0.02em",
            }}>WUPHF</span>
            <div style={{ display: "flex", gap: 6 }}>
              <div style={iconBtnStyle}><IconSidebarCollapse /></div>
              <div style={iconBtnStyle}><IconSettings /></div>
            </div>
          </div>

          {/* Team section — collapsible with chevron */}
          <SectionTitle label="Team" withChevron expanded />
          <div style={{ padding: "4px 12px 16px", borderBottom: `1px solid ${NEX.sidebarBorder}` }}>
            {starterAgents.map((agent, i) => {
              const dotPulse = Math.sin(frame * 0.12 + i * 2) * 0.2 + 0.8;
              const task = tasks[i];
              return (
                <div key={agent.slug} style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 10,
                  width: "100%",
                  padding: "6px 10px",
                  marginBottom: 1,
                  borderRadius: 6,
                  color: NEX.sidebarText,
                }}>
                  <span style={{ width: 24, height: 24, display: "inline-flex", alignItems: "center", justifyContent: "center", flexShrink: 0 }}>
                    <PixelAvatar slug={agent.slug} color={agent.color} size={24} />
                  </span>
                  <div style={{ display: "flex", flexDirection: "column", flex: 1, minWidth: 0 }}>
                    <span style={{
                      fontSize: 13,
                      color: "inherit",
                      overflow: "hidden",
                      textOverflow: "ellipsis",
                      whiteSpace: "nowrap",
                    }}>{agent.name}</span>
                    {task && (
                      <span style={{
                        fontSize: 11,
                        color: "rgba(255,255,255,0.45)",
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                        whiteSpace: "nowrap",
                      }}>{task}</span>
                    )}
                  </div>
                  <div style={{
                    width: 6,
                    height: 6,
                    borderRadius: "50%",
                    backgroundColor: NEX.presence,
                    opacity: dotPulse,
                    flexShrink: 0,
                    marginLeft: "auto",
                  }} />
                </div>
              );
            })}
            <AddRow label="New Agent" />
          </div>

          {/* Channels */}
          <SectionTitle label="Channels" />
          <div style={{ padding: "4px 12px 16px" }}>
            {[
              { label: "general", active: true },
              { label: "product", active: false },
              { label: "gtm", active: false },
            ].map((c) => (
              <div key={c.label} style={{
                display: "flex",
                alignItems: "center",
                gap: 10,
                width: "100%",
                padding: "6px 10px",
                marginBottom: 1,
                borderRadius: 6,
                fontSize: 13,
                color: c.active ? NEX.activeFg : NEX.sidebarText,
                background: c.active ? NEX.activeBg : "transparent",
                fontWeight: c.active ? 600 : 400,
              }}>
                <span style={{ width: 18, textAlign: "center", flexShrink: 0, color: "currentColor" }}>#</span>
                <span style={{ flex: 1, minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                  {c.label}
                </span>
              </div>
            ))}
            <AddRow label="New Channel" />
          </div>

          {/* Apps */}
          <SectionTitle label="Apps" borderTop />
          <div style={{ padding: "4px 12px 16px", flex: 1, overflow: "hidden" }}>
            {[
              { label: "Wiki", icon: <IconBookStack />, badge: "3" },
              { label: "Tasks", icon: <IconCheckCircle /> },
              { label: "Requests", icon: <IconClipboardCheck /> },
              { label: "Policies", icon: <IconShield /> },
              { label: "Calendar", icon: <IconCalendar /> },
              { label: "Skills", icon: <IconFlash /> },
              { label: "Activity", icon: <IconPackage /> },
              { label: "Receipts", icon: <IconPage /> },
            ].map((a) => (
              <div key={a.label} style={{
                display: "flex",
                alignItems: "center",
                gap: 10,
                width: "100%",
                padding: "6px 10px",
                marginBottom: 1,
                borderRadius: 6,
                fontSize: 13,
                color: NEX.sidebarText,
              }}>
                <span style={{
                  width: 16,
                  height: 16,
                  flexShrink: 0,
                  display: "inline-flex",
                  alignItems: "center",
                  justifyContent: "center",
                  color: "currentColor",
                }}>
                  {a.icon}
                </span>
                <span style={{ flex: 1, minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                  {a.label}
                </span>
                {a.badge && (
                  <span style={{
                    minWidth: 18,
                    height: 18,
                    padding: "0 6px",
                    display: "inline-flex",
                    alignItems: "center",
                    justifyContent: "center",
                    borderRadius: 9,
                    background: colors.accent,     // tertiary-400 purple
                    color: "#FFFFFF",
                    fontSize: 10,
                    fontWeight: 600,
                  }}>{a.badge}</span>
                )}
              </div>
            ))}
          </div>

          {/* WorkspaceSummary row */}
          <div style={{
            padding: "6px 20px 2px",
            borderTop: `1px solid ${NEX.sidebarBorder}`,
            fontSize: 11,
            color: NEX.sidebarTextMuted,
            flexShrink: 0,
          }}>
            5 agents active · 2 tasks open
          </div>

          {/* Usage toggle */}
          <div style={{
            display: "flex",
            alignItems: "center",
            gap: 6,
            height: 36,
            padding: "0 16px",
            fontSize: 11,
            color: "rgba(255,255,255,0.78)",
            flexShrink: 0,
          }}>
            <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="m9 18 6-6-6-6" />
            </svg>
            <span>Usage</span>
            <span style={{ marginLeft: "auto", fontFamily: fonts.mono, color: "#FFFFFF" }}>$2.0871</span>
          </div>
        </aside>

        {/* ═════ MAIN ═════ */}
        <div style={{ flex: 1, display: "flex", flexDirection: "column", overflow: "hidden", background: NEX.bg }}>
          {/* Channel header */}
          <div style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            height: 72,
            padding: "0 28px",
            borderBottom: `1px solid ${NEX.border}`,
            background: "rgba(255,255,255,0.8)",
            flexShrink: 0,
          }}>
            <div style={{ display: "flex", alignItems: "center" }}>
              <span style={{ fontSize: 18, fontWeight: 600, color: NEX.text, fontFamily: fonts.sans }}># general</span>
              <span style={{ fontSize: 14, color: NEX.textSecondary, marginLeft: 14, fontFamily: fonts.sans }}>
                The shared office
              </span>
            </div>
            <div style={{
              width: 36,
              height: 36,
              borderRadius: 6,
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              color: NEX.textSecondary,
            }}>
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <circle cx="11" cy="11" r="8" />
                <path d="m21 21-4.3-4.3" />
              </svg>
            </div>
          </div>

          {/* Messages */}
          <div style={{
            flex: 1,
            overflow: "hidden",
            padding: "20px 24px 24px",
            display: "flex",
            flexDirection: "column",
            gap: 10,
            position: "relative",
          }}>
            <ChatMessage
              name="You"
              color={colors.human}
              text="Build a landing page. Ship it today."
              enterFrame={15}
              timestamp="9:01 AM"
            />

            <ChatMessage
              name="CEO"
              color={colors.ceo}
              text="On it. @eng scaffold the page, @gtm write the hero copy."
              enterFrame={55}
              isStreaming
              timestamp="9:01 AM"
              mentions={[
                { name: "eng", color: colors.eng },
                { name: "gtm", color: colors.gtm },
              ]}
            />

            <ChatMessage
              name="Founding Engineer"
              color={colors.eng}
              text="Claiming it. Scaffolding now."
              enterFrame={110}
              timestamp="9:02 AM"
              isReply
              firstOfStack
            />

            <ChatMessage
              name="GTM Lead"
              color={colors.gtm}
              text="Hero copy in 2 minutes. Already have three options."
              enterFrame={150}
              timestamp="9:02 AM"
              isReply
            />
          </div>

          {/* Composer — rounded card with cyan send button */}
          <div style={{
            padding: "16px 28px 20px",
            borderTop: `1px solid ${NEX.border}`,
            background: NEX.bgCard,
            flexShrink: 0,
          }}>
            <div style={{
              display: "flex",
              alignItems: "center",
              gap: 8,
              background: NEX.bg,
              border: `1px solid ${NEX.border}`,
              borderRadius: 10,
              padding: "10px 10px 10px 18px",
            }}>
              <span style={{
                flex: 1,
                fontSize: 15,
                color: NEX.textTertiary,
                fontFamily: fonts.sans,
              }}>
                Message #general
              </span>
              <div style={{
                width: 36,
                height: 36,
                borderRadius: 8,
                background: cyan[400],
                color: "#0b3a44",
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                flexShrink: 0,
              }}>
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="m22 2-7 20-4-9-9-4Z" />
                  <path d="M22 2 11 13" />
                </svg>
              </div>
            </div>
          </div>

          {/* Status bar — matches web/src/components/layout/StatusBar.tsx */}
          <div style={{
            display: "flex",
            alignItems: "center",
            gap: 14,
            height: 44,
            padding: "0 24px",
            borderTop: `1px solid ${NEX.border}`,
            background: NEX.bg,
            fontFamily: fonts.mono,
            fontSize: 12,
            color: NEX.textTertiary,
            flexShrink: 0,
          }}>
            <span># general</span>
            <span>office</span>
            <span style={{ flex: 1 }} />
            <span>5 agents</span>
            <span>⚙ codex</span>
            <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
              <span style={{ width: 7, height: 7, borderRadius: "50%", background: "#03a04c" }} />
              connected
            </span>
          </div>
        </div>
        </div>
        </div>
      </div>
    </AbsoluteFill>
  );
};

const trafficDot = (color: string): React.CSSProperties => ({
  width: 13,
  height: 13,
  borderRadius: "50%",
  background: color,
  display: "inline-block",
});

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

const IconPage = () => (
  <svg {...iconProps}>
    <path d="M6 3h8l5 5v11a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2Z" />
    <path d="M14 3v5h5" />
    <path d="M8 13h8M8 17h5" />
  </svg>
);

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
  <div style={{
    padding: "14px 12px 2px",
    borderTop: borderTop ? `1px solid ${NEX.sidebarBorder}` : undefined,
  }}>
    <div style={{
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
    }}>
      <span>{label}</span>
      {withChevron && (
        <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"
          style={{ transform: expanded ? "rotate(90deg)" : "rotate(0deg)" }}>
          <path d="m9 18 6-6-6-6" />
        </svg>
      )}
    </div>
  </div>
);

const AddRow: React.FC<{ label: string }> = ({ label }) => (
  <div style={{
    display: "flex",
    alignItems: "center",
    gap: 10,
    padding: "6px 10px",
    fontSize: 12,
    color: NEX.sidebarTextMuted,
  }}>
    <span style={{ width: 18, textAlign: "center", flexShrink: 0 }}>+</span>
    <span>{label}</span>
  </div>
);
