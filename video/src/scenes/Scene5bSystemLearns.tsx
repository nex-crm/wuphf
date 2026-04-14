import { AbsoluteFill, useCurrentFrame, interpolate, Easing, spring } from "remotion";
import { colors, fonts, sec, FPS, slack } from "../theme";
import { AgentAvatar } from "../components/AgentAvatar";
import { FadeIn } from "../components/FadeIn";
import { DotGrid, RadialGlow } from "../components/DotGrid";
import { Confetti } from "../components/Confetti";

export const Scene5bSystemLearns: React.FC = () => {
  const frame = useCurrentFrame();

  const uiOpacity = interpolate(frame, [0, 12], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  // CEO message with proposal
  const ceoMsgOpacity = interpolate(frame, [15, 25], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });
  const ceoMsgSlide = interpolate(frame, [15, 25], [15, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.out(Easing.cubic),
  });

  // Proposal card flies in
  const proposalScale = spring({
    frame: Math.max(0, frame - sec(1.5)),
    fps: FPS,
    config: { damping: 14, stiffness: 180 },
  });
  const proposalOpacity = interpolate(frame, [sec(1.5), sec(2)], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  // Approve button glows
  const approveGlow = interpolate(
    frame,
    [sec(3.5), sec(4), sec(4.5), sec(5)],
    [0, 1, 1, 0.6],
    { extrapolateLeft: "clamp", extrapolateRight: "clamp" }
  );

  // Click animation
  const clickFrame = sec(4.8);
  const clickScale = frame >= clickFrame
    ? interpolate(frame, [clickFrame, clickFrame + 3, clickFrame + 8], [1, 0.95, 1], {
        extrapolateLeft: "clamp",
        extrapolateRight: "clamp",
      })
    : 1;

  // "Approved" badge appears after click
  const approvedOpacity = interpolate(frame, [sec(5), sec(5.3)], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  // Success ripple
  const rippleScale = interpolate(frame, [sec(5), sec(5.8)], [0.5, 2], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.out(Easing.cubic),
  });
  const rippleOpacity = interpolate(frame, [sec(5), sec(5.8)], [0.4, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  // Bottom narration
  const narrationOpacity = interpolate(frame, [sec(5.5), sec(6)], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  return (
    <AbsoluteFill style={{ backgroundColor: "#0B0D10", opacity: uiOpacity }}>
      <DotGrid color="#FFFFFF" opacity={0.04} spacing={40} size={1.2} />
      <RadialGlow color={slack.presence} x="50%" y="50%" size={1200} opacity={0.18} />
      <Confetti startFrame={sec(5)} count={50} originX={0.5} originY={0.5} />
      <div
        style={{
          position: "absolute", inset: 0,
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          justifyContent: "center",
          padding: 60,
          gap: 24,
        }}
      >
      {/* Header */}
      <FadeIn startFrame={5} durationFrames={12}>
        <div
          style={{
            fontFamily: fonts.sans,
            fontSize: 24,
            color: colors.textMuted,
            textTransform: "uppercase" as const,
            letterSpacing: 3,
            marginBottom: 16,
          }}
        >
          Pattern Detected
        </div>
      </FadeIn>

      {/* CEO message */}
      <div
        style={{
          opacity: ceoMsgOpacity,
          transform: `translateY(${ceoMsgSlide}px)`,
          display: "flex",
          gap: 20,
          maxWidth: 900,
          padding: "22px 32px",
          backgroundColor: colors.bgCard,
          borderRadius: 16,
          border: `1px solid ${colors.ceo}30`,
        }}
      >
        <AgentAvatar name="CEO" color={colors.ceo} size={60} />
        <div style={{ flex: 1 }}>
          <div style={{ fontFamily: fonts.sans, fontSize: 22, fontWeight: 700, color: colors.ceo }}>
            CEO
          </div>
          <div style={{ fontFamily: fonts.sans, fontSize: 22, color: colors.text, marginTop: 6, lineHeight: 1.5 }}>
            I've noticed we prep every sales meeting the same way. Proposing a reusable skill...
          </div>
        </div>
      </div>

      {/* Skill Proposal Card */}
      <div
        style={{
          opacity: proposalOpacity,
          transform: `scale(${proposalScale})`,
          width: 700,
          backgroundColor: colors.bgTerminal,
          borderRadius: 16,
          border: `1px solid ${colors.green}40`,
          overflow: "hidden",
          boxShadow: `0 8px 32px rgba(0,0,0,0.4)`,
          position: "relative",
        }}
      >
        {/* Card header */}
        <div
          style={{
            padding: "18px 28px",
            backgroundColor: `${colors.green}15`,
            borderBottom: `1px solid ${colors.green}30`,
            display: "flex",
            alignItems: "center",
            gap: 12,
          }}
        >
          <div style={{ fontFamily: fonts.sans, fontSize: 22, fontWeight: 700, color: colors.green }}>
            New Skill Proposal
          </div>
        </div>

        {/* Card content */}
        <div style={{ padding: "22px 28px" }}>
          <div style={{ fontFamily: fonts.mono, fontSize: 24, color: colors.textBright, fontWeight: 700 }}>
            deal-prep
          </div>
          <div style={{ fontFamily: fonts.sans, fontSize: 18, color: colors.text, marginTop: 10, lineHeight: 1.5 }}>
            Pull company brief, past touchpoints, buying committee, and likely objections. Drop the note in the CRM 10 min before every meeting.
          </div>
          <div style={{ display: "flex", gap: 8, marginTop: 16 }}>
            {["revops", "sales", "prep"].map((tag) => (
              <span
                key={tag}
                style={{
                  padding: "4px 14px",
                  borderRadius: 100,
                  backgroundColor: `${colors.accent}20`,
                  fontFamily: fonts.mono,
                  fontSize: 14,
                  color: colors.accent,
                  fontWeight: 600,
                }}
              >
                {tag}
              </span>
            ))}
          </div>
        </div>

        {/* Action buttons */}
        <div
          style={{
            padding: "12px 20px",
            borderTop: "1px solid #30363D",
            display: "flex",
            gap: 10,
            justifyContent: "flex-end",
          }}
        >
          <div
            style={{
              padding: "12px 28px",
              borderRadius: 10,
              backgroundColor: "#333",
              fontFamily: fonts.sans,
              fontSize: 18,
              fontWeight: 600,
              color: colors.textMuted,
            }}
          >
            Reject
          </div>
          <div
            style={{
              padding: "12px 32px",
              borderRadius: 10,
              backgroundColor: colors.green,
              fontFamily: fonts.sans,
              fontSize: 18,
              fontWeight: 700,
              color: "#FFF",
              transform: `scale(${clickScale})`,
              boxShadow: `0 0 ${approveGlow * 20}px ${colors.green}${Math.round(approveGlow * 60).toString(16).padStart(2, "0")}`,
              position: "relative",
            }}
          >
            Approve
          </div>
        </div>

        {/* Approved overlay */}
        {frame >= sec(5) && (
          <div
            style={{
              position: "absolute",
              top: 0,
              left: 0,
              right: 0,
              bottom: 0,
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              backgroundColor: `${colors.green}E0`,
              opacity: approvedOpacity,
            }}
          >
            <div style={{ fontFamily: fonts.sans, fontSize: 28, fontWeight: 700, color: "#FFF" }}>
              Approved {"\u2713"}
            </div>
          </div>
        )}

        {/* Success ripple */}
        {frame >= sec(5) && (
          <div
            style={{
              position: "absolute",
              top: "50%",
              left: "50%",
              width: 200,
              height: 200,
              borderRadius: "50%",
              border: `2px solid ${colors.green}`,
              transform: `translate(-50%, -50%) scale(${rippleScale})`,
              opacity: rippleOpacity,
              pointerEvents: "none" as const,
            }}
          />
        )}
      </div>

      {/* Evolution text */}
      <FadeIn startFrame={sec(5.8)} durationFrames={12} slideUp={10}>
        <div
          style={{
            fontFamily: fonts.sans,
            fontSize: 16,
            color: colors.textDim,
            textAlign: "center",
          }}
        >
          Skill injected into all agents. Next time, they know how.
        </div>
      </FadeIn>

      {/* Bottom narration */}
      <div
        style={{
          position: "absolute",
          bottom: 50,
          opacity: narrationOpacity,
          fontFamily: fonts.sans,
          fontSize: 28,
          fontWeight: 500,
          color: colors.textBright,
          textAlign: "center",
        }}
      >
        The system learns. You just approve.
      </div>
      </div>
    </AbsoluteFill>
  );
};
