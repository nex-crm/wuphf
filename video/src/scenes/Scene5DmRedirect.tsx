import { AbsoluteFill, useCurrentFrame, interpolate } from "remotion";
import { colors, fonts, sec, slack } from "../theme";
import { ChatMessage } from "../components/ChatMessage";
import { PixelAvatar } from "../components/PixelAvatar";

export const Scene5DmRedirect: React.FC = () => {
  const frame = useCurrentFrame();

  const uiOpacity = interpolate(frame, [0, 10], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  const livePulse = Math.sin(frame * 0.15) * 0.3 + 0.7;

  const leadsCount = Math.min(47, Math.floor(interpolate(frame, [0, sec(5)], [0, 47], { extrapolateLeft: "clamp", extrapolateRight: "clamp" })));

  return (
    <AbsoluteFill style={{ backgroundColor: slack.bg, opacity: uiOpacity }}>
      <div style={{ display: "flex", height: "100%" }}>
        {/* Agent profile panel — wider */}
        <div style={{
          width: 440,
          backgroundColor: slack.bgWarm,
          borderRight: `1px solid ${slack.border}`,
          padding: 44,
          display: "flex", flexDirection: "column", alignItems: "center", gap: 18,
        }}>
          <PixelAvatar slug="gtm" color={colors.gtm} size={96} />

          <div style={{ fontFamily: fonts.sans, fontSize: 32, fontWeight: 700, color: slack.text }}>
            GTM Lead
          </div>

          <div style={{
            display: "flex", alignItems: "center", gap: 10,
            fontFamily: fonts.sans, fontSize: 20, color: slack.greenPresence,
          }}>
            <div style={{ width: 14, height: 14, borderRadius: "50%", backgroundColor: slack.greenPresence, opacity: livePulse }} />
            working — pulling lead list
          </div>

          <div style={{
            width: "100%", backgroundColor: "#0D1117",
            borderRadius: 12, padding: 20,
            fontFamily: fonts.mono, fontSize: 18, color: slack.text,
            lineHeight: 1.8, marginTop: 20, border: `1px solid ${slack.border}`,
          }}>
            <div style={{ color: colors.gtm }}>// leads.csv</div>
            <div>filter: <span style={{ color: slack.yellow }}>fintech, Series A-B</span></div>
            <div>matched: <span style={{ color: slack.presence }}>{leadsCount} / 50</span></div>
            <div style={{ color: slack.textTertiary, fontSize: 16 }}>source: Apollo, LinkedIn</div>
          </div>

          <div style={{
            display: "flex", alignItems: "center", gap: 10,
            fontFamily: fonts.mono, fontSize: 18, color: slack.textTertiary, marginTop: 16,
          }}>
            <div style={{ width: 10, height: 10, borderRadius: "50%", backgroundColor: slack.red, opacity: livePulse }} />
            LIVE enrichment
          </div>
        </div>

        {/* DM conversation */}
        <div style={{ flex: 1, display: "flex", flexDirection: "column" }}>
          <div style={{
            padding: "24px 32px", borderBottom: `1px solid ${slack.border}`,
            fontFamily: fonts.sans, display: "flex", alignItems: "center", gap: 14,
          }}>
            <PixelAvatar slug="gtm" color={colors.gtm} size={32} />
            <span style={{ fontSize: 28, fontWeight: 700, color: slack.text }}>GTM Lead</span>
            <span style={{ fontSize: 18, color: slack.textTertiary }}>DM</span>
          </div>

          <div style={{ flex: 1, padding: "20px 0", display: "flex", flexDirection: "column", gap: 14 }}>
            <ChatMessage
              name="GTM Lead"
              color={colors.gtm}
              text="Pulling 50 qualified leads in fintech, Series A-B. List in 2 minutes."
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

          <div style={{ padding: "16px 32px 24px" }}>
            <div style={{
              backgroundColor: slack.bgWarm, border: `1px solid ${slack.border}`,
              borderRadius: 12, padding: "18px 22px",
              fontSize: 20, color: slack.textTertiary, fontFamily: fonts.sans,
            }}>
              Message GTM Lead...
            </div>
          </div>
        </div>
      </div>
    </AbsoluteFill>
  );
};
