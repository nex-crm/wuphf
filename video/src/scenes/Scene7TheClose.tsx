import { AbsoluteFill, useCurrentFrame, interpolate, Easing } from "remotion";
import { colors, fonts, sec } from "../theme";
import { Terminal } from "../components/Terminal";
import { TypeWriter } from "../components/TypeWriter";

const ELEGANT = Easing.bezier(0.25, 0.46, 0.45, 0.94);

export const Scene7TheClose: React.FC = () => {
  const frame = useCurrentFrame();

  const termOpacity = interpolate(frame, [0, 18], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: ELEGANT,
  });

  // Each row expands smoothly from 0 → 1 — the growing height pushes the
  // column so `justifyContent: center` re-centers the composition each frame.
  const row = (start: number, end: number) =>
    interpolate(frame, [start, end], [0, 1], {
      extrapolateLeft: "clamp",
      extrapolateRight: "clamp",
      easing: ELEGANT,
    });

  const tagline = row(sec(3), sec(4));
  const punch = row(sec(4.5), sec(5.5));
  const cta = row(sec(6), sec(7));

  return (
    <AbsoluteFill style={{ backgroundColor: "#3B145D" }}>
      <div style={{
        position: "absolute", inset: 0,
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        padding: 100,
        transform: "scale(1.2)",
        transformOrigin: "center",
        willChange: "transform",
      }}>
      <div style={{ opacity: termOpacity, width: 820 }}>
        <Terminal title="Get started">
          <div style={{ fontSize: 18, lineHeight: 1.8 }}>
            <div>
              <span style={{ color: colors.green }}>$</span>{" "}
              <TypeWriter
                text="git clone https://github.com/nex-crm/wuphf.git"
                startFrame={5}
                charsPerFrame={1.4}
                style={{ fontSize: 18, color: colors.textBright }}
              />
            </div>
            <div>
              <span style={{ color: colors.green }}>$</span>{" "}
              <TypeWriter
                text="cd wuphf && ./wuphf"
                startFrame={45}
                charsPerFrame={1.4}
                style={{ fontSize: 18, color: colors.textBright }}
              />
            </div>
          </div>
        </Terminal>
      </div>

      {/* Tagline — expands height so the column pushes apart as it appears */}
      <div style={{
        overflow: "hidden",
        maxHeight: tagline * 140,
        marginTop: tagline * 28,
      }}>
        <div
          style={{
            opacity: tagline,
            transform: `translateY(${(1 - tagline) * 10}px)`,
            fontFamily: fonts.sans,
            fontSize: 48,
            fontWeight: 800,
            color: "#FFEBFC",
            textAlign: "center",
            letterSpacing: -1,
          }}
        >
          Open source. Self-hosted. MIT.
        </div>
      </div>

      {/* Punch line — two rows */}
      <div style={{
        overflow: "hidden",
        maxHeight: punch * 140,
        marginTop: punch * 28,
      }}>
        <div
          style={{
            opacity: punch,
            transform: `translateY(${(1 - punch) * 10}px)`,
            fontFamily: fonts.sans,
            fontSize: 28,
            color: "#CF72D9",
            textAlign: "center",
            fontStyle: "italic",
            lineHeight: 1.5,
          }}
        >
          Named after Ryan Howard's worst idea.
          <br />
          Turns out it was his best one.
        </div>
      </div>

      {/* CTA pill */}
      <div style={{
        overflow: "hidden",
        maxHeight: cta * 120,
        marginTop: cta * 28,
      }}>
        <div
          style={{
            opacity: cta,
            transform: `translateY(${(1 - cta) * 10}px)`,
            padding: "16px 36px",
            borderRadius: 14,
            backgroundColor: "rgba(255,235,252,0.06)",
            fontFamily: fonts.sans,
            fontSize: 24,
            fontWeight: 600,
            color: "#FFEBFC",
            border: "1px solid #9F4DBF",
          }}
        >
          github.com/nex-crm/wuphf
        </div>
      </div>
      </div>
    </AbsoluteFill>
  );
};
