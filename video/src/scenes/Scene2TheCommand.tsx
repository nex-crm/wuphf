import { AbsoluteFill, useCurrentFrame, interpolate, Easing } from "remotion";
import { colors, fonts } from "../theme";
import { Terminal } from "../components/Terminal";
import { TypeWriter } from "../components/TypeWriter";
import { DotGrid } from "../components/DotGrid";

export const Scene2TheCommand: React.FC = () => {
  const frame = useCurrentFrame();

  const terminalOpacity = interpolate(frame, [0, 12], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });
  const terminalScale = interpolate(frame, [0, 12], [0.96, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.out(Easing.cubic),
  });

  // Output after typing
  const outputOpacity = interpolate(frame, [50, 60], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  // Browser slide-up
  const browserOpacity = interpolate(frame, [75, 90], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });
  const browserSlide = interpolate(frame, [75, 105], [300, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.out(Easing.cubic),
  });

  return (
    <AbsoluteFill style={{ backgroundColor: colors.bgBlack }}>
      <DotGrid color="#FFFFFF" opacity={0.04} spacing={40} size={1.2} />
      <div
      style={{
        position: "absolute", inset: 0,
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        padding: 100,
        gap: 30,
      }}
    >
      <div
        style={{
          opacity: terminalOpacity,
          transform: `scale(${terminalScale})`,
          width: 800,
        }}
      >
        <Terminal title="~/my-startup">
          <div style={{ fontSize: 32 }}>
            <span style={{ color: colors.green }}>$</span>{" "}
            <TypeWriter text="./wuphf" startFrame={10} charsPerFrame={0.4} style={{ fontSize: 32 }} />
          </div>

          <div style={{ opacity: outputOpacity, marginTop: 20 }}>
            <div style={{ color: colors.yellow, fontSize: 26 }}>Starting office...</div>
            <div style={{ color: colors.text, fontSize: 26 }}>
              Pack: <span style={{ color: colors.ceo, fontWeight: 700 }}>starter</span>
            </div>
            <div style={{ color: colors.green, fontSize: 26, fontWeight: 600 }}>
              Ready at localhost:7891
            </div>
          </div>
        </Terminal>
      </div>

      {/* Browser preview hint */}
      <div
        style={{
          opacity: browserOpacity,
          transform: `translateY(${browserSlide}px)`,
          width: 800,
          height: 160,
          backgroundColor: colors.bgSidebar,
          borderRadius: 12,
          border: "1px solid #35363A",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          gap: 12,
        }}
      >
        <div style={{ width: 10, height: 10, borderRadius: "50%", backgroundColor: colors.green }} />
        <span style={{ fontFamily: fonts.sans, fontSize: 20, color: colors.textDim }}>
          localhost:7891
        </span>
      </div>
      </div>
    </AbsoluteFill>
  );
};
