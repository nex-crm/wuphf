import { AbsoluteFill } from "remotion";
import { colors, fonts } from "./theme";
import { NexSidebar } from "./components/NexSidebar";
import { WuphfLabel } from "./components/WuphfLabel";

// Static Skills-app composition.
// Channel chrome + sidebar match the other WUPHF window compositions.

const NEX = {
  bg: "#FFFFFF",
  border: "#e9eaeb",
  borderLight: "#f2f2f3",
  text: "#28292a",
  textSecondary: "#686c6e",
  textTertiary: "#85898b",
};

const trafficDot = (color: string): React.CSSProperties => ({
  width: 13,
  height: 13,
  borderRadius: "50%",
  background: color,
  display: "inline-block",
});

const SKILLS = [
  {
    name: "Pilot Onboarding Builder",
    desc: "Turn a customer pilot goal into channels, owners, success criteria, and first-week tasks.",
  },
  {
    name: "Workspace Readiness Pass",
    desc: "Check whether a WUPHF workspace is clear, governed, and ready for a customer team.",
  },
  {
    name: "Release Readiness Sweep",
    desc: "Coordinate PM, engineering, design, and GTM checks before a launch or customer handoff.",
  },
  {
    name: "Customer Follow-up Draft",
    desc: "Draft a concise customer follow-up that ties WUPHF work to value, decisions, and next steps.",
  },
  {
    name: "Call Notes to Tasks",
    desc: "Convert customer call notes into concrete product, success, and sales tasks.",
  },
];

const BoltIcon: React.FC<{ size?: number; color?: string }> = ({ size = 18, color = "#E8B44A" }) => (
  <svg width={size} height={size} viewBox="0 0 24 24" fill={color} aria-hidden="true">
    <path d="M13 3 4 14h7l-1 7 9-11h-7l1-7Z" />
  </svg>
);

export const WuphfSkills: React.FC = () => {
  return (
    <AbsoluteFill
      style={{
        background: "#FFB3E6",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        position: "relative",
      }}
    >
      <WuphfLabel>Skills</WuphfLabel>
      <div
        style={{
          width: 1360,
          height: 1000,
          transform: "scale(0.8)",
          transformOrigin: "center",
          background: "#FFCFF1",
          borderRadius: 20,
          padding: 4,
          overflow: "hidden",
          boxShadow:
            "0 0 0 1px rgba(0,0,0,0.05), 0 40px 100px rgba(66, 26, 104, 0.35), 0 12px 32px rgba(0,0,0,0.12)",
          display: "flex",
          flexDirection: "column",
          willChange: "transform",
          backfaceVisibility: "hidden",
        }}
      >
        {/* Titlebar */}
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 10,
            height: 40,
            padding: "0 14px",
            background: "#FFCFF1",
            flexShrink: 0,
          }}
        >
          <div style={{ display: "flex", gap: 8 }}>
            <span style={trafficDot("#ff5f57")} />
            <span style={trafficDot("#febc2e")} />
            <span style={trafficDot("#28c840")} />
          </div>
          <span
            style={{
              flex: 1,
              textAlign: "center",
              fontFamily: fonts.sans,
              fontSize: 12,
              color: "#686c6e",
            }}
          >
            wuphf.app — Skills
          </span>
          <span style={{ width: 54 }} />
        </div>

        <div
          style={{
            flex: 1,
            display: "flex",
            minHeight: 0,
            borderRadius: 16,
            overflow: "hidden",
          }}
        >
          <NexSidebar active={{ kind: "app", slug: "skills" }} />

          {/* Main Skills area */}
          <div
            style={{
              flex: 1,
              display: "flex",
              flexDirection: "column",
              overflow: "hidden",
              background: NEX.bg,
            }}
          >
            {/* Channel header */}
            <div
              style={{
                display: "flex",
                alignItems: "center",
                height: 56,
                padding: "0 24px",
                borderBottom: `1px solid ${NEX.border}`,
                background: "rgba(255,255,255,0.8)",
                flexShrink: 0,
              }}
            >
              <span
                style={{
                  fontSize: 16,
                  fontWeight: 700,
                  color: NEX.text,
                  fontFamily: fonts.sans,
                }}
              >
                Skills
              </span>
            </div>

            {/* Runtime strip */}
            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: 8,
                height: 28,
                padding: "0 20px",
                borderBottom: `1px solid ${NEX.borderLight}`,
                flexShrink: 0,
                fontFamily: fonts.sans,
              }}
            >
              <span
                style={{
                  display: "inline-flex",
                  alignItems: "center",
                  padding: "2px 8px",
                  borderRadius: 999,
                  background: "#ffe4e0",
                  color: "#8c1727",
                  fontSize: 10,
                  fontWeight: 700,
                  textTransform: "uppercase" as const,
                  letterSpacing: "0.06em",
                }}
              >
                1 blocked
              </span>
            </div>

            {/* Skills list */}
            <div
              style={{
                flex: 1,
                overflow: "auto",
                padding: "24px 32px 40px",
                fontFamily: fonts.sans,
              }}
            >
              {/* Section header */}
              <div
                style={{
                  fontSize: 18,
                  fontWeight: 700,
                  color: NEX.text,
                  paddingBottom: 12,
                  borderBottom: `1px solid ${NEX.border}`,
                  marginBottom: 20,
                }}
              >
                Skills
              </div>

              <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
                {SKILLS.map((s) => (
                  <div
                    key={s.name}
                    style={{
                      background: NEX.bg,
                      border: `1px solid ${NEX.border}`,
                      borderRadius: 10,
                      padding: "18px 22px",
                      display: "flex",
                      flexDirection: "column",
                      gap: 10,
                    }}
                  >
                    <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
                      <BoltIcon size={18} />
                      <span
                        style={{
                          fontSize: 15,
                          fontWeight: 700,
                          color: NEX.text,
                        }}
                      >
                        {s.name}
                      </span>
                    </div>
                    <div
                      style={{
                        fontSize: 14,
                        lineHeight: 1.5,
                        color: NEX.textSecondary,
                      }}
                    >
                      {s.desc}
                    </div>
                    <div>
                      <button
                        style={{
                          display: "inline-flex",
                          alignItems: "center",
                          gap: 6,
                          background: colors.accent,
                          color: "#FFFFFF",
                          border: "none",
                          borderRadius: 8,
                          padding: "8px 14px",
                          fontFamily: fonts.sans,
                          fontSize: 13,
                          fontWeight: 600,
                          cursor: "pointer",
                        }}
                      >
                        <BoltIcon size={14} color="#FFFFFF" />
                        Invoke
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            </div>

            {/* Status bar */}
            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: 14,
                height: 36,
                padding: "0 20px",
                borderTop: `1px solid ${NEX.border}`,
                background: NEX.bg,
                fontFamily: fonts.mono,
                fontSize: 11,
                color: NEX.textTertiary,
                flexShrink: 0,
              }}
            >
              <span>skills</span>
              <span>office</span>
              <span style={{ flex: 1 }} />
              <span
                style={{
                  display: "inline-flex",
                  alignItems: "center",
                  gap: 5,
                  padding: "1px 6px",
                  border: `1px solid ${NEX.border}`,
                  borderRadius: 4,
                }}
              >
                ?
              </span>
              <span>shortcuts</span>
              <span>7 agents</span>
              <span>⚙ codex · gpt-5.4</span>
              <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
                <span
                  style={{
                    width: 7,
                    height: 7,
                    borderRadius: "50%",
                    background: "#03a04c",
                  }}
                />
                connected
              </span>
            </div>
          </div>
        </div>
      </div>
    </AbsoluteFill>
  );
};
