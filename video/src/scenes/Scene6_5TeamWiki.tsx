import {
  AbsoluteFill,
  Easing,
  interpolate,
  spring,
  useCurrentFrame,
} from "remotion";
import { FPS, colors, fonts, sec } from "../theme";
import { FadeIn } from "../components/FadeIn";
import { PixelAvatar } from "../components/PixelAvatar";
import { WikiFonts } from "../components/WikiFonts";

// Visual tokens pulled from web/src/styles/wiki.css + global.css so the
// scene matches the real /wiki surface (not the DESIGN-WIKI spec, which
// the live implementation has already deviated from — see wiki.css:1-40).
const wk = {
  paper: "#FFFFFF",
  paperDark: "#f2f2f3",
  paperWarm: "#f8f8f9",
  text: "#28292a",
  textMuted: "#575a5c",
  textTertiary: "#85898b",
  border: "#e9eaeb",
  borderLight: "#f2f2f3",
  borderStrong: "#cfd1d2",
  olive500: "#3f4224",
  olive400: "#969640",
  olive300: "#d4db18",
  olive200: "#eef679",
  olive100: "#f8ffdb",
  wikilink: "#3f4224",
  wikilinkBroken: "#C94A4A",
  codeBg: "#f2f2f3",
} as const;

const DISPLAY_FONT = `"Fraunces", ui-serif, Georgia, "Times New Roman", serif`;
const BODY_SERIF = `"Source Serif 4", ui-serif, Georgia, serif`;
const MONO_FONT = `"Geist Mono", "SFMono-Regular", Menlo, Monaco, Consolas, monospace`;
const CHROME_FONT = fonts.sans;

const LEAD_PREFIX = "Customer X";
const LEAD_REST =
  " is a mid-market logistics company based in Cincinnati. They run a 14-truck regional fleet and moved from spreadsheet dispatching to API-driven routing in Q2.";
const APPENDED_SENTENCE =
  " They renewed for two years after a ride-along with their dispatch lead.";

export const Scene6_5TeamWiki: React.FC = () => {
  const frame = useCurrentFrame();

  // ── Scene-level enter (hard cut from Scene 6, but still fade the
  //    paper in 4 frames so the jump doesn't feel like a glitch).
  const sceneFade = interpolate(frame, [0, 4], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  // ── Article slides up as it arrives.
  const articleSlide = interpolate(frame, [0, 14], [18, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.out(Easing.cubic),
  });

  // ── Live-edit status banner pulse + amber dot.
  const pulse = 0.45 + 0.55 * (0.5 + 0.5 * Math.sin((frame / FPS) * Math.PI * 1.8));

  // ── Entity Brief Bar state machine:
  //    0.0–1.8s: "3 new facts since last synthesis" + Refresh button idle
  //    1.8–3.2s: Button clicked → "Synthesizing…" (disabled)
  //    3.2s:    SSE callback → bar flips to "Brief synthesized just now"
  //             + appended sentence types into the body
  const clickFrame = sec(1.8);
  const synthesizedFrame = sec(3.2);
  const beforeClick = frame < clickFrame;
  const afterSynth = frame >= synthesizedFrame;
  const synthesizing = !beforeClick && !afterSynth;

  const clickPress = frame >= clickFrame
    ? interpolate(frame, [clickFrame, clickFrame + 3, clickFrame + 8], [1, 0.94, 1], {
        extrapolateLeft: "clamp",
        extrapolateRight: "clamp",
      })
    : 1;

  // ── Appended sentence typewriter (starts at synthesizedFrame).
  const typeElapsed = Math.max(0, frame - synthesizedFrame);
  const typedChars = Math.min(APPENDED_SENTENCE.length, Math.floor(typeElapsed * 1.6));
  const typedText = APPENDED_SENTENCE.slice(0, typedChars);

  // ── Brief-bar flip animation: pending → synthesized.
  const flipSpring = spring({
    frame: Math.max(0, frame - synthesizedFrame),
    fps: FPS,
    config: { damping: 18, stiffness: 200 },
  });

  // ── git clone footer fades in near the end.
  const footerOpacity = interpolate(frame, [sec(6.5), sec(7.1)], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  return (
    <AbsoluteFill style={{ backgroundColor: wk.paper, opacity: sceneFade }}>
      <WikiFonts />

      {/* ── Top app bar (matches WUPHF chrome height: 46px) ──────────── */}
      <div
        style={{
          height: 46,
          borderBottom: `1px solid ${wk.border}`,
          backgroundColor: wk.paper,
          display: "flex",
          alignItems: "center",
          padding: "0 20px",
          fontFamily: CHROME_FONT,
          fontSize: 13,
          color: wk.textMuted,
          gap: 14,
          flexShrink: 0,
        }}
      >
        <div
          style={{
            fontWeight: 700,
            color: wk.text,
            letterSpacing: -0.2,
            fontSize: 14,
          }}
        >
          WUPHF
        </div>
        <div style={{ width: 1, height: 18, background: wk.border }} />
        <div style={{ fontWeight: 500, color: wk.text }}>Team Wiki</div>
        <div style={{ marginLeft: "auto", fontFamily: MONO_FONT, fontSize: 11, color: wk.textTertiary }}>
          port 7891 · local
        </div>
      </div>

      {/* ── Three-column body ───────────────────────────────────────── */}
      <div style={{ flex: 1, display: "flex", minHeight: 0 }}>
        {/* Left nav */}
        <div
          style={{
            width: 260,
            borderRight: `1px solid ${wk.border}`,
            backgroundColor: wk.paper,
            padding: "22px 14px",
            fontFamily: CHROME_FONT,
            fontSize: 13,
            color: wk.textMuted,
            flexShrink: 0,
          }}
        >
          <div
            style={{
              border: `1px solid ${wk.border}`,
              borderRadius: 8,
              padding: "7px 10px",
              fontSize: 12,
              color: wk.textTertiary,
              marginBottom: 18,
              backgroundColor: wk.paper,
            }}
          >
            Search wiki…
          </div>
          {[
            { label: "PEOPLE", items: ["nazz", "ryan", "stanley"] },
            { label: "COMPANIES", items: [] },
            { label: "CUSTOMERS", items: ["customer-x", "acme-co"] },
            { label: "PROJECTS", items: ["q2-routing"] },
            { label: "PLAYBOOKS", items: ["deal-prep"] },
          ].map((group) => (
            <div key={group.label} style={{ marginBottom: 16 }}>
              <div
                style={{
                  fontSize: 10,
                  fontWeight: 600,
                  textTransform: "uppercase",
                  letterSpacing: "0.08em",
                  color: wk.textTertiary,
                  padding: "0 10px 4px",
                }}
              >
                {group.label}
              </div>
              {group.items.map((it) => {
                const active = it === "customer-x";
                return (
                  <div
                    key={it}
                    style={{
                      padding: "6px 10px",
                      borderRadius: 6,
                      backgroundColor: active ? wk.olive200 : "transparent",
                      color: active ? wk.text : wk.textMuted,
                      fontWeight: active ? 500 : 400,
                      fontSize: 13,
                    }}
                  >
                    {it}
                  </div>
                );
              })}
            </div>
          ))}
        </div>

        {/* Article column */}
        <div
          style={{
            flex: 1,
            overflow: "hidden",
            padding: "0 64px 80px",
            transform: `translateY(${articleSlide}px)`,
          }}
        >
          {/* Status banner — live edit pulse */}
          <FadeIn startFrame={6} durationFrames={10}>
            <div
              style={{
                marginTop: 20,
                marginBottom: 24,
                background: wk.olive100,
                borderLeft: `4px solid ${wk.olive400}`,
                padding: "10px 16px",
                display: "flex",
                alignItems: "center",
                gap: 12,
                fontFamily: CHROME_FONT,
                fontSize: 13,
                color: wk.text,
              }}
            >
              <div
                style={{
                  width: 8,
                  height: 8,
                  borderRadius: "50%",
                  background: wk.olive400,
                  opacity: pulse,
                  flexShrink: 0,
                }}
              />
              <div>
                <strong style={{ fontWeight: 600 }}>Live:</strong> GTM is editing this article right now.
                Last saved a few seconds ago.
              </div>
              <div
                style={{
                  marginLeft: "auto",
                  fontFamily: MONO_FONT,
                  fontSize: 11,
                  color: wk.textMuted,
                  letterSpacing: 0,
                }}
              >
                47 rev · 6 contrib · 2,347 words
              </div>
            </div>
          </FadeIn>

          {/* Hat-bar tabs */}
          <div
            style={{
              borderBottom: `1px solid ${wk.border}`,
              display: "flex",
              fontFamily: CHROME_FONT,
              fontSize: 12,
              marginBottom: 0,
            }}
          >
            {[
              { label: "Article", active: true },
              { label: "Talk", active: false, dim: true },
              { label: "Edit", active: false },
              { label: "History", active: false },
              { label: "Raw markdown", active: false },
            ].map((t) => (
              <div
                key={t.label}
                style={{
                  padding: "8px 14px 10px",
                  color: t.active ? wk.text : t.dim ? wk.textTertiary : wk.textMuted,
                  fontWeight: t.active ? 500 : 400,
                  borderBottom: `2px solid ${t.active ? wk.text : "transparent"}`,
                  marginBottom: -1,
                }}
              >
                {t.label}
              </div>
            ))}
            <div
              style={{
                marginLeft: "auto",
                fontFamily: MONO_FONT,
                fontSize: 11,
                color: wk.textTertiary,
                padding: "8px 0 10px",
              }}
            >
              Cincinnati, OH · Mid-market Logistics
            </div>
          </div>

          {/* Breadcrumb */}
          <div
            style={{
              fontFamily: CHROME_FONT,
              fontSize: 12,
              color: wk.textMuted,
              margin: "20px 0 16px",
              display: "flex",
              gap: 6,
            }}
          >
            <span>Team Wiki</span>
            <span style={{ color: wk.textTertiary }}>›</span>
            <span>customers</span>
            <span style={{ color: wk.textTertiary }}>›</span>
            <span style={{ color: wk.text }}>Customer X</span>
          </div>

          {/* Title */}
          <div
            style={{
              fontFamily: DISPLAY_FONT,
              fontSize: 52,
              fontWeight: 500,
              lineHeight: 1.05,
              letterSpacing: -0.8,
              color: wk.text,
              fontVariationSettings: '"opsz" 100',
              margin: "0 0 4px",
            }}
          >
            Customer X
          </div>
          <div
            style={{
              fontFamily: BODY_SERIF,
              fontStyle: "italic",
              fontSize: 15,
              color: wk.textMuted,
              marginBottom: 14,
            }}
          >
            From Team Wiki, your team&rsquo;s encyclopedia.
          </div>
          <div
            style={{
              borderTop: `1px solid ${wk.text}`,
              borderBottom: `1px solid ${wk.text}`,
              height: 3,
              margin: "14px 0 24px",
            }}
          />

          {/* Byline */}
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: 10,
              fontFamily: CHROME_FONT,
              fontSize: 13,
              color: wk.textMuted,
              marginBottom: 28,
            }}
          >
            <div style={{ imageRendering: "pixelated" as const }}>
              <PixelAvatar slug="gtm" color={colors.gtm} size={22} />
            </div>
            <span>Last edited by</span>
            <span style={{ color: wk.text, fontWeight: 500 }}>GTM Lead</span>
            <span
              style={{
                background: wk.olive200,
                color: wk.text,
                padding: "2px 8px",
                borderRadius: 4,
                fontFamily: MONO_FONT,
                fontSize: 11,
                display: "inline-flex",
                alignItems: "center",
                gap: 6,
              }}
            >
              <span
                style={{
                  width: 6,
                  height: 6,
                  borderRadius: "50%",
                  background: wk.olive400,
                  opacity: pulse,
                }}
              />
              a few seconds ago
            </span>
            <span style={{ color: wk.textTertiary }}>·</span>
            <span>started 42 days ago</span>
            <span style={{ color: wk.textTertiary }}>·</span>
            <span>6 contributors</span>
          </div>

          {/* Entity Brief Bar — the v1.2 feature */}
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: 14,
              padding: "10px 14px",
              border: `1px solid ${afterSynth ? wk.border : wk.olive300}`,
              background: afterSynth ? wk.paperWarm : wk.olive100,
              borderRadius: 8,
              fontFamily: CHROME_FONT,
              fontSize: 13,
              color: wk.text,
              marginBottom: 28,
              transform: afterSynth ? `scale(${0.98 + flipSpring * 0.02})` : "scale(1)",
              transition: "none",
            }}
          >
            {afterSynth ? (
              <span style={{ color: wk.textMuted }}>
                <strong style={{ color: wk.text, fontWeight: 600 }}>Brief synthesized</strong> just now. 0 new facts since.
              </span>
            ) : (
              <span>
                <strong style={{ fontWeight: 700 }}>3</strong> new facts since last synthesis
              </span>
            )}
            {!afterSynth && (
              <div
                style={{
                  marginLeft: "auto",
                  padding: "6px 14px",
                  borderRadius: 6,
                  background: synthesizing ? wk.borderStrong : wk.olive300,
                  color: synthesizing ? wk.textMuted : wk.text,
                  fontWeight: 500,
                  fontSize: 12,
                  transform: `scale(${clickPress})`,
                  fontFamily: CHROME_FONT,
                }}
              >
                {synthesizing ? "Synthesizing…" : "Refresh brief"}
              </div>
            )}
          </div>

          {/* Lead paragraph */}
          <div
            style={{
              fontFamily: BODY_SERIF,
              fontSize: 20,
              lineHeight: 1.72,
              color: wk.text,
              maxWidth: 720,
              fontVariationSettings: '"opsz" 24',
            }}
          >
            <strong style={{ fontWeight: 700 }}>{LEAD_PREFIX}</strong>
            {LEAD_REST}
            <sup>
              <span
                style={{
                  color: wk.wikilink,
                  borderBottom: `1px dashed ${wk.wikilink}`,
                  fontSize: 13,
                  marginLeft: 2,
                }}
              >
                [1]
              </span>
            </sup>
            {typedText && (
              <span style={{ color: wk.text }}>
                {typedText}
                {typedChars < APPENDED_SENTENCE.length && (
                  <span
                    style={{
                      display: "inline-block",
                      width: 2,
                      height: 22,
                      background: wk.olive400,
                      marginLeft: 2,
                      verticalAlign: "middle",
                      opacity: Math.floor(frame / 15) % 2 === 0 ? 1 : 0,
                    }}
                  />
                )}
              </span>
            )}
          </div>

          {/* Section head */}
          <div
            style={{
              fontFamily: DISPLAY_FONT,
              fontSize: 28,
              fontWeight: 500,
              color: wk.text,
              marginTop: 40,
              marginBottom: 6,
              paddingBottom: 4,
              borderBottom: `1px solid ${wk.border}`,
              fontVariationSettings: '"opsz" 36',
            }}
          >
            Relationship
          </div>
          <div
            style={{
              fontFamily: BODY_SERIF,
              fontSize: 18,
              lineHeight: 1.72,
              color: wk.text,
              maxWidth: 720,
            }}
          >
            Primary contact is{" "}
            <span
              style={{
                color: wk.wikilink,
                borderBottom: `1px dashed ${wk.wikilink}`,
              }}
            >
              Pam Beesly
            </span>
            , Head of Dispatch. Renewal conversation owned by{" "}
            <span
              style={{
                color: wk.wikilinkBroken,
                borderBottom: `1px dashed ${wk.wikilinkBroken}`,
              }}
            >
              GTM Playbook
            </span>
            .
          </div>
        </div>

        {/* Right rail — TOC box, kept narrow so the article stays hero */}
        <div
          style={{
            width: 300,
            borderLeft: `1px solid ${wk.border}`,
            padding: "22px 20px",
            fontFamily: CHROME_FONT,
            fontSize: 12,
            color: wk.textMuted,
            flexShrink: 0,
          }}
        >
          <div
            style={{
              background: wk.paperWarm,
              border: `1px solid ${wk.border}`,
              padding: 14,
              borderRadius: 6,
            }}
          >
            <div
              style={{
                fontSize: 10,
                fontWeight: 600,
                textTransform: "uppercase",
                letterSpacing: "0.08em",
                color: wk.textTertiary,
                marginBottom: 10,
              }}
            >
              Contents
            </div>
            {[
              { num: "1", label: "Overview" },
              { num: "2", label: "Relationship" },
              { num: "2.1", label: "Buying committee", indent: true },
              { num: "3", label: "Ops posture" },
              { num: "4", label: "Sources" },
            ].map((t) => (
              <div
                key={t.num}
                style={{
                  display: "flex",
                  gap: 8,
                  padding: "3px 0",
                  paddingLeft: t.indent ? 14 : 0,
                  color: wk.text,
                  fontSize: 12,
                }}
              >
                <span style={{ fontFamily: MONO_FONT, color: wk.textTertiary, minWidth: 24 }}>
                  {t.num}
                </span>
                <span>{t.label}</span>
              </div>
            ))}
          </div>
        </div>
      </div>

      {/* ── Edit-log footer: the files-over-apps punchline ─────────── */}
      <div
        style={{
          height: 44,
          borderTop: `1px solid ${wk.border}`,
          backgroundColor: wk.paperWarm,
          padding: "0 20px",
          display: "flex",
          alignItems: "center",
          gap: 14,
          fontFamily: MONO_FONT,
          fontSize: 12,
          color: wk.textMuted,
          flexShrink: 0,
          opacity: footerOpacity,
        }}
      >
        <span style={{ color: wk.olive400 }}>$</span>
        <span style={{ color: wk.text }}>
          git clone <span style={{ color: wk.wikilink }}>git@wuphf.local:team/wiki.git</span>
        </span>
        <span style={{ marginLeft: "auto", color: wk.textTertiary, fontSize: 11 }}>
          Your agents write it down. You own the repo.
        </span>
      </div>
    </AbsoluteFill>
  );
};
