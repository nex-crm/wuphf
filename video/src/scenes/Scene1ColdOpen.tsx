import { AbsoluteFill, useCurrentFrame, interpolate, Easing } from "remotion";
import { fonts } from "../theme";
import { DotGrid } from "../components/DotGrid";

const WUPHF_LETTERS = "WUPHF".split("");
const ELEGANT = Easing.bezier(0.25, 0.46, 0.45, 0.94);

export const Scene1ColdOpen: React.FC = () => {
  const frame = useCurrentFrame();

  // Title: per-letter cubic stagger — smooth rise, no overshoot
  const letterAnim = (i: number) => {
    const start = 6 + i * 3;
    const op = interpolate(frame, [start, start + 20], [0, 1], {
      extrapolateLeft: "clamp",
      extrapolateRight: "clamp",
      easing: ELEGANT,
    });
    const slide = interpolate(frame, [start, start + 20], [18, 0], {
      extrapolateLeft: "clamp",
      extrapolateRight: "clamp",
      easing: ELEGANT,
    });
    return { op, slide };
  };

  // Subtitle — slide + fade
  const subOp = interpolate(frame, [36, 54], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: ELEGANT,
  });
  const subSlide = interpolate(frame, [36, 54], [14, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: ELEGANT,
  });

  // Strikethrough — gentle sweep
  const strikeWidth = interpolate(frame, [55, 72], [0, 100], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: ELEGANT,
  });

  // Real subtitle — smooth fade + slide
  const realSubOp = interpolate(frame, [72, 92], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: ELEGANT,
  });
  const realSubSlide = interpolate(frame, [72, 92], [18, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: ELEGANT,
  });

  return (
    <AbsoluteFill style={{ backgroundColor: "#3B145D", overflow: "hidden" }}>
      <DotGrid color="#FFFFFF" opacity={0.05} spacing={40} size={1.2} drift={false} />

      <div style={{
        position: "absolute",
        inset: 0,
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
      }}>
        {/* WUPHF — per-letter stagger with smooth rise */}
        <div style={{
          display: "flex",
          fontFamily: fonts.sans,
          fontSize: 180,
          fontWeight: 900,
          color: "#FFEBFC",
          letterSpacing: -6,
        }}>
          {WUPHF_LETTERS.map((ch, i) => {
            const a = letterAnim(i);
            return (
              <span key={i} style={{
                display: "inline-block",
                opacity: a.op,
                transform: `translateY(${a.slide}px)`,
              }}>
                {ch}
              </span>
            );
          })}
        </div>

        {/* Fake subtitle with strikethrough */}
        <div style={{
          opacity: subOp,
          transform: `translateY(${subSlide}px)`,
          fontFamily: fonts.sans,
          fontSize: 32,
          color: "#9F4DBF",
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
            backgroundColor: "#9F4DBF",
            borderRadius: 2,
          }} />
        </div>

        {/* "The Office" of AI agents — spring-pop entry */}
        <div style={{
          opacity: realSubOp,
          transform: `translateY(${realSubSlide}px)`,
          fontFamily: fonts.sans,
          fontSize: 52,
          color: "#CF72D9",
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
