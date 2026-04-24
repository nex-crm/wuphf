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
  const PICK_DURATION = 30;
  const ELEGANT = Easing.bezier(0.25, 0.46, 0.45, 0.94);
  // Smooth 0 → 1 ramp, no spring overshoot, same timing as muteFade
  const pickScale = interpolate(
    frame,
    [pickStart, pickStart + PICK_DURATION],
    [0, 1],
    { extrapolateLeft: "clamp", extrapolateRight: "clamp", easing: ELEGANT },
  );
  // Non-picked elements fade out with the same curve as the pick beat
  const muteFade = interpolate(
    frame,
    [pickStart, pickStart + PICK_DURATION - 8],
    [1, 0],
    { extrapolateLeft: "clamp", extrapolateRight: "clamp", easing: ELEGANT },
  );

  // Tail fade-out — content dims while the gradient bg stays continuous so
  // the transition to Scene 4 reads as a crossfade on the same background.
  const SCENE_DURATION = sec(7.5);
  const exitFade = interpolate(
    frame,
    [SCENE_DURATION - 18, SCENE_DURATION],
    [1, 0],
    { extrapolateLeft: "clamp", extrapolateRight: "clamp", easing: ELEGANT },
  );

  return (
    <AbsoluteFill style={{
      background: "radial-gradient(ellipse at top, #f3e8ff 0%, #ede2f7 40%, #d9c6ea 100%)",
    }}>
      <DotGrid color="#3B145D" opacity={0.04} spacing={40} size={1.2} />

      <div
        style={{
          position: "absolute",
          inset: 0,
          padding: 80,
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          justifyContent: "center",
          rowGap: muteFade * 140,
          transform: "translateY(100px)",
          opacity: exitFade,
        }}
      >
        <div
          style={{
            opacity: titleOpacity * muteFade,
            transform: `translateY(${titleSlide}px)`,
            textAlign: "center",
            maxHeight: muteFade * 400,
            overflow: "hidden",
          }}
        >
          <div
            style={{
              fontFamily: fonts.sans,
              fontSize: 20,
              fontWeight: 700,
              letterSpacing: "0.22em",
              textTransform: "uppercase",
              color: "#9F4DBF",
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
              color: "#3B145D",
              letterSpacing: -1.5,
              lineHeight: 1.05,
            }}
          >
            Your office, out of the box.
          </div>
        </div>

        {/* Cards — fanned out like a hand of playing cards */}
        <div
          style={{
            display: "flex",
            justifyContent: "center",
            alignItems: "center",
            width: "100%",
          }}
        >
          {packs.map((p, i) => {
            const ELEGANT = Easing.bezier(0.25, 0.46, 0.45, 0.94);
            const appearAt = 8 + i * 6;
            // Fan geometry — each card pivots around a point far below the deck
            const fanCenter = (packs.length - 1) / 2;
            const offset = i - fanCenter;
            const fanAngle = offset * 13;    // spread angle per card
            const picked = i === pickedIndex;

            // Spring-driven entry — pulls card up from below into its fan slot
            const enter = spring({
              frame: frame - appearAt,
              fps: FPS,
              config: { damping: 13, stiffness: 170, mass: 0.85 },
            });
            const opacity = Math.min(1, enter * 1.6);
            const rotateZAnim = enter * fanAngle;
            // Arc lift — central cards sit higher than outer cards (bell curve)
            const maxOffset = fanCenter; // = (packs.length - 1) / 2
            const arcLift = Math.cos((offset / maxOffset) * Math.PI / 2) * 55;
            const translateY = (1 - enter) * 280 - arcLift * enter;

            // Pick beat
            //   picked card: scales up, centers horizontally, lifts
            //   non-picked cards: close the gap — re-fan across the slots
            //   the picked card vacated (so they fill the empty space
            //   rather than collapsing to a single point).
            const pickProgress = frame > pickStart ? pickScale : 0;
            const SLOT_SPACING = 180;   // cardWidth 360 + margin -90*2
            // Re-fan target offset: remaining cards redistribute as a new
            // 3-card fan. Cards with index < pickedIndex slide right, cards
            // with index > pickedIndex slide left.
            let targetOffset = offset;
            if (!picked) {
              const newSeqIndex = i < pickedIndex ? i : i - 1;
              const newFanCenter = (packs.length - 2) / 2;  // 3 cards remaining
              targetOffset = newSeqIndex - newFanCenter;
            } else {
              targetOffset = 0;   // picked card goes to scene center
            }
            const slotShift = (targetOffset - offset) * SLOT_SPACING * pickProgress;
            const targetFanAngle = targetOffset * 13;
            const angleShift = (targetFanAngle - fanAngle) * pickProgress;

            const pickedLift = picked ? -20 * pickProgress : 0;
            const cardScale = picked ? 1 + 0.24 * pickProgress : 1;
            // Gate the "picked" visual treatment until the pick beat starts
            const isActive = picked && frame >= pickStart;
            // Fan pivots at each card's own bottom edge — matching a
            // classic hand-of-cards fan (tops splay out, bottoms aligned).
            // Picked card migrates its pivot toward card center during pick
            // so the scale-up doesn't push it off-axis.
            const originY = picked
              ? 360 * (1 - pickProgress) + 180 * pickProgress
              : 360;

            const roster = PACK_ROSTERS[p.name] ?? [];

            return (
              <div
                key={p.name}
                style={{
                  opacity: picked ? opacity : opacity * muteFade,
                  transform: `translate(${slotShift}px, ${translateY + pickedLift}px) rotateZ(${rotateZAnim + angleShift}deg) scale(${cardScale})`,
                  transformOrigin: `50% ${originY}px`,
                  margin: "0 -90px",
                  width: 360,
                  zIndex: isActive ? 40 : 10 + i,
                  background: colors.bgCard,
                  border: `1px solid ${isActive ? "#9F4DBF" : colors.border}`,
                  boxShadow: isActive
                    ? [
                        "0 1px 2px rgba(0, 0, 0, 0.06)",
                        "0 4px 10px rgba(0, 0, 0, 0.08)",
                        "0 20px 40px rgba(0, 0, 0, 0.12)",
                        "0 48px 96px rgba(0, 0, 0, 0.14)",
                      ].join(", ")
                    : [
                        "0 1px 2px rgba(0, 0, 0, 0.05)",
                        "0 3px 8px rgba(0, 0, 0, 0.05)",
                        "0 12px 28px rgba(0, 0, 0, 0.07)",
                        "0 32px 64px rgba(0, 0, 0, 0.08)",
                      ].join(", "),
                  borderRadius: 20,
                  padding: 28,
                  display: "flex",
                  flexDirection: "column",
                  gap: 16,
                  minHeight: 360,
                  position: "relative",
                }}
              >
                {/* Agents pill — top of card */}
                <div style={{ display: "flex", justifyContent: "flex-end" }}>
                  <div
                    style={{
                      display: "inline-flex",
                      alignItems: "center",
                      gap: 6,
                      fontFamily: fonts.sans,
                      fontSize: 12,
                      fontWeight: 600,
                      letterSpacing: "0.06em",
                      textTransform: "uppercase",
                      color: "#9F4DBF",
                      background: "#FFEBFC",
                      padding: "5px 12px",
                      borderRadius: 999,
                    }}
                  >
                    <span style={{
                      fontFamily: fonts.mono,
                      fontWeight: 700,
                      fontSize: 13,
                      letterSpacing: 0,
                    }}>{p.agents}</span>
                    <span>agents</span>
                  </div>
                </div>

                {/* Emoji */}
                <div
                  style={{
                    fontSize: 52,
                    lineHeight: 1,
                    filter: picked ? "none" : "grayscale(0.15)",
                  }}
                >
                  {p.emoji}
                </div>

                <div
                  style={{
                    fontFamily: fonts.sans,
                    fontSize: 30,
                    fontWeight: 800,
                    color: "#3B145D",
                    letterSpacing: -0.8,
                    lineHeight: 1.1,
                  }}
                >
                  {p.name}
                </div>
                <div
                  style={{
                    fontFamily: fonts.sans,
                    fontSize: 16,
                    fontWeight: 400,
                    color: "#612a92",
                    opacity: 0.8,
                    lineHeight: 1.5,
                  }}
                >
                  {p.desc}
                </div>

                {/* Roster row */}
                <div style={{ marginTop: "auto", display: "flex", flexDirection: "column", gap: 10 }}>
                  <div style={{
                    fontFamily: fonts.sans,
                    fontSize: 10,
                    fontWeight: 700,
                    letterSpacing: "0.10em",
                    textTransform: "uppercase",
                    color: "#9F4DBF",
                    opacity: 0.75,
                  }}>
                    Includes
                  </div>
                  <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
                  {roster.slice(0, 5).map((r) => (
                    <div
                      key={r.slug}
                      style={{
                        width: 36,
                        height: 36,
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "center",
                        overflow: "hidden",
                      }}
                    >
                      <PixelAvatar slug={r.slug} color={r.color} size={32} />
                    </div>
                  ))}
                  </div>
                </div>

              </div>
            );
          })}
        </div>

        {/* Caption: "Or build your own sitcom cast." */}
        <div
          style={{
            marginTop: 64,
            position: "relative",
            zIndex: 50,
            opacity: interpolate(frame, [sec(5.0), sec(5.6)], [0, 1], {
              extrapolateLeft: "clamp",
              extrapolateRight: "clamp",
            }),
            fontFamily: fonts.sans,
            fontSize: 34,
            fontWeight: 600,
            fontStyle: "italic",
            color: "#3B145D",
            textAlign: "center",
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
