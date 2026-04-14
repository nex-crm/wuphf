import { AbsoluteFill, useCurrentFrame, interpolate, Easing, spring } from "remotion";
import { colors, fonts, sec, FPS, slack } from "../theme";
import { Terminal } from "../components/Terminal";
import { TypeWriter } from "../components/TypeWriter";
import { RadialGlow } from "../components/DotGrid";

export const Scene7TheClose: React.FC = () => {
  const frame = useCurrentFrame();

  const termOpacity = interpolate(frame, [0, 12], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  const taglineOpacity = interpolate(frame, [sec(3), sec(3.8)], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  const punchOpacity = interpolate(frame, [sec(4.5), sec(5.3)], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  const ctaScale = spring({
    frame: Math.max(0, frame - sec(6)),
    fps: FPS,
    config: { damping: 15, stiffness: 180 },
  });
  const ctaOpacity = interpolate(frame, [sec(6), sec(6.5)], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  return (
    <AbsoluteFill style={{ backgroundColor: "#0B0D10" }}>
      <RadialGlow color={slack.sidebar} x="50%" y="30%" size={900} opacity={0.25} />
      <div style={{
        position: "absolute", inset: 0,
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        gap: 28,
        padding: 100,
      }}>
      <div style={{ opacity: termOpacity, width: 1100 }}>
        <Terminal title="Get started">
          <div style={{ fontSize: 18, lineHeight: 1.8 }}>
            <div>
              <span style={{ color: colors.green }}>$</span>{" "}
              <TypeWriter
                text="git clone https://github.com/nex-crm/wuphf.git"
                startFrame={5}
                charsPerFrame={1.4}
                style={{ fontSize: 18 }}
              />
            </div>
            <div>
              <span style={{ color: colors.green }}>$</span>{" "}
              <TypeWriter
                text="cd wuphf && ./wuphf"
                startFrame={45}
                charsPerFrame={1.4}
                style={{ fontSize: 18 }}
              />
            </div>
          </div>
        </Terminal>
      </div>

      <div
        style={{
          opacity: taglineOpacity,
          fontFamily: fonts.sans,
          fontSize: 48,
          fontWeight: 800,
          color: colors.textBright,
          textAlign: "center",
          letterSpacing: -1,
        }}
      >
        Open source. Self-hosted. MIT.
      </div>

      <div
        style={{
          opacity: punchOpacity,
          fontFamily: fonts.sans,
          fontSize: 28,
          color: colors.yellow,
          textAlign: "center",
          fontStyle: "italic",
          lineHeight: 1.5,
        }}
      >
        Named after Ryan Howard's worst idea.
        <br />
        Turns out it was his best one.
      </div>

      <div
        style={{
          opacity: ctaOpacity,
          transform: `scale(${ctaScale})`,
          padding: "16px 36px",
          borderRadius: 14,
          backgroundColor: "#222",
          fontFamily: fonts.sans,
          fontSize: 24,
          fontWeight: 600,
          color: "#FFF",
          border: "1px solid #444",
        }}
      >
        github.com/nex-crm/wuphf
      </div>
      </div>
    </AbsoluteFill>
  );
};
