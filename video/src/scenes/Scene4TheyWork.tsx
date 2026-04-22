import { AbsoluteFill, useCurrentFrame, interpolate } from "remotion";
import { colors, cyan, fonts, olive, sec, starterAgents, slack } from "../theme";
import { ChatMessage } from "../components/ChatMessage";
import { PixelAvatar } from "../components/PixelAvatar";

export const Scene4TheyWork: React.FC = () => {
  const frame = useCurrentFrame();

  const uiOpacity = interpolate(frame, [0, 12], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  const statusColors = [cyan[400], cyan[400], olive[300], cyan[400], cyan[400]];
  const tasks = [
    "delegating to team",
    "writing hero copy",
    "scaffolding page",
    "reviewing brief",
    "drafting visuals",
  ];

  return (
    <AbsoluteFill style={{ backgroundColor: slack.bg, opacity: uiOpacity }}>
      <div style={{ display: "flex", height: "100%" }}>
        {/* ── SIDEBAR (wider, bigger text) ── */}
        <div style={{
          width: 380,
          backgroundColor: slack.sidebar,
          display: "flex",
          flexDirection: "column",
          fontFamily: fonts.sans,
        }}>
          {/* Logo */}
          <div style={{
            padding: "28px 24px 24px",
            borderBottom: `1px solid ${slack.sidebarBorder}`,
            backgroundColor: "rgba(255,255,255,0.04)",
            display: "flex", alignItems: "center", justifyContent: "space-between",
          }}>
            <div style={{ fontSize: 24, fontWeight: 700, color: "#FFF", fontStyle: "italic" }}>WUPHF</div>
            <div style={{ width: 14, height: 14, borderRadius: "50%", backgroundColor: slack.presence }} />
          </div>

          {/* ── TEAM (matches real app order: Team → Channels → Apps) ── */}
          <div style={{ padding: "16px 18px 6px" }}>
            <div style={{ fontSize: 10, fontWeight: 700, textTransform: "uppercase" as const, letterSpacing: "0.08em", color: olive[200], opacity: 0.7 }}>
              Team
            </div>
          </div>

          <div style={{ padding: "0 10px" }}>
            {starterAgents.map((agent, i) => {
              const dotPulse = Math.sin(frame * 0.12 + i * 2) * 0.2 + 0.8;

              return (
                <div key={agent.slug} style={{
                  display: "flex", alignItems: "center", gap: 10,
                  padding: "6px 10px", borderRadius: 5, marginBottom: 1,
                  color: slack.sidebarText,
                }}>
                  <div style={{ width: 22, height: 22, background: "rgba(255,255,255,0.05)", borderRadius: 5, display: "flex", alignItems: "center", justifyContent: "center", overflow: "hidden" }}>
                    <PixelAvatar slug={agent.slug} color={agent.color} size={20} />
                  </div>
                  <div style={{ flex: 1, minWidth: 0, fontSize: 13, color: "rgba(255,255,255,0.85)" }}>{agent.name}</div>
                  <div style={{ width: 7, height: 7, borderRadius: "50%", backgroundColor: statusColors[i] ?? cyan[400], opacity: dotPulse, flexShrink: 0 }} />
                </div>
              );
            })}
            <div style={{ padding: "6px 10px", color: "rgba(255,255,255,0.45)", fontSize: 12 }}>
              + New Agent
            </div>
          </div>

          {/* ── CHANNELS ── */}
          <div style={{ padding: "14px 18px 4px" }}>
            <div style={{ fontSize: 10, fontWeight: 700, textTransform: "uppercase" as const, letterSpacing: "0.08em", color: olive[200], opacity: 0.7 }}>
              Channels
            </div>
          </div>
          <div style={{ padding: "0 10px" }}>
            {[
              { label: "general", active: true },
              { label: "product", active: false },
              { label: "gtm", active: false },
            ].map((c) => (
              <div key={c.label} style={{
                padding: "5px 12px", borderRadius: 5,
                fontSize: 13, color: c.active ? "rgba(255,255,255,0.95)" : slack.sidebarText,
                background: c.active ? "rgba(246,215,77,0.22)" : "transparent",
                fontWeight: c.active ? 600 : 400,
                marginBottom: 1,
                display: "flex", gap: 6,
              }}>
                <span style={{ opacity: 0.6 }}>#</span>
                <span>{c.label}</span>
              </div>
            ))}
            <div style={{ padding: "6px 12px", color: "rgba(255,255,255,0.45)", fontSize: 12 }}>
              + New Channel
            </div>
          </div>

          {/* ── APPS (matches real app — Wiki active with 3-pending badge) ── */}
          <div style={{ padding: "14px 18px 4px" }}>
            <div style={{ fontSize: 10, fontWeight: 700, textTransform: "uppercase" as const, letterSpacing: "0.08em", color: olive[200], opacity: 0.7 }}>
              Apps
            </div>
          </div>
          <div style={{ padding: "0 10px", flex: 1, overflow: "hidden" }}>
            {[
              { label: "Wiki", badge: "3" },
              { label: "Tasks" },
              { label: "Requests" },
              { label: "Policies" },
              { label: "Calendar" },
              { label: "Skills" },
              { label: "Activity" },
              { label: "Receipts" },
            ].map((a) => (
              <div key={a.label} style={{
                display: "flex", alignItems: "center", gap: 10,
                padding: "6px 12px", borderRadius: 5, marginBottom: 1,
                color: slack.sidebarText,
                fontSize: 13,
              }}>
                <span style={{ width: 14, height: 14, background: "rgba(255,255,255,0.18)", borderRadius: 3, display: "inline-block" }} />
                <span>{a.label}</span>
                {a.badge && (
                  <span style={{
                    marginLeft: "auto",
                    background: "rgba(159,77,191,0.85)",
                    color: "#FFF",
                    borderRadius: 999,
                    fontSize: 11,
                    fontWeight: 600,
                    padding: "1px 8px",
                  }}>
                    {a.badge}
                  </span>
                )}
              </div>
            ))}
          </div>

          {/* Bottom usage row (matches real app) */}
          <div style={{ padding: "8px 16px 6px", borderTop: `1px solid ${slack.sidebarBorder}`, fontSize: 11, color: "rgba(255,255,255,0.55)" }}>
            5 agents active, 2 tasks open, 2.4M tokens
          </div>
          <div style={{ padding: "4px 16px 10px", fontSize: 11, color: "rgba(255,255,255,0.5)", display: "flex" }}>
            <span>Usage</span>
            <span style={{ marginLeft: "auto", fontFamily: fonts.mono, color: "rgba(255,255,255,0.85)" }}>$2.0871</span>
          </div>
        </div>

        {/* ── MAIN CHANNEL ── */}
        <div style={{ flex: 1, display: "flex", flexDirection: "column", backgroundColor: slack.bg }}>
          <div style={{
            padding: "24px 32px", borderBottom: `1px solid ${slack.border}`,
            display: "flex", alignItems: "baseline", gap: 18,
          }}>
            <span style={{ fontFamily: fonts.sans, fontSize: 28, fontWeight: 700, color: slack.text }}># general</span>
            <span style={{ fontFamily: fonts.sans, fontSize: 18, color: slack.textTertiary }}>The shared office</span>
          </div>

          <div style={{ flex: 1, padding: "20px 0", display: "flex", flexDirection: "column", gap: 14, position: "relative" }}>
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

          {/* Composer */}
          <div style={{ padding: "12px 32px 16px" }}>
            <div style={{
              backgroundColor: slack.bgWarm, border: `1px solid ${slack.border}`,
              borderRadius: 8, padding: "14px 18px",
              fontSize: 18, color: slack.textTertiary, fontFamily: fonts.sans,
              display: "flex", alignItems: "center", justifyContent: "space-between",
            }}>
              <span>Message #general — type / for commands, @ to mention</span>
              {/* Send arrow */}
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke={slack.textTertiary} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="m22 2-7 20-4-9-9-4z"/><path d="m22 2-10 10"/>
              </svg>
            </div>
          </div>

          {/* Status bar — matches real platform exactly */}
          <div style={{
            padding: "4px 16px", borderTop: `1px solid ${slack.borderLight}`,
            display: "flex", alignItems: "center", gap: 16,
            fontFamily: fonts.mono, fontSize: 12, color: slack.textTertiary,
            backgroundColor: slack.bgWarm,
          }}>
            <span style={{ color: slack.text }}># general office</span>
            <span style={{ marginLeft: "auto" }}>codex</span>
            <span>3 agents</span>
            <span style={{ display: "flex", alignItems: "center", gap: 4 }}>
              <div style={{ width: 6, height: 6, borderRadius: "50%", backgroundColor: slack.greenPresence }} />
              connected
            </span>
          </div>
        </div>
      </div>
    </AbsoluteFill>
  );
};
