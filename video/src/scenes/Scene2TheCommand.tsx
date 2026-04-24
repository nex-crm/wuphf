import { AbsoluteFill, useCurrentFrame, interpolate, Easing } from "remotion";
import { colors, fonts } from "../theme";
import { Terminal } from "../components/Terminal";
import { TypeWriter } from "../components/TypeWriter";

export const Scene2TheCommand: React.FC = () => {
  const frame = useCurrentFrame();

  const ELEGANT = Easing.bezier(0.25, 0.46, 0.45, 0.94);

  // Terminal enters from the bottom already at its zoomed-in size,
  // waits during typing, then smoothly zooms out to default size.
  const terminalOpacity = interpolate(frame, [0, 18], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: ELEGANT,
  });
  const terminalSlide = interpolate(frame, [0, 24], [360, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: ELEGANT,
  });
  // Holds at 1.2× through entry + typing (ends ~frame 30), then eases to 1.0
  const zoom = interpolate(frame, [0, 40, 60], [1.2, 1.2, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: ELEGANT,
  });

  // Output rows appear one-by-one
  const row = (start: number) => {
    const op = interpolate(frame, [start, start + 12], [0, 1], {
      extrapolateLeft: "clamp",
      extrapolateRight: "clamp",
      easing: ELEGANT,
    });
    const slide = interpolate(frame, [start, start + 12], [6, 0], {
      extrapolateLeft: "clamp",
      extrapolateRight: "clamp",
      easing: ELEGANT,
    });
    return { opacity: op, transform: `translateY(${slide}px)` };
  };
  const row1 = row(46);
  const row2 = row(58);
  const row3 = row(70);

  // Each row collapses to 0 height until it appears, then expands smoothly.
  // This makes the terminal fit only its visible content.
  const rowH = (start: number) =>
    interpolate(frame, [start, start + 8], [0, 52], {
      extrapolateLeft: "clamp",
      extrapolateRight: "clamp",
      easing: ELEGANT,
    });
  const h1 = rowH(46);
  const h2 = rowH(58);
  const h3 = rowH(70);
  // Output-block margin-top grows with the first row
  const outputMt = interpolate(frame, [46, 54], [0, 20], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: ELEGANT,
  });

  // Pill slides in from above
  const browserOpacity = interpolate(frame, [75, 92], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: ELEGANT,
  });
  const browserSlide = interpolate(frame, [75, 100], [-260, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: ELEGANT,
  });

  return (
    <AbsoluteFill style={{ backgroundColor: "#3B145D" }}>
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
          transform: `translateY(${terminalSlide}px) scale(${zoom})`,
          transformOrigin: "center 30%",
          width: 800,
          position: "relative",
          zIndex: 2,
        }}
      >
        <Terminal title="~/my-startup">
          <div style={{ fontSize: 38, lineHeight: 1.4 }}>
            <span style={{ color: "#969640" }}>$</span>{" "}
            <TypeWriter text="./wuphf" startFrame={10} charsPerFrame={0.4} style={{ color: "#D4DB18", fontSize: 38 }} />
          </div>

          <div style={{ marginTop: outputMt }}>
            <div style={{ height: h1, overflow: "hidden" }}>
              <div style={row1}>Starting office...</div>
            </div>
            <div style={{ height: h2, overflow: "hidden" }}>
              <div style={row2}>
                Pack: <span style={{ color: "#cfd1d2", fontWeight: 600 }}>starter</span>
              </div>
            </div>
            <div style={{ height: h3, overflow: "hidden" }}>
              <div style={{ ...row3, color: "#cfd1d2" }}>
                Ready at localhost:7891
              </div>
            </div>
          </div>
        </Terminal>
      </div>

      {/* Localhost pill — slides down from behind the terminal */}
      <div
        style={{
          opacity: browserOpacity,
          transform: `translateY(${browserSlide}px)`,
          display: "inline-flex",
          alignItems: "center",
          gap: 12,
          padding: "12px 26px",
          backgroundColor: "#FFFFFF",
          borderRadius: 9999,
          zIndex: 1,
          marginTop: 32,
        }}
      >
        <div style={{ width: 10, height: 10, borderRadius: "50%", backgroundColor: "#03a04c" }} />
        <span style={{ fontFamily: fonts.sans, fontSize: 26, fontWeight: 400, color: "#000000" }}>
          localhost:7891
        </span>
      </div>
      </div>
    </AbsoluteFill>
  );
};
