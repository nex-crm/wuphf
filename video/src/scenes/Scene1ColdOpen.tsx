import { AbsoluteFill, useCurrentFrame, interpolate, Easing, spring } from "remotion";
import { colors, fonts, slack, FPS } from "../theme";
import { DotGrid, RadialGlow } from "../components/DotGrid";

export const Scene1ColdOpen: React.FC = () => {
  const frame = useCurrentFrame();

  // Title fades in gracefully (no shake, no bounce)
  const titleOp = interpolate(frame, [8, 22], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.out(Easing.cubic),
  });
  const titleScale = interpolate(frame, [8, 22], [0.94, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.out(Easing.cubic),
  });

  // Subtitle fades in
  const subOp = interpolate(frame, [38, 52], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  // Strikethrough sweeps in — no wobble
  const strikeWidth = interpolate(frame, [55, 70], [0, 100], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.out(Easing.cubic),
  });

  // Real subtitle fades in gracefully — stays visible for full remaining scene
  const realSubOp = interpolate(frame, [72, 88], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.out(Easing.cubic),
  });
  const realSubSlide = interpolate(frame, [72, 88], [14, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.out(Easing.cubic),
  });

  return (
    <AbsoluteFill style={{ backgroundColor: colors.bgBlack, overflow: "hidden" }}>
      <DotGrid color="#FFFFFF" opacity={0.05} spacing={40} size={1.2} drift={false} />
      <RadialGlow color={slack.sidebar} x="50%" y="50%" size={1400} opacity={0.35} />

      <div style={{
        position: "absolute",
        inset: 0,
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
      }}>
        {/* WUPHF — no shake, settled */}
        <div style={{
          opacity: titleOp,
          transform: `scale(${titleScale})`,
          fontFamily: fonts.sans,
          fontSize: 180,
          fontWeight: 900,
          color: "#FFF",
          letterSpacing: -6,
          textShadow: `0 0 80px ${slack.sidebar}`,
        }}>
          WUPHF
        </div>

        {/* Fake subtitle with strikethrough */}
        <div style={{
          opacity: subOp,
          fontFamily: fonts.sans,
          fontSize: 32,
          color: colors.textMuted,
          marginTop: 20,
          position: "relative",
        }}>
          Washington University Public Health Fund
          <div style={{
            position: "absolute",
            top: "50%",
            left: 0,
            width: `${strikeWidth}%`,
            height: 4,
            backgroundColor: slack.red,
            borderRadius: 2,
          }} />
        </div>

        {/* "The Office" of AI agents — stays on screen for full remaining duration */}
        <div style={{
          opacity: realSubOp,
          transform: `translateY(${realSubSlide}px)`,
          fontFamily: fonts.sans,
          fontSize: 52,
          color: colors.yellow,
          marginTop: 36,
          fontStyle: "italic",
          fontWeight: 700,
        }}>
          "The Office" of AI agents.
        </div>
      </div>
    </AbsoluteFill>
  );
};
