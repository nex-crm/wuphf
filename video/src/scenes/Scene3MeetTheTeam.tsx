import { AbsoluteFill, useCurrentFrame, interpolate, Easing, spring } from "remotion";
import { colors, fonts, sec, FPS, slack } from "../theme";
import { DotGrid, RadialGlow } from "../components/DotGrid";
import { PixelAvatar } from "../components/PixelAvatar";

const packDisplays = [
  {
    name: "Starter Team",
    label: "01",
    agents: [
      { slug: "ceo", color: colors.ceo, name: "CEO" },
      { slug: "eng", color: colors.eng, name: "Engineer" },
      { slug: "gtm", color: colors.gtm, name: "GTM Lead" },
    ],
  },
  {
    name: "Founding Team",
    label: "02",
    agents: [
      { slug: "ceo", color: colors.ceo, name: "CEO" },
      { slug: "pm", color: colors.pm, name: "PM" },
      { slug: "fe", color: colors.fe, name: "FE" },
      { slug: "be", color: colors.be, name: "BE" },
      { slug: "ai", color: colors.ai, name: "AI" },
      { slug: "designer", color: colors.designer, name: "Design" },
      { slug: "cmo", color: colors.cmo, name: "CMO" },
      { slug: "cro", color: colors.cro, name: "CRO" },
    ],
  },
  {
    name: "Coding Team",
    label: "03",
    agents: [
      { slug: "ceo", color: "#8B5CF6", name: "Lead" },
      { slug: "fe", color: colors.fe, name: "FE" },
      { slug: "be", color: colors.be, name: "BE" },
      { slug: "be", color: "#22C55E", name: "QA" },
    ],
  },
  {
    name: "Lead Gen Agency",
    label: "04",
    agents: [
      { slug: "cro", color: colors.cro, name: "AE" },
      { slug: "pm", color: colors.gtm, name: "SDR" },
      { slug: "ai", color: colors.ai, name: "Research" },
      { slug: "designer", color: colors.designer, name: "Content" },
    ],
  },
];

export const Scene3MeetTheTeam: React.FC = () => {
  const frame = useCurrentFrame();

  const packDuration = sec(1.8);
  const packIndex = Math.min(3, Math.floor(frame / packDuration));
  const packLocal = frame - packIndex * packDuration;
  const currentPack = packDisplays[packIndex];

  const packOpacity = interpolate(
    packLocal, [0, 8, packDuration - 8, packDuration], [0, 1, 1, 0],
    { extrapolateLeft: "clamp", extrapolateRight: "clamp" }
  );

  const packSlide = interpolate(packLocal, [0, 10], [30, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.out(Easing.cubic),
  });

  const customOpacity = interpolate(frame, [sec(5.8), sec(6.3)], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  return (
    <AbsoluteFill style={{ backgroundColor: "#0B0D10" }}>
      <DotGrid color="#FFFFFF" opacity={0.04} spacing={40} size={1.2} />
      <RadialGlow color={slack.sidebar} x="50%" y="50%" size={1200} opacity={0.25} />

      {/* Overall title */}
      <div style={{
        position: "absolute", top: 120, left: 0, right: 0,
        display: "flex", justifyContent: "center",
      }}>
        <div style={{
          fontFamily: fonts.mono, fontSize: 22, color: slack.textTertiary,
          textTransform: "uppercase" as const, letterSpacing: 10, fontWeight: 600,
        }}>
          Team Packs
        </div>
      </div>

      <div style={{
        position: "absolute", inset: 0,
        display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", gap: 28,
      }}>

        {/* Pack counter + name */}
        <div style={{
          opacity: packOpacity,
          display: "flex", flexDirection: "column", alignItems: "center", gap: 6,
        }}>
          <div style={{
            fontFamily: fonts.mono, fontSize: 13, color: slack.accent,
            letterSpacing: 4, textTransform: "uppercase" as const,
          }}>
            {currentPack.label} / {packDisplays.length.toString().padStart(2, "0")}
          </div>
          <span style={{
            fontFamily: fonts.sans, fontSize: 52, fontWeight: 800, color: colors.textBright,
            textTransform: "uppercase" as const, letterSpacing: 3,
          }}>
            {currentPack.name}
          </span>
        </div>

        {/* Agent avatars — pixel art from platform sprite system */}
        <div style={{
          opacity: packOpacity,
          transform: `translateY(${packSlide}px)`,
          display: "flex", gap: 20, justifyContent: "center", flexWrap: "wrap", maxWidth: 900,
        }}>
          {currentPack.agents.map((agent, i) => {
            const agentScale = spring({
              frame: Math.max(0, packLocal - 4 - i * 3),
              fps: FPS,
              config: { damping: 12, stiffness: 200 },
            });

            return (
              <div key={`${packIndex}-${i}`} style={{
                transform: `scale(${agentScale})`,
                display: "flex", flexDirection: "column", alignItems: "center", gap: 10,
              }}>
                <div style={{
                  width: 100, height: 100, borderRadius: 16,
                  backgroundColor: `${agent.color}18`,
                  border: `1.5px solid ${agent.color}35`,
                  display: "flex", alignItems: "center", justifyContent: "center",
                }}>
                  <PixelAvatar slug={agent.slug} color={agent.color} size={72} />
                </div>
                <div style={{ fontFamily: fonts.sans, fontSize: 20, fontWeight: 600, color: colors.textBright, textAlign: "center" }}>
                  {agent.name}
                </div>
              </div>
            );
          })}
        </div>

        {/* Pagination dots */}
        <div style={{ display: "flex", gap: 8, marginTop: 4 }}>
          {packDisplays.map((_, i) => (
            <div key={i} style={{
              width: i === packIndex ? 28 : 10, height: 10, borderRadius: 5,
              backgroundColor: i === packIndex ? slack.accent : "#444",
            }} />
          ))}
        </div>

        {/* "or build your own" */}
        <div style={{
          opacity: customOpacity,
          display: "flex", flexDirection: "column", alignItems: "center", gap: 8,
        }}>
          <div style={{
            fontFamily: fonts.sans, fontSize: 38, fontWeight: 700, color: colors.yellow, fontStyle: "italic",
          }}>
            ...or build your own
          </div>
          <div style={{
            fontFamily: fonts.sans, fontSize: 22, color: colors.textDim, fontStyle: "italic",
          }}>
            They are remarkably unbothered either way.
          </div>
        </div>
      </div>
    </AbsoluteFill>
  );
};
