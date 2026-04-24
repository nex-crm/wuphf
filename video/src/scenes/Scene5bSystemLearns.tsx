import { AbsoluteFill, useCurrentFrame, interpolate, Easing } from "remotion";
import { colors, fonts, sec } from "../theme";
import { PixelAvatar } from "../components/PixelAvatar";
import { FadeIn } from "../components/FadeIn";
import { NotebookFonts } from "../components/NotebookFonts";

// v26 Scene 5b, reframed 2026-04-22: narration stays byte-for-byte
// ("They notice patterns, propose improvements. You just say yes…"),
// but the artifact on screen now reflects the real product feature —
// an agent's notebook entry being promoted to the team wiki via
// human review. Card styled against web/src/styles/notebook.css tokens.

const NB = {
  paper: "#FAFFE5",
  paperDark: "#E9DFC9",
  rule: "rgba(150, 150, 64, 0.07)",
  surface: "#FAF5E8",
  text: "#2A2721",
  textMuted: "#5B5547",
  textTertiary: "#8A8373",
  border: "#D9CEB5",
  borderLight: "#E6DEC6",
  amber: "#C78A1F",
  amberBg: "rgba(199, 138, 31, 0.10)",
  green: "#6A8B52",
  greenBg: "rgba(106, 139, 82, 0.12)",
  stampRed: "#B43A2F",
  display: "'Covered By Your Grace', 'Comic Sans MS', cursive",
  bodySerif: "'IBM Plex Serif', Georgia, serif",
  chrome: "Inter, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif",
  mono: "'Geist Mono', SFMono-Regular, Menlo, monospace",
};

const ELEGANT = Easing.bezier(0.25, 0.46, 0.45, 0.94);

export const Scene5bSystemLearns: React.FC = () => {
  const frame = useCurrentFrame();

  const bgOpacity = interpolate(frame, [0, 14], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });
  // Tail fade-out — keeps gradient bg continuous with neighboring scenes
  const SCENE_DURATION_5B = sec(10);
  const exitFade = interpolate(
    frame,
    [SCENE_DURATION_5B - 18, SCENE_DURATION_5B],
    [1, 0],
    { extrapolateLeft: "clamp", extrapolateRight: "clamp", easing: ELEGANT },
  );

  const headerOpacity = interpolate(frame, [5, 22], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: ELEGANT,
  });
  const headerSlide = interpolate(frame, [5, 22], [8, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: ELEGANT,
  });

  // Notebook card appears FIRST; CEO bubble slides out from under it
  const cardOpacity = interpolate(frame, [5, 22], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: ELEGANT,
  });
  const cardSlide = interpolate(frame, [5, 22], [18, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: ELEGANT,
  });

  // Bubble slides down from behind the card's bottom edge
  const bubbleStart = sec(1.4);
  const bubbleEnd = sec(2.1);
  const ceoMsgOpacity = interpolate(frame, [bubbleStart, bubbleStart + 6], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: ELEGANT,
  });
  // Starts hidden behind the card (negative Y), ends at natural flex slot below
  const ceoMsgSlide = interpolate(frame, [bubbleStart, bubbleEnd], [-180, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: ELEGANT,
  });

  // Promote state machine — smoother crossfades between states
  const clickFrame = sec(3.8);
  const promotedFrame = sec(5.0);
  const buttonGlow = interpolate(
    frame,
    [sec(2.5), sec(3.2), sec(3.8), sec(4.4)],
    [0, 1, 1, 0.2],
    { extrapolateLeft: "clamp", extrapolateRight: "clamp", easing: ELEGANT },
  );
  const buttonPress =
    frame >= clickFrame
      ? interpolate(frame, [clickFrame, clickFrame + 3, clickFrame + 10], [1, 0.94, 1], {
          extrapolateLeft: "clamp",
          extrapolateRight: "clamp",
          easing: ELEGANT,
        })
      : 1;
  // Smooth state-blend values: 0 idle → 1 pending (at clickFrame+8) → 1 promoted
  const toPending = interpolate(frame, [clickFrame, clickFrame + 8], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: ELEGANT,
  });
  const toPromoted = interpolate(frame, [promotedFrame, promotedFrame + 10], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: ELEGANT,
  });
  const pending = frame >= clickFrame && frame < promotedFrame;
  const promoted = frame >= promotedFrame;

  const rippleScale = interpolate(frame, [promotedFrame, promotedFrame + 22], [0.6, 2], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: ELEGANT,
  });
  const rippleOpacity = interpolate(frame, [promotedFrame, promotedFrame + 22], [0.3, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: ELEGANT,
  });

  const taglineOpacity = interpolate(frame, [sec(5.8), sec(6.6)], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: ELEGANT,
  });

  // Slow scene zoom — 1.1 → 1.18 across the full 10-second beat
  const SCENE_DURATION = sec(10);
  const sceneZoom = interpolate(frame, [0, SCENE_DURATION], [1.1, 1.18], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  return (
    <AbsoluteFill
      style={{
        opacity: bgOpacity,
        background: "radial-gradient(ellipse at top, #f3e8ff 0%, #ede2f7 40%, #d9c6ea 100%)",
      }}
    >
      <NotebookFonts />
      <div
        style={{
          position: "absolute",
          inset: 0,
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          justifyContent: "center",
          padding: 60,
          gap: 28,
          transform: `scale(${sceneZoom})`,
          transformOrigin: "center",
          willChange: "transform",
          backfaceVisibility: "hidden",
          opacity: exitFade,
        }}
      >
        {/* Eyebrow — matches Scene 3 "Pick a pack" style */}
        <div
          style={{
            opacity: headerOpacity,
            transform: `translateY(${headerSlide}px)`,
            fontFamily: fonts.sans,
            fontSize: 20,
            fontWeight: 700,
            letterSpacing: "0.22em",
            textTransform: "uppercase" as const,
            color: "#9F4DBF",
          }}
        >
          Pattern detected
        </div>

        {/* Notebook entry card — full nb-* token set from notebook.css */}
        <div
          style={{
            opacity: cardOpacity,
            transform: `translateY(${cardSlide}px)`,
            width: 860,
            background: NB.paper,
            backgroundImage: `repeating-linear-gradient(0deg, transparent 0, transparent 15px, ${NB.rule} 15px, ${NB.rule} 16px)`,
            borderRadius: 14,
            border: `1px solid ${toPromoted > 0.5 ? NB.green : NB.border}`,
            boxShadow: "0 1px 2px rgba(0,0,0,0.05), 0 8px 24px rgba(0,0,0,0.08), 0 24px 60px rgba(0,0,0,0.10)",
            overflow: "hidden",
            position: "relative",
            zIndex: 2,
            color: NB.text,
          }}
        >
          {/* Meta strip */}
          <div
            style={{
              padding: "14px 24px 10px",
              borderBottom: `1px solid ${NB.borderLight}`,
              display: "flex",
              alignItems: "center",
              gap: 10,
              fontFamily: NB.chrome,
              fontSize: 12,
              color: NB.textMuted,
            }}
          >
            <div style={{ width: 22, height: 22, display: "flex", alignItems: "center", justifyContent: "center" }}>
              <PixelAvatar slug="ceo" color={colors.ceo} size={22} />
            </div>
            <span style={{ color: NB.text, fontWeight: 500 }}>CEO&rsquo;s notebook</span>
            <span style={{ color: NB.textTertiary }}>·</span>
            <span
              style={{
                fontFamily: NB.chrome,
                fontSize: 10,
                fontWeight: 700,
                textTransform: "uppercase" as const,
                letterSpacing: "0.08em",
                color: pending ? NB.amber : promoted ? NB.green : NB.textTertiary,
              }}
            >
              {promoted ? "Promoted" : pending ? "In review" : "Draft"}
            </span>
            <span
              style={{
                marginLeft: "auto",
                fontFamily: NB.mono,
                fontSize: 11,
                color: NB.textTertiary,
              }}
            >
              team/playbooks/deal-prep.md
            </span>
          </div>

          {/* Title + body */}
          <div style={{ padding: "26px 34px 20px" }}>
            <div
              style={{
                fontFamily: NB.display,
                fontSize: 54,
                fontWeight: 400,
                color: NB.text,
                lineHeight: 1.05,
              }}
            >
              Deal prep playbook
            </div>
            <div
              style={{
                fontFamily: NB.bodySerif,
                fontStyle: "italic",
                fontSize: 16,
                color: NB.textMuted,
                marginTop: 6,
                marginBottom: 16,
              }}
            >
              Draft, from CEO&rsquo;s notebook.
            </div>
            <div
              style={{
                fontFamily: NB.bodySerif,
                fontSize: 20,
                lineHeight: 1.65,
                color: NB.text,
                maxWidth: 820,
              }}
            >
              Pull the company brief, past touchpoints, the buying committee, and likely objections.
              Drop the note in the channel ten minutes before every meeting.
            </div>
          </div>

          {/* Actions footer — mirrors real PromoteButton row */}
          <div
            style={{
              padding: "14px 24px",
              borderTop: `1px solid ${NB.border}`,
              background: "#FFFFFF",
              display: "flex",
              alignItems: "center",
              gap: 12,
            }}
          >
            <span style={{ fontFamily: NB.chrome, fontSize: 13, color: NB.textMuted }}>
              {promoted ? (
                <span style={{ color: NB.green, fontWeight: 600 }}>Added to team wiki ✓</span>
              ) : pending ? (
                <>Waiting on reviewer…</>
              ) : (
                <>Ready to submit for review.</>
              )}
            </span>
            <div style={{ marginLeft: "auto", display: "flex", alignItems: "center", gap: 16 }}>
              {/* .nb-discard-link — exact port from notebook.css:634-643 */}
              <button
                style={{
                  fontFamily: NB.chrome,
                  fontSize: 13,
                  color: NB.textMuted,
                  textDecoration: "underline",
                  background: "transparent",
                  border: 0,
                  cursor: "pointer",
                  padding: "4px 2px",
                }}
              >
                Discard entry
              </button>
              {/* .nb-promote-btn — exact port from notebook.css:600-632 */}
              <button
                style={{
                  position: "relative",
                  fontFamily: "Inter, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif",
                  fontSize: 13,
                  fontWeight: 600,
                  lineHeight: 1,
                  background: promoted
                    ? NB.green
                    : pending
                    ? "rgba(159, 77, 191, 0.12)"
                    : "#9f4dbf",
                  color: promoted || !pending ? "#fff" : "#9f4dbf",
                  border: `1px solid ${
                    promoted ? NB.green : pending ? "rgba(159, 77, 191, 0.35)" : "#9f4dbf"
                  }`,
                  padding: "0 16px",
                  height: 44,
                  minHeight: 44,
                  cursor: pending ? "not-allowed" : "pointer",
                  display: "inline-flex",
                  alignItems: "center",
                  gap: 6,
                  textDecoration: "none",
                  borderRadius: 8,
                  transform: `scale(${buttonPress})`,
                  overflow: "hidden",
                }}
              >
                {/* Crossfade labels across states */}
                <span style={{
                  position: "absolute", inset: 0,
                  display: "flex", alignItems: "center", justifyContent: "center",
                  opacity: 1 - Math.max(toPending, toPromoted),
                }}>Promote to wiki →</span>
                <span style={{
                  position: "absolute", inset: 0,
                  display: "flex", alignItems: "center", justifyContent: "center",
                  opacity: toPending * (1 - toPromoted),
                }}>Pending review…</span>
                <span style={{
                  position: "absolute", inset: 0,
                  display: "flex", alignItems: "center", justifyContent: "center",
                  opacity: toPromoted,
                }}>Added to wiki ✓</span>
                {/* invisible spacer keeps box height */}
                <span style={{ visibility: "hidden" }}>Pending review…</span>
              </button>
            </div>
          </div>

          {/* Success ripple */}
          {frame >= promotedFrame && (
            <div
              style={{
                position: "absolute",
                top: "50%",
                right: 140,
                width: 260,
                height: 260,
                borderRadius: "50%",
                border: `2px solid ${NB.green}`,
                transform: `translate(-50%, -50%) scale(${rippleScale})`,
                opacity: rippleOpacity,
                pointerEvents: "none" as const,
              }}
            />
          )}
        </div>

        {/* CEO chat bubble — slides down from behind the card's bottom edge */}
        <div
          style={{
            opacity: ceoMsgOpacity,
            transform: `translateY(${ceoMsgSlide}px)`,
            display: "flex",
            gap: 18,
            width: 720,
            padding: "24px 32px",
            marginTop: -40,
            fontFamily: fonts.sans,
            backgroundColor: "#FFFFFF",
            borderRadius: 16,
            border: `1px solid ${colors.border}`,
            boxShadow: "0 1px 2px rgba(0,0,0,0.05), 0 8px 24px rgba(0,0,0,0.08), 0 24px 60px rgba(0,0,0,0.10)",
            zIndex: 1,
            position: "relative",
          }}
        >
          <div
            style={{
              width: 46,
              height: 46,
              background: "#f2f2f3",
              borderRadius: 9,
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              overflow: "hidden",
              flexShrink: 0,
            }}
          >
            <PixelAvatar slug="ceo" color={colors.ceo} size={36} />
          </div>
          <div style={{ flex: 1 }}>
            <div style={{ display: "flex", alignItems: "baseline", gap: 10 }}>
              <span style={{ fontFamily: fonts.sans, fontSize: 17, fontWeight: 700, color: "#28292a" }}>
                CEO
              </span>
              <span
                style={{
                  fontFamily: fonts.sans,
                  background: colors.greenBg,
                  color: colors.green,
                  padding: "2px 8px",
                  borderRadius: 3,
                  fontSize: 11,
                  fontWeight: 500,
                }}
              >
                lead
              </span>
              <span style={{ fontFamily: fonts.sans, fontSize: 12, color: colors.textTertiary, marginLeft: "auto" }}>
                9:14 AM
              </span>
            </div>
            <div
              style={{
                fontFamily: fonts.sans,
                fontSize: 17,
                color: colors.textSecondary,
                marginTop: 6,
                lineHeight: 1.55,
              }}
            >
              I&rsquo;ve noticed we prep every sales meeting the same way.
              <br />
              Drafting a playbook for the wiki…
            </div>
          </div>
        </div>

        {/* Promoted trail pill — grows its height so the tagline below
            smoothly slides down as the pill claims its space. */}
        {(() => {
          const pillExpand = interpolate(
            frame,
            [promotedFrame + 2, promotedFrame + 18],
            [0, 1],
            { extrapolateLeft: "clamp", extrapolateRight: "clamp", easing: ELEGANT },
          );
          const pillOpacity = interpolate(
            frame,
            [promotedFrame + 4, promotedFrame + 16],
            [0, 1],
            { extrapolateLeft: "clamp", extrapolateRight: "clamp", easing: ELEGANT },
          );
          return (
            <div
              style={{
                overflow: "hidden",
                maxHeight: pillExpand * 80,
                marginTop: pillExpand * 4,
                display: "flex",
                justifyContent: "center",
              }}
            >
              <div
                style={{
                  opacity: pillOpacity,
                  transform: `translateY(${(1 - pillOpacity) * 8}px)`,
                  display: "flex",
                  alignItems: "center",
                  gap: 10,
                  background: "#FFFFFF",
                  border: `1px solid ${colors.border}`,
                  padding: "7px 14px",
                  borderRadius: 999,
                  fontFamily: fonts.mono,
                  fontSize: 13,
                  color: colors.text,
                  boxShadow: "0 2px 6px rgba(0,0,0,0.06), 0 8px 20px rgba(0,0,0,0.08)",
                }}
              >
                <span style={{ color: NB.green }}>+</span>
                <span>team / playbooks / deal-prep.md</span>
                <span style={{ color: colors.textTertiary }}>→ wiki</span>
              </div>
            </div>
          );
        })()}

        {/* Closing tagline */}
        <FadeIn startFrame={sec(5.8)} durationFrames={14} slideUp={10}>
          <div
            style={{
              opacity: taglineOpacity,
              fontFamily: fonts.sans,
              fontSize: 20,
              fontWeight: 500,
              color: "#3B145D",
              textAlign: "center",
              maxWidth: 900,
            }}
          >
            An agent drafts it. You approve. Every other agent can now read it.
          </div>
        </FadeIn>
      </div>
    </AbsoluteFill>
  );
};
