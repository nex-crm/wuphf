import { AbsoluteFill, Easing, interpolate, useCurrentFrame } from "remotion";
import { colors, cyan, fonts, sec } from "../theme";
import { ChatMessage } from "../components/ChatMessage";
import { PixelAvatar } from "../components/PixelAvatar";
import { NexSidebar } from "../components/NexSidebar";

// Scene 5: 1:1 with the GTM Lead agent while they pull a lead list.
// The real web app doesn't have this exact layout — our version rebuilds
// it from the nex design tokens so it matches Scene 4 + 5c.

const NEX = {
  sidebarBg: "#612a92",
  sidebarText: "rgba(255,255,255,0.72)",
  sidebarTextMuted: "rgba(255,255,255,0.55)",
  sidebarTitle: "rgba(255,255,255,0.55)",
  sidebarBorder: "rgba(255,255,255,0.08)",
  activeBg: cyan[400],
  activeFg: "#0b3a44",
  presence: "#35da79",
  bg: "#FFFFFF",
  border: "#e9eaeb",
  text: "#28292a",
  textSecondary: "#686c6e",
  textTertiary: "#85898b",
};

const ELEGANT = Easing.bezier(0.25, 0.46, 0.45, 0.94);

const trafficDot = (color: string): React.CSSProperties => ({
  width: 13,
  height: 13,
  borderRadius: "50%",
  background: color,
  display: "inline-block",
});

export const Scene5DmRedirect: React.FC = () => {
  const frame = useCurrentFrame();

  const uiOpacity = interpolate(frame, [0, 14], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });
  const slideY = interpolate(frame, [0, 18], [80, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: ELEGANT,
  });

  // Fall-out at the end — accelerates with gravity, fades out near the end
  const SCENE_DURATION = sec(9.5);
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

  const livePulse = 0.55 + 0.45 * Math.sin(frame * 0.18);
  const leadsCount = Math.min(
    47,
    Math.round(
      interpolate(frame, [0, sec(5)], [0, 47], {
        extrapolateLeft: "clamp",
        extrapolateRight: "clamp",
      }),
    ),
  );

  return (
    <AbsoluteFill
      style={{
        background: "radial-gradient(ellipse at top, #f3e8ff 0%, #ede2f7 40%, #d9c6ea 100%)",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
      }}
    >
      {/* Window shell */}
      <div
        style={{
          width: 1280,
          height: 1200,
          marginTop: 280,
          opacity: uiOpacity * exitFade,
          transform: `translateY(${slideY + fallY}px) rotateZ(${fallRot}deg) scale(1.3)`,
          transformOrigin: "center",
          background: "#ebe5f0",
          borderRadius: 20,
          padding: 4,
          overflow: "hidden",
          boxShadow:
            "0 0 0 1px rgba(0,0,0,0.05), 0 40px 100px rgba(66, 26, 104, 0.35), 0 12px 32px rgba(0,0,0,0.12)",
          display: "flex",
          flexDirection: "column",
        }}
      >
        <div style={{ flex: 1, display: "flex", flexDirection: "column", minHeight: 0 }}>
          {/* Titlebar */}
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: 10,
              height: 40,
              padding: "0 14px",
              background: "#ebe5f0",
              borderBottom: "1px solid rgba(0,0,0,0.06)",
              flexShrink: 0,
            }}
          >
            <div style={{ display: "flex", gap: 8 }}>
              <span style={trafficDot("#ff5f57")} />
              <span style={trafficDot("#febc2e")} />
              <span style={trafficDot("#28c840")} />
            </div>
            <span
              style={{
                flex: 1,
                textAlign: "center",
                fontFamily: fonts.sans,
                fontSize: 12,
                color: "#686c6e",
                letterSpacing: "0.01em",
              }}
            >
              wuphf.app — @gtm
            </span>
            <span style={{ width: 54 }} />
          </div>

          {/* Rounded UI container */}
          <div
            style={{
              flex: 1,
              display: "flex",
              minHeight: 0,
              borderRadius: 16,
              overflow: "hidden",
            }}
          >
            <NexSidebar
              active={{ kind: "dm", slug: "gtm" }}
              agentTasks={{
                ceo: "reviewing pipeline",
                gtm: "pulling leads",
                eng: "scaffolding page",
                pm: "spec review",
                designer: "hero visuals",
              }}
            />

            {/* ═════ MAIN — DM ═════ */}
            <div style={{ flex: 1, display: "flex", flexDirection: "column", overflow: "hidden", background: NEX.bg }}>
              {/* DM header */}
              <div
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 12,
                  height: 72,
                  padding: "0 28px",
                  borderBottom: `1px solid ${NEX.border}`,
                  background: "rgba(255,255,255,0.8)",
                  flexShrink: 0,
                }}
              >
                <div style={{ width: 36, height: 36, display: "flex", alignItems: "center", justifyContent: "center" }}>
                  <PixelAvatar slug="gtm" color={colors.gtm} size={32} />
                </div>
                <span style={{ fontSize: 18, fontWeight: 600, color: NEX.text, fontFamily: fonts.sans }}>
                  GTM Lead
                </span>
                <span
                  style={{
                    display: "inline-flex",
                    alignItems: "center",
                    gap: 6,
                    padding: "3px 10px",
                    background: "#e7f9ed",
                    color: "#0B6B2A",
                    borderRadius: 999,
                    fontFamily: fonts.sans,
                    fontSize: 12,
                    fontWeight: 600,
                  }}
                >
                  <span
                    style={{
                      width: 7,
                      height: 7,
                      borderRadius: "50%",
                      background: NEX.presence,
                      opacity: livePulse,
                    }}
                  />
                  online
                </span>
                <span style={{ marginLeft: "auto", fontSize: 12, color: NEX.textTertiary, fontFamily: fonts.mono, letterSpacing: "0.04em" }}>
                  1:1
                </span>
              </div>

              {/* Messages */}
              <div
                style={{
                  flex: 1,
                  overflow: "hidden",
                  padding: "20px 24px 24px",
                  display: "flex",
                  flexDirection: "column",
                  gap: 10,
                  position: "relative",
                }}
              >
                <ChatMessage
                  name="GTM Lead"
                  color={colors.gtm}
                  text="Pulling 50 qualified leads in fintech, Series A–B. List in 2 minutes."
                  enterFrame={15}
                  timestamp="9:05 AM"
                />
                <ChatMessage
                  name="You"
                  color={colors.human}
                  text="Actually, expand to Series C. And filter for revenue $10M+."
                  enterFrame={65}
                  timestamp="9:06 AM"
                />
                <ChatMessage
                  name="GTM Lead"
                  color={colors.gtm}
                  text="Got it. Widening the filter. Adding revenue gate."
                  enterFrame={115}
                  isStreaming
                  timestamp="9:06 AM"
                />
              </div>

              {/* Status bar */}
              <div
                style={{
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
                }}
              >
                <span>@gtm</span>
                <span>1:1</span>
                <span style={{ flex: 1 }} />
                <span>5 agents</span>
                <span>⚙ claude-code</span>
                <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
                  <span style={{ width: 7, height: 7, borderRadius: "50%", background: "#03a04c" }} />
                  connected
                </span>
              </div>
            </div>

            {/* ═════ RIGHT RAIL — Agent Panel (mirrors web AgentPanel.tsx) ═════ */}
            <aside
              style={{
                width: 400,
                flexShrink: 0,
                borderLeft: `1px solid ${NEX.border}`,
                background: "#FFFFFF",
                fontFamily: fonts.sans,
                display: "flex",
                flexDirection: "column",
                overflow: "hidden",
                boxShadow: "-4px 0 24px rgba(0, 0, 0, 0.04)",
              }}
            >
              {/* Header: avatar + name/role + close */}
              <div
                style={{
                  display: "flex",
                  alignItems: "flex-start",
                  justifyContent: "space-between",
                  padding: "16px 20px 8px",
                  gap: 12,
                  flexShrink: 0,
                }}
              >
                <div style={{ display: "flex", alignItems: "center", gap: 10, flex: 1, minWidth: 0 }}>
                  <div style={{
                    width: 36,
                    height: 36,
                    borderRadius: 8,
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "center",
                    flexShrink: 0,
                  }}>
                    <PixelAvatar slug="gtm" color={colors.gtm} size={36} />
                  </div>
                  <div style={{ minWidth: 0, display: "flex", flexDirection: "column", gap: 2 }}>
                    <div style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
                      <span style={{ fontSize: 15, fontWeight: 700, color: NEX.text }}>GTM Lead</span>
                      <span style={{
                        width: 6, height: 6, borderRadius: "50%",
                        background: NEX.presence, opacity: livePulse,
                        marginLeft: -2,
                      }} />
                    </div>
                    <span style={{ fontSize: 12, color: NEX.textTertiary, fontWeight: 400, lineHeight: 1.4 }}>
                      Go-to-market lead
                    </span>
                  </div>
                </div>
                <div style={{
                  width: 36, height: 36, borderRadius: 6,
                  display: "flex", alignItems: "center", justifyContent: "center",
                  color: NEX.textSecondary,
                  flexShrink: 0,
                }}>
                  <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M6 6 18 18M18 6 6 18" />
                  </svg>
                </div>
              </div>

              {/* Info grid */}
              <div style={{ padding: "16px 20px", borderBottom: `1px solid ${NEX.border}` }}>
                <InfoRow label="slug" value="gtm" />
                <InfoRow label="provider" value="claude-code" />
                <InfoRow label="status" value="active" />
                <InfoRow label="task" value="pulling lead list" />
              </div>

              {/* Enabled in #general toggle */}
              <div style={{
                padding: "14px 20px",
                borderBottom: `1px solid ${NEX.border}`,
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                gap: 12,
              }}>
                <span style={{ fontSize: 13, color: NEX.text }}>
                  Enabled in <strong style={{ fontWeight: 600 }}>#general</strong>
                </span>
                <div style={{
                  width: 36,
                  height: 20,
                  borderRadius: 999,
                  background: colors.accent,
                  position: "relative",
                  flexShrink: 0,
                }}>
                  <div style={{
                    position: "absolute",
                    right: 3,
                    top: 3,
                    width: 14,
                    height: 14,
                    borderRadius: "50%",
                    background: "#FFFFFF",
                  }} />
                </div>
              </div>

              {/* Primary actions */}
              <div style={{
                display: "flex",
                gap: 8,
                padding: "14px 20px",
                borderBottom: `1px solid ${NEX.border}`,
              }}>
                <button style={{
                  flex: 1,
                  height: 32,
                  padding: "0 14px",
                  borderRadius: 999,
                  background: colors.accent,
                  color: "#FFFFFF",
                  border: "none",
                  fontFamily: fonts.sans,
                  fontSize: 12,
                  fontWeight: 500,
                  cursor: "pointer",
                }}>
                  Open DM
                </button>
                <button style={{
                  flex: 1,
                  height: 32,
                  padding: "0 14px",
                  borderRadius: 999,
                  background: colors.accentBg,
                  color: colors.accent,
                  border: `1px solid ${NEX.border}`,
                  fontFamily: fonts.sans,
                  fontSize: 12,
                  fontWeight: 500,
                  cursor: "pointer",
                }}>
                  View logs
                </button>
              </div>

              {/* Live stream section */}
              <div style={{ padding: "16px 20px", flex: 1, display: "flex", flexDirection: "column", minHeight: 0 }}>
                <div style={{
                  fontSize: 10, fontWeight: 600, textTransform: "uppercase",
                  letterSpacing: "0.05em", color: NEX.textTertiary,
                  marginBottom: 8,
                }}>
                  Live stream
                </div>
                <div style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 6,
                  fontSize: 11,
                  color: NEX.textTertiary,
                  marginBottom: 6,
                }}>
                  <span style={{
                    width: 5, height: 5, borderRadius: "50%",
                    background: NEX.presence,
                    opacity: livePulse,
                  }} />
                  Connected
                </div>
                <div style={{
                  fontFamily: fonts.mono,
                  fontSize: 11,
                  lineHeight: 1.6,
                  background: "#fafafa",
                  border: `1px solid ${NEX.border}`,
                  borderRadius: 4,
                  padding: "8px 12px",
                  flex: 1,
                  overflow: "hidden",
                  color: NEX.textSecondary,
                }}>
                  <div style={{ color: NEX.textTertiary }}>// leads.csv</div>
                  <div>
                    filter: <span style={{ color: colors.accent, fontWeight: 500 }}>fintech, Series A–B</span>
                  </div>
                  <div>
                    matched:{" "}
                    <span style={{ color: "#03a04c", fontWeight: 600, fontVariantNumeric: "tabular-nums" as const }}>
                      {leadsCount} / 50
                    </span>
                  </div>
                  <div style={{ color: NEX.textTertiary }}>source: Apollo, LinkedIn</div>
                  <div style={{ marginTop: 4, color: NEX.textSecondary }}>
                    → widening filter to Series C…
                  </div>
                </div>
              </div>
            </aside>
          </div>
        </div>
      </div>
    </AbsoluteFill>
  );
};

const InfoRow: React.FC<{ label: string; value: string }> = ({ label, value }) => (
  <div style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 12, padding: "3px 0" }}>
    <span style={{ color: "#85898b", fontFamily: fonts.mono, minWidth: 72, flexShrink: 0 }}>
      {label}
    </span>
    <span style={{ color: "#28292a", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
      {value}
    </span>
  </div>
);

