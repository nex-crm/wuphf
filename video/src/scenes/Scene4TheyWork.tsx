import { AbsoluteFill, useCurrentFrame, interpolate } from "remotion";
import { colors, fonts, sec, starterAgents, slack } from "../theme";
import { ChatMessage } from "../components/ChatMessage";

export const Scene4TheyWork: React.FC = () => {
  const frame = useCurrentFrame();

  const uiOpacity = interpolate(frame, [0, 12], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  const tokensUsed = interpolate(frame, [sec(2), sec(9)], [0, 3.2], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  const statusColors = [slack.greenPresence, "#1D9BD1", slack.yellow];
  const tasks = ["delegating to team", "scaffolding page", "writing hero copy"];

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
            <div style={{ fontSize: 32, fontWeight: 700, color: "#FFF", fontStyle: "italic" }}>WUPHF</div>
            <div style={{ width: 14, height: 14, borderRadius: "50%", backgroundColor: slack.presence }} />
          </div>

          {/* Channels */}
          <div style={{ padding: "20px 20px 8px" }}>
            <div style={{ fontSize: 16, fontWeight: 600, textTransform: "uppercase" as const, letterSpacing: "0.08em", color: slack.sidebarText, marginBottom: 10 }}>
              Channels
            </div>
            <div style={{
              padding: "12px 16px", borderRadius: 10,
              fontSize: 22, color: "#FFF",
              backgroundColor: slack.sidebarActive, fontWeight: 600,
            }}>
              # general
            </div>
          </div>

          {/* Team */}
          <div style={{ padding: "20px 20px 12px" }}>
            <div style={{ fontSize: 16, fontWeight: 600, textTransform: "uppercase" as const, letterSpacing: "0.08em", color: slack.sidebarText }}>
              Team
            </div>
          </div>

          <div style={{ padding: "0 20px" }}>
            {starterAgents.map((agent, i) => {
              const dotPulse = Math.sin(frame * 0.12 + i * 2) * 0.2 + 0.8;

              return (
                <div key={agent.slug} style={{
                  display: "flex", alignItems: "center", gap: 12,
                  padding: "10px 10px", borderRadius: 8, marginBottom: 4,
                  color: slack.sidebarText,
                }}>
                  <div style={{ width: 12, height: 12, borderRadius: "50%", backgroundColor: statusColors[i], opacity: dotPulse, flexShrink: 0 }} />
                  <div style={{
                    width: 40, height: 40, borderRadius: 8,
                    backgroundColor: agent.color,
                    display: "flex", alignItems: "center", justifyContent: "center",
                    fontSize: 22, flexShrink: 0,
                  }}>
                    {agent.emoji}
                  </div>
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div style={{ fontSize: 20, fontWeight: 600, color: "rgba(255,255,255,0.9)" }}>{agent.name}</div>
                    <div style={{ fontSize: 14, color: "rgba(255,255,255,0.5)", fontFamily: fonts.mono, marginTop: 2 }}>{tasks[i]}</div>
                  </div>
                </div>
              );
            })}
          </div>

          {/* Token tracker */}
          {frame > sec(2) && (
            <div style={{ marginTop: "auto", padding: "20px 24px", borderTop: `1px solid ${slack.sidebarBorder}` }}>
              <div style={{ fontSize: 14, color: slack.sidebarText, textTransform: "uppercase" as const, letterSpacing: "0.08em", marginBottom: 4 }}>
                Tokens this turn
              </div>
              <div style={{ display: "flex", alignItems: "baseline", gap: 4 }}>
                <div style={{ fontSize: 36, fontWeight: 800, color: slack.presence, fontFamily: fonts.mono }}>
                  {tokensUsed.toFixed(1)}K
                </div>
              </div>
            </div>
          )}
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
          <div style={{ padding: "16px 32px 24px" }}>
            <div style={{
              backgroundColor: slack.bgWarm, border: `1px solid ${slack.border}`,
              borderRadius: 12, padding: "18px 22px",
              fontSize: 20, color: slack.textTertiary, fontFamily: fonts.sans,
            }}>
              Message #general...
            </div>
          </div>
        </div>
      </div>
    </AbsoluteFill>
  );
};
