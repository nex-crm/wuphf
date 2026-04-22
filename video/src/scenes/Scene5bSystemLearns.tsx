import { AbsoluteFill, useCurrentFrame, interpolate, Easing, spring } from "remotion";
import { colors, fonts, sec, FPS, olive, tertiary } from "../theme";
import { PixelAvatar } from "../components/PixelAvatar";
import { FadeIn } from "../components/FadeIn";
import { DotGrid, RadialGlow } from "../components/DotGrid";

// v26 Scene 5b, reframed 2026-04-22: narration stays byte-for-byte
// ("They notice patterns, propose improvements. You just say yes…"),
// but the artifact on screen now reflects the real product feature —
// an agent's notebook entry being promoted to the team wiki via
// human review. Matches web/src/components/notebook/PromoteButton.tsx.

export const Scene5bSystemLearns: React.FC = () => {
  const frame = useCurrentFrame();

  const bgOpacity = interpolate(frame, [0, 12], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  const headerOpacity = interpolate(frame, [5, 17], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  const ceoMsgOpacity = interpolate(frame, [15, 25], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });
  const ceoMsgSlide = interpolate(frame, [15, 25], [12, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.out(Easing.cubic),
  });

  const cardScale = spring({
    frame: Math.max(0, frame - sec(1.5)),
    fps: FPS,
    config: { damping: 14, stiffness: 180 },
  });
  const cardOpacity = interpolate(frame, [sec(1.5), sec(2)], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  // State machine on the "Promote to wiki" button:
  //   before 3.8s : idle "Promote to wiki →"
  //   3.8s–5.0s   : "Pending review by you"
  //   after 5.0s  : promoted — green border + "Added to wiki"
  const clickFrame = sec(3.8);
  const promotedFrame = sec(5.0);

  const buttonGlow = interpolate(
    frame,
    [sec(2.5), sec(3.2), sec(3.8), sec(4.2)],
    [0, 1, 1, 0.2],
    { extrapolateLeft: "clamp", extrapolateRight: "clamp" },
  );
  const buttonPress = frame >= clickFrame
    ? interpolate(frame, [clickFrame, clickFrame + 3, clickFrame + 8], [1, 0.94, 1], {
        extrapolateLeft: "clamp",
        extrapolateRight: "clamp",
      })
    : 1;

  const pending = frame >= clickFrame && frame < promotedFrame;
  const promoted = frame >= promotedFrame;

  const rippleScale = interpolate(frame, [promotedFrame, promotedFrame + 18], [0.6, 2.1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.out(Easing.cubic),
  });
  const rippleOpacity = interpolate(frame, [promotedFrame, promotedFrame + 18], [0.35, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  const taglineOpacity = interpolate(frame, [sec(5.8), sec(6.3)], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  return (
    <AbsoluteFill style={{ backgroundColor: colors.bgWarm, opacity: bgOpacity }}>
      <DotGrid color={tertiary[400]} opacity={0.05} spacing={40} size={1.2} />
      <RadialGlow color={tertiary[400]} x="50%" y="50%" size={1200} opacity={0.12} />

      <div
        style={{
          position: "absolute",
          inset: 0,
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          justifyContent: "center",
          padding: 60,
          gap: 24,
        }}
      >
        {/* Header */}
        <div
          style={{
            opacity: headerOpacity,
            fontFamily: fonts.sans,
            fontSize: 22,
            color: colors.textTertiary,
            textTransform: "uppercase" as const,
            letterSpacing: 3,
            marginBottom: 8,
            fontWeight: 600,
          }}
        >
          Pattern Detected
        </div>

        {/* CEO message (dark chat bubble, matches in-app chat style) */}
        <div
          style={{
            opacity: ceoMsgOpacity,
            transform: `translateY(${ceoMsgSlide}px)`,
            display: "flex",
            gap: 18,
            maxWidth: 960,
            padding: "20px 28px",
            backgroundColor: colors.bgCard,
            borderRadius: 14,
            border: `1px solid ${colors.border}`,
            boxShadow: "0 8px 22px rgba(40,41,42,0.08)",
          }}
        >
          <div
            style={{
              width: 44,
              height: 44,
              background: "#f2f2f3",
              borderRadius: 8,
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              overflow: "hidden",
              flexShrink: 0,
            }}
          >
            <PixelAvatar slug="ceo" color={colors.ceo} size={36} />
          </div>
          <div style={{ flex: 1 }}>
            <div style={{ display: "flex", alignItems: "baseline", gap: 8 }}>
              <span style={{ fontFamily: fonts.sans, fontSize: 18, fontWeight: 700, color: "#28292a" }}>
                CEO
              </span>
              <span
                style={{
                  background: colors.greenBg,
                  color: colors.green,
                  padding: "1px 6px",
                  borderRadius: 3,
                  fontSize: 11,
                  fontWeight: 500,
                }}
              >
                lead
              </span>
            </div>
            <div
              style={{
                fontFamily: fonts.sans,
                fontSize: 19,
                color: "#28292a",
                marginTop: 6,
                lineHeight: 1.45,
              }}
            >
              I&rsquo;ve noticed we prep every sales meeting the same way. Drafting a playbook for the wiki…
            </div>
          </div>
        </div>

        {/* Notebook entry card — reflects the real product feature */}
        <div
          style={{
            opacity: cardOpacity,
            transform: `scale(${cardScale})`,
            width: 760,
            background: "#FAF8F2",
            borderRadius: 14,
            border: `1px solid ${promoted ? colors.green : "#E8E4D8"}`,
            boxShadow: promoted
              ? `0 12px 30px rgba(3, 160, 76, 0.22)`
              : `0 12px 30px rgba(40, 41, 42, 0.10)`,
            overflow: "hidden",
            position: "relative",
          }}
        >
          {/* Notebook meta row */}
          <div
            style={{
              padding: "14px 22px 10px",
              borderBottom: "1px solid #E8E4D8",
              display: "flex",
              alignItems: "center",
              gap: 10,
              fontFamily: fonts.sans,
              fontSize: 12,
              color: "#616061",
            }}
          >
            <div
              style={{
                background: "#F5F1E6",
                border: "1px solid #E8E4D8",
                padding: "2px 8px",
                borderRadius: 999,
                fontSize: 10,
                fontWeight: 700,
                textTransform: "uppercase" as const,
                letterSpacing: "0.08em",
                color: "#616061",
              }}
            >
              Notebook · CEO
            </div>
            <span style={{ color: "#8A8680" }}>draft</span>
            <span style={{ marginLeft: "auto", fontFamily: fonts.mono, fontSize: 11, color: "#8A8680" }}>
              team/playbooks/deal-prep.md
            </span>
          </div>

          {/* Entry title + body */}
          <div style={{ padding: "18px 26px 14px" }}>
            <div
              style={{
                fontFamily: fonts.display,
                fontSize: 30,
                fontWeight: 500,
                color: "#1D1C1D",
                letterSpacing: -0.3,
                lineHeight: 1.15,
                fontVariationSettings: '"opsz" 36',
              }}
            >
              Deal prep playbook
            </div>
            <div
              style={{
                fontFamily: fonts.serif,
                fontStyle: "italic",
                fontSize: 14,
                color: "#616061",
                marginTop: 4,
                marginBottom: 12,
              }}
            >
              Draft, from CEO&rsquo;s notebook.
            </div>
            <div
              style={{
                fontFamily: fonts.serif,
                fontSize: 16,
                lineHeight: 1.6,
                color: "#1D1C1D",
                maxWidth: 620,
              }}
            >
              Pull the company brief, past touchpoints, the buying committee, and likely objections.
              Drop the note in the channel ten minutes before every meeting.
            </div>
          </div>

          {/* Actions footer — real PromoteButton pattern */}
          <div
            style={{
              padding: "10px 18px",
              borderTop: "1px solid #E8E4D8",
              background: "#FFFFFF",
              display: "flex",
              alignItems: "center",
              gap: 10,
            }}
          >
            <span style={{ fontFamily: fonts.sans, fontSize: 12, color: "#616061" }}>
              {promoted ? (
                <span style={{ color: colors.green, fontWeight: 600 }}>Added to team wiki ✓</span>
              ) : pending ? (
                <>Waiting on reviewer…</>
              ) : (
                <>Ready to submit for review.</>
              )}
            </span>
            <div style={{ marginLeft: "auto", display: "flex", gap: 8 }}>
              <div
                style={{
                  padding: "8px 14px",
                  borderRadius: 8,
                  border: "1px solid #E8E4D8",
                  color: "#616061",
                  fontFamily: fonts.sans,
                  fontSize: 13,
                  background: "transparent",
                }}
              >
                Discard entry
              </div>
              <div
                style={{
                  padding: "8px 18px",
                  borderRadius: 8,
                  background: promoted
                    ? colors.green
                    : pending
                    ? "#C9C5B8"
                    : olive[400],
                  color: "#FFFFFF",
                  fontFamily: fonts.sans,
                  fontSize: 13,
                  fontWeight: 600,
                  transform: `scale(${buttonPress})`,
                  boxShadow: buttonGlow
                    ? `0 0 ${buttonGlow * 20}px ${olive[400]}88`
                    : "none",
                  position: "relative",
                }}
              >
                {promoted
                  ? "Added to wiki ✓"
                  : pending
                  ? "Pending review by you"
                  : "Promote to wiki →"}
              </div>
            </div>
          </div>

          {/* Success ripple */}
          {frame >= promotedFrame && (
            <div
              style={{
                position: "absolute",
                top: "50%",
                right: 120,
                width: 260,
                height: 260,
                borderRadius: "50%",
                border: `2px solid ${colors.green}`,
                transform: `translate(-50%, -50%) scale(${rippleScale})`,
                opacity: rippleOpacity,
                pointerEvents: "none" as const,
              }}
            />
          )}
        </div>

        {/* Promoted trail — pill appears below the card */}
        {promoted && (
          <div
            style={{
              opacity: interpolate(frame, [promotedFrame, promotedFrame + 10], [0, 1], {
                extrapolateLeft: "clamp",
                extrapolateRight: "clamp",
              }),
              display: "flex",
              alignItems: "center",
              gap: 10,
              background: colors.bgCard,
              border: `1px solid ${colors.border}`,
              padding: "7px 14px",
              borderRadius: 999,
              fontFamily: fonts.mono,
              fontSize: 13,
              color: colors.text,
              boxShadow: "0 2px 8px rgba(40,41,42,0.06)",
            }}
          >
            <span style={{ color: colors.green }}>+</span>
            <span>team / playbooks / deal-prep.md</span>
            <span style={{ color: colors.textTertiary }}>→ wiki</span>
          </div>
        )}

        {/* Closing tagline */}
        <FadeIn startFrame={sec(5.8)} durationFrames={12} slideUp={10}>
          <div
            style={{
              opacity: taglineOpacity,
              fontFamily: fonts.sans,
              fontSize: 18,
              color: colors.textSecondary,
              textAlign: "center",
              maxWidth: 900,
            }}
          >
            An agent drafts it. You approve. Every other agent can now read it.
          </div>
        </FadeIn>
      </div>
    </AbsoluteFill>
  );
};
