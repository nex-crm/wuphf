import { AbsoluteFill, Easing, interpolate, spring, useCurrentFrame } from "remotion";
import { FPS, colors, cyan, fonts, neutral, olive, packs, sec, tertiary } from "../../theme";
import { DotGrid, RadialGlow } from "../../components/DotGrid";
import { PixelAvatar } from "../../components/PixelAvatar";

// Per-pack mini roster so the cards don't feel empty.
const PACK_ROSTERS: Record<string, Array<{ slug: string; color: string }>> = {
  "Starter Team": [
    { slug: "ceo", color: colors.ceo },
    { slug: "eng", color: colors.eng },
    { slug: "gtm", color: colors.gtm },
  ],
  "Founding Team": [
    { slug: "ceo", color: colors.ceo },
    { slug: "gtm", color: colors.gtm },
    { slug: "eng", color: colors.eng },
    { slug: "pm", color: colors.pm },
    { slug: "designer", color: colors.designer },
  ],
  "Coding Team": [
    { slug: "eng", color: colors.eng },
    { slug: "fe", color: colors.fe },
    { slug: "be", color: colors.be },
    { slug: "ai", color: colors.ai },
  ],
  "Lead Gen Agency": [
    { slug: "gtm", color: colors.gtm },
    { slug: "cro", color: colors.cro },
    { slug: "pm", color: colors.pm },
    { slug: "cmo", color: colors.cmo },
  ],
};

export const Scene3PickAPack: React.FC = () => {
  const frame = useCurrentFrame();

  const titleOpacity = interpolate(frame, [0, 10], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });
  const titleSlide = interpolate(frame, [0, 14], [12, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.out(Easing.cubic),
  });

  // Which card the user lands on — we zoom in on "Founding Team" to match
  // the canonical demo.
  const pickedIndex = 1;
  const pickStart = sec(4.0);
  const pickScale = spring({
    frame: Math.max(0, frame - pickStart),
    fps: FPS,
    config: { damping: 15, stiffness: 160 },
  });

  return (
    <AbsoluteFill style={{ backgroundColor: colors.bg }}>
      <DotGrid color={tertiary[400]} opacity={0.05} spacing={40} size={1.2} />
      <RadialGlow color={cyan[400]} x="50%" y="45%" size={1200} opacity={0.1} />

      <div
        style={{
          position: "absolute",
          inset: 0,
          padding: 80,
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          gap: 48,
        }}
      >
        <div
          style={{
            opacity: titleOpacity,
            transform: `translateY(${titleSlide}px)`,
            textAlign: "center",
          }}
        >
          <div
            style={{
              fontFamily: fonts.sans,
              fontSize: 20,
              fontWeight: 700,
              letterSpacing: "0.22em",
              textTransform: "uppercase",
              color: colors.textTertiary,
              marginBottom: 14,
            }}
          >
            Pick a pack
          </div>
          <div
            style={{
              fontFamily: fonts.sans,
              fontSize: 72,
              fontWeight: 800,
              color: colors.text,
              letterSpacing: -1.5,
              lineHeight: 1.05,
            }}
          >
            Your office, out of the box.
          </div>
        </div>

        {/* Cards */}
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(4, 1fr)",
            gap: 20,
            width: 1620,
            marginTop: 20,
          }}
        >
          {packs.map((p, i) => {
            const appearAt = 14 + i * 6;
            const opacity = interpolate(frame, [appearAt, appearAt + 10], [0, 1], {
              extrapolateLeft: "clamp",
              extrapolateRight: "clamp",
            });
            const translate = interpolate(frame, [appearAt, appearAt + 14], [26, 0], {
              extrapolateLeft: "clamp",
              extrapolateRight: "clamp",
              easing: Easing.out(Easing.cubic),
            });
            const picked = i === pickedIndex;
            const cardScale = picked
              ? 1 + 0.04 * pickScale
              : frame > pickStart
              ? interpolate(frame, [pickStart, pickStart + 12], [1, 0.96], {
                  extrapolateLeft: "clamp",
                  extrapolateRight: "clamp",
                })
              : 1;
            const cardDim = picked
              ? 1
              : frame > pickStart
              ? interpolate(frame, [pickStart, pickStart + 12], [1, 0.55], {
                  extrapolateLeft: "clamp",
                  extrapolateRight: "clamp",
                })
              : 1;

            const roster = PACK_ROSTERS[p.name] ?? [];

            return (
              <div
                key={p.name}
                style={{
                  opacity: opacity * cardDim,
                  transform: `translateY(${translate}px) scale(${cardScale})`,
                  background: colors.bgCard,
                  border: `1px solid ${picked ? tertiary[400] : colors.border}`,
                  boxShadow: picked
                    ? `0 20px 40px rgba(159, 77, 191, 0.20)`
                    : `0 4px 18px rgba(40, 41, 42, 0.06)`,
                  borderRadius: 20,
                  padding: 28,
                  display: "flex",
                  flexDirection: "column",
                  gap: 16,
                  minHeight: 320,
                  position: "relative",
                }}
              >
                <div style={{ display: "flex", gap: 10, alignItems: "center" }}>
                  <div
                    style={{
                      fontSize: 42,
                      lineHeight: 1,
                      filter: picked ? "none" : "grayscale(0.2)",
                    }}
                  >
                    {p.emoji}
                  </div>
                  <div
                    style={{
                      marginLeft: "auto",
                      fontFamily: fonts.mono,
                      fontSize: 11,
                      color: colors.textTertiary,
                      background: colors.bgWarm,
                      padding: "3px 8px",
                      borderRadius: 999,
                    }}
                  >
                    {p.agents} agents
                  </div>
                </div>

                <div
                  style={{
                    fontFamily: fonts.sans,
                    fontSize: 26,
                    fontWeight: 700,
                    color: colors.text,
                    letterSpacing: -0.4,
                  }}
                >
                  {p.name}
                </div>
                <div
                  style={{
                    fontFamily: fonts.sans,
                    fontSize: 15,
                    color: colors.textSecondary,
                    lineHeight: 1.45,
                  }}
                >
                  {p.desc}
                </div>

                {/* Roster row */}
                <div style={{ marginTop: "auto", display: "flex", gap: 6, alignItems: "center" }}>
                  {roster.slice(0, 5).map((r) => (
                    <div
                      key={r.slug}
                      style={{
                        width: 34,
                        height: 34,
                        background: colors.bgWarm,
                        borderRadius: 7,
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "center",
                        overflow: "hidden",
                      }}
                    >
                      <PixelAvatar slug={r.slug} color={r.color} size={28} />
                    </div>
                  ))}
                </div>

                {/* "Picked" overlay on the chosen card */}
                {picked && frame > pickStart + 4 && (
                  <div
                    style={{
                      position: "absolute",
                      right: 16,
                      top: 16,
                      background: olive[300],
                      color: olive[500],
                      fontSize: 11,
                      fontWeight: 700,
                      letterSpacing: 0.4,
                      padding: "4px 10px",
                      borderRadius: 999,
                      textTransform: "uppercase",
                      opacity: interpolate(frame, [pickStart + 4, pickStart + 14], [0, 1], {
                        extrapolateLeft: "clamp",
                        extrapolateRight: "clamp",
                      }),
                    }}
                  >
                    Selected
                  </div>
                )}
              </div>
            );
          })}
        </div>

        {/* Caption: "Or roll your own." */}
        <div
          style={{
            marginTop: 12,
            opacity: interpolate(frame, [sec(5.0), sec(5.6)], [0, 1], {
              extrapolateLeft: "clamp",
              extrapolateRight: "clamp",
            }),
            fontFamily: fonts.sans,
            fontSize: 22,
            fontStyle: "italic",
            color: colors.textSecondary,
          }}
        >
          Or build your own sitcom cast.
        </div>
      </div>
    </AbsoluteFill>
  );
};

// Avoid `packs` typecheck noise if unused in future imports.
void neutral;
