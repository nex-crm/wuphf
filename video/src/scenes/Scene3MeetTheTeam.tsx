import { AbsoluteFill, useCurrentFrame, interpolate, Easing, spring } from "remotion";
import { colors, fonts, packs, sec, FPS, agentEmojis, slack } from "../theme";
import { DotGrid, RadialGlow } from "../components/DotGrid";

const packDisplays = [
  {
    name: "Starter Team",
    emoji: "🚀",
    agents: [
      { emoji: "👔", color: colors.ceo, name: "CEO" },
      { emoji: "⚙️", color: colors.eng, name: "Engineer" },
      { emoji: "💰", color: colors.gtm, name: "GTM Lead" },
    ],
  },
  {
    name: "Founding Team",
    emoji: "🏢",
    agents: [
      { emoji: "👔", color: colors.ceo, name: "CEO" },
      { emoji: "📋", color: colors.pm, name: "PM" },
      { emoji: "🎨", color: colors.fe, name: "FE" },
      { emoji: "⚙️", color: colors.be, name: "BE" },
      { emoji: "🧠", color: colors.ai, name: "AI" },
      { emoji: "✏️", color: colors.designer, name: "Design" },
      { emoji: "📣", color: colors.cmo, name: "CMO" },
      { emoji: "💰", color: colors.cro, name: "CRO" },
    ],
  },
  {
    name: "Coding Team",
    emoji: "💻",
    agents: [
      { emoji: "👔", color: "#8B5CF6", name: "Lead" },
      { emoji: "🎨", color: colors.fe, name: "FE" },
      { emoji: "⚙️", color: colors.be, name: "BE" },
      { emoji: "🧪", color: "#22C55E", name: "QA" },
    ],
  },
  {
    name: "Lead Gen Agency",
    emoji: "📈",
    agents: [
      { emoji: "🤝", color: colors.cro, name: "AE" },
      { emoji: "📞", color: colors.gtm, name: "SDR" },
      { emoji: "🔍", color: colors.ai, name: "Research" },
      { emoji: "📝", color: colors.designer, name: "Content" },
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

  const customOpacity = interpolate(frame, [sec(7), sec(7.5)], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  return (
    <AbsoluteFill style={{ backgroundColor: "#0B0D10" }}>
      <DotGrid color="#FFFFFF" opacity={0.04} spacing={40} size={1.2} />
      <RadialGlow color={slack.sidebar} x="50%" y="50%" size={1200} opacity={0.25} />
      {/* Overall title — present throughout pack cycling */}
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
      {/* Pack name with emoji */}
      <div style={{
        opacity: packOpacity,
        display: "flex", alignItems: "center", gap: 18,
      }}>
        <span style={{ fontSize: 56 }}>{currentPack.emoji}</span>
        <span style={{
          fontFamily: fonts.sans, fontSize: 44, fontWeight: 700, color: colors.textBright,
          textTransform: "uppercase" as const, letterSpacing: 4,
        }}>
          {currentPack.name}
        </span>
      </div>

      {/* Agent avatars with emojis */}
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
                width: 110, height: 110, borderRadius: 22,
                backgroundColor: agent.color,
                display: "flex", alignItems: "center", justifyContent: "center",
                fontSize: 54,
                boxShadow: `0 0 40px ${agent.color}30`,
              }}>
                {agent.emoji}
              </div>
              <div style={{ fontFamily: fonts.sans, fontSize: 22, fontWeight: 600, color: colors.textBright, textAlign: "center" }}>
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
